// Package api is the API. Typed operations in, typed answers out.
//
// It knows nothing about Telegram: no chat ids, no bots, no envelopes, no correlation ids. Those
// belong to the transport adapter (docs/01-architecture.md, R-API-1). The point is not that a
// second transport is coming — it is that the operations stay describable without one.
package api

import (
	"context"
	"fmt"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
)

// Principal is who is asking. The transport resolves it — from a Telegram chat id today — and the
// API never learns how (R-API-1).
type Principal struct {
	UserID     int64
	ProviderID int32
	// DeviceID distinguishes a user's devices, which share one chat and therefore one message
	// budget (docs/04 §5).
	DeviceID uint32
	// ReaderChatID is the chat delivery bookkeeping is keyed on. Not the same axis as UserID: one
	// user with both a session and a personal bot has two readers, each with its own delivered
	// messages (docs/06 §3) — the bug that cost a day in the prototype.
	ReaderChatID int64
	// Role is the caller's users.role, resolved by the transport from the binding — never from a
	// payload (R-SEC-9). Empty for a dev/unbound principal. It grants nothing about content
	// (R-DATA-6); its only use is exempting staff from the content-request quota (ADR 0014 §D2).
	Role string
	// BetaTester is users.beta_tester from the binding. Staff (admin, superadmin) are nightly
	// testers without this flag. It only selects the in-app shelf. main-bot Download stays stable.
	BetaTester bool
}

// RailStore is what the API needs from storage. Declared here, by the consumer, and small
// (R-GO-5).
type RailStore interface {
	Rail(ctx context.Context, kind catalog.RailKind, tab catalog.Kind) ([]catalog.Row, error)
	RailSeq(ctx context.Context) (uint64, error)
}

// HomeTTL is how long a client may treat a home answer as fresh.
//
// Server-driven so it can be tuned without shipping an app. An hour, because not much is published
// in an hour (docs/11 §2).
const HomeTTL = 3600

// API implements the operation set.
type API struct {
	rails      RailStore
	deliveries DeliveryStore
	catalog    CatalogStore
	searches   SearchStore
	browse     BrowseStore
	hydrate    TitleHydrator
	person     PersonHydrator
	searcher   TitleSearcher
	requests   RequestStore
	geminiKeys GeminiKeyStore
	releases   AppReleaseStore

	playbackPrefs PlaybackPrefsStore
	devices       deviceStore
	taste         TasteStore
	peopleSearch  PersonSearcher
	tasteSeeds    TasteTitleSeeder

	subtitles         SubtitleProvider
	subtitleCache     SubtitleCacheStore
	subtitleUploader  SubtitleUploader
	subtitleDeliverer SubtitleDeliverer

	personal        PersonalStore
	personalActions PersonalActions

	limits RateLimits
}

// New builds an API over a store.
func New(rails RailStore, deliveries DeliveryStore, catalogStore CatalogStore, searches SearchStore, limits RateLimits) *API {
	return &API{rails: rails, deliveries: deliveries, catalog: catalogStore, searches: searches, limits: limits}
}

// WithBrowse attaches env browse/list/user-state (ADR 0009).
func (a *API) WithBrowse(b BrowseStore) *API {
	a.browse = b
	return a
}

// WithHydrate attaches TMDB title + person loaders for on-demand misses.
func (a *API) WithHydrate(titles TitleHydrator, people PersonHydrator) *API {
	a.hydrate = titles
	a.person = people
	return a
}

// WithTitleSearch attaches TMDB multi-search for SearchRes.missing (ADR 0010).
func (a *API) WithTitleSearch(s TitleSearcher) *API {
	a.searcher = s
	return a
}

// WithRequests attaches the content-request store (ADR 0014). Absent, Request returns an error
// rather than silently accepting nothing.
func (a *API) WithRequests(r RequestStore) *API {
	a.requests = r
	return a
}

// WithGeminiKeys attaches the sealed Gemini key store (doc 15 §12). Absent, Prefs returns an error
// rather than claiming the user has no key.
func (a *API) WithGeminiKeys(s GeminiKeyStore) *API {
	a.geminiKeys = s
	return a
}

// deviceStore records which app shelf checked in. Nil means home still works.
type deviceStore interface {
	TouchDevice(ctx context.Context, userID int64, platform string) error
}

// WithDevices records HomeReq.platform so staff can see which apps are in use.
func (a *API) WithDevices(s deviceStore) *API {
	a.devices = s
	return a
}

func (a *API) noteDevice(ctx context.Context, userID int64, platform string) {
	if a.devices == nil || userID == 0 || platform == "" {
		return
	}
	// A failed stamp must not blank the home screen.
	if err := a.devices.TouchDevice(ctx, userID, platform); err != nil {
		return
	}
}

// Home returns one tab of the home screen: the whole screen in one answer, because the transport
// gives us about one message per second and a second round-trip is a second the user waits
// (R-PROTO-3).
func (a *API) Home(ctx context.Context, p Principal, req *sushiv1.HomeReq) (*sushiv1.HomeRes, error) {
	a.noteDevice(ctx, p.UserID, req.GetPlatform())
	tab, err := tabKind(req.GetTab())
	if err != nil {
		return nil, err
	}

	seq, err := a.rails.RailSeq(ctx)
	if err != nil {
		return nil, fmt.Errorf("home: %w", err)
	}

	hidden, hiddenProto := a.notInterested(ctx, p.UserID)

	// The client already has this build. Saying so costs a few bytes; resending three rails it
	// would discard costs a message it could have spent on something the user asked for.
	// not_interested still rides along: continue-watching is client-owned and would otherwise
	// keep a title the viewer just hid on another device.
	if req.GetSinceSeq() != 0 && req.GetSinceSeq() >= seq {
		res := &sushiv1.HomeRes{Seq: seq, TtlSeconds: HomeTTL, NotInterested: hiddenProto}
		a.attachLatestApp(ctx, p, req, res)
		return res, nil
	}

	kinds := []catalog.RailKind{catalog.RailSlider, catalog.RailMostWatched, catalog.RailTrending}
	rails := make([]*sushiv1.Rail, 0, len(kinds))
	seen := make(map[uint64]struct{})

	for _, kind := range kinds {
		rows, err := a.rails.Rail(ctx, kind, tab)
		if err != nil {
			return nil, fmt.Errorf("home: %w", err)
		}
		if kind == catalog.RailSlider {
			rows = a.prependLatestLater(ctx, p.UserID, tab, rows)
		}
		rows = dropNotInterested(rows, hidden)
		rows = dedupe(rows, seen)
		if len(rows) == 0 {
			// A rail with nothing in it is not sent. An empty row of cards reads as a bug, and the
			// client renders what it is given (R-RAIL-5).
			continue
		}
		rails = append(rails, &sushiv1.Rail{Kind: railKind(kind), Rows: protoRows(rows)})
	}

	res := &sushiv1.HomeRes{Rails: rails, Seq: seq, TtlSeconds: HomeTTL, NotInterested: hiddenProto}
	a.attachLatestApp(ctx, p, req, res)
	return res, nil
}

type hiddenTitle struct {
	id   uint64
	kind catalog.Kind
}

// notInterested loads titles this viewer hid from Home. A lookup error leaves the rails
// unfiltered: a home screen is better than a blank one.
func (a *API) notInterested(ctx context.Context, userID int64) (map[hiddenTitle]struct{}, []*sushiv1.NotInterestedTitle) {
	if a.browse == nil || userID == 0 {
		return nil, nil
	}
	rows, err := a.browse.ListNotInterested(ctx, userID)
	if err != nil || len(rows) == 0 {
		return nil, nil
	}
	set := make(map[hiddenTitle]struct{}, len(rows))
	out := make([]*sushiv1.NotInterestedTitle, 0, len(rows))
	for _, r := range rows {
		if r.TMDBID == 0 {
			continue
		}
		set[hiddenTitle{id: r.TMDBID, kind: r.Kind}] = struct{}{}
		out = append(out, &sushiv1.NotInterestedTitle{TmdbId: r.TMDBID, Kind: protoKind(r.Kind)})
	}
	return set, out
}

func dropNotInterested(rows []catalog.Row, hidden map[hiddenTitle]struct{}) []catalog.Row {
	if len(hidden) == 0 {
		return rows
	}
	out := make([]catalog.Row, 0, len(rows))
	for _, r := range rows {
		if _, ok := hidden[hiddenTitle{id: r.TMDBID, kind: r.Kind}]; ok {
			continue
		}
		out = append(out, r)
	}
	return out
}

// prependLatestLater puts this viewer's newest watch-later first on the slider so a news-channel
// tap lands on the home banner. Fail closed: a lookup error leaves the shared rail alone.
func (a *API) prependLatestLater(ctx context.Context, userID int64, tab catalog.Kind, rows []catalog.Row) []catalog.Row {
	if userID == 0 || a.browse == nil {
		return rows
	}
	later, ok, err := a.browse.LatestWatchLater(ctx, userID, tab)
	if err != nil || !ok || later.TMDBID == 0 {
		return rows
	}
	out := make([]catalog.Row, 0, len(rows)+1)
	out = append(out, later)
	for _, r := range rows {
		if r.TMDBID == later.TMDBID && r.Kind == later.Kind {
			continue
		}
		out = append(out, r)
	}
	return out
}

// dedupe drops rows already shown higher up the screen.
//
// R-RAIL-1: an item appears at most once on home. With a catalog of a few thousand titles the same
// film lands in three rails easily, and a home screen showing it three times reads as broken
// rather than popular.
func dedupe(rows []catalog.Row, seen map[uint64]struct{}) []catalog.Row {
	out := rows[:0:0]
	for _, r := range rows {
		if _, dup := seen[r.TMDBID]; dup {
			continue
		}
		seen[r.TMDBID] = struct{}{}
		out = append(out, r)
	}
	return out
}

func protoRows(rows []catalog.Row) []*sushiv1.Row {
	out := make([]*sushiv1.Row, len(rows))
	for i, r := range rows {
		out[i] = protoRow(r)
	}
	return out
}

func protoKind(k catalog.Kind) sushiv1.Kind {
	switch k {
	case catalog.KindMovie:
		return sushiv1.Kind_KIND_MOVIE
	case catalog.KindSeries:
		return sushiv1.Kind_KIND_SERIES
	case catalog.KindUnknown:
		return sushiv1.Kind_KIND_UNSPECIFIED
	default:
		return sushiv1.Kind_KIND_UNSPECIFIED
	}
}

func railKind(k catalog.RailKind) sushiv1.RailKind {
	switch k {
	case catalog.RailSlider:
		return sushiv1.RailKind_RAIL_KIND_SLIDER
	case catalog.RailMostWatched:
		return sushiv1.RailKind_RAIL_KIND_MOST_WATCHED
	case catalog.RailTrending:
		return sushiv1.RailKind_RAIL_KIND_TRENDING
	case catalog.RailUnknown:
		return sushiv1.RailKind_RAIL_KIND_UNSPECIFIED
	default:
		return sushiv1.RailKind_RAIL_KIND_UNSPECIFIED
	}
}

func tabKind(t sushiv1.Tab) (catalog.Kind, error) {
	switch t {
	case sushiv1.Tab_TAB_MOVIES:
		return catalog.KindMovie, nil
	case sushiv1.Tab_TAB_SERIES:
		return catalog.KindSeries, nil
	case sushiv1.Tab_TAB_UNSPECIFIED:
		// Defaulting would hide a client bug behind a screen that looks right.
		return catalog.KindUnknown, fmt.Errorf("home: tab not specified")
	default:
		return catalog.KindUnknown, fmt.Errorf("home: unknown tab %d", t)
	}
}
