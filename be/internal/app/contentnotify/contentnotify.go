// Package contentnotify is main-bot's background reconciler for content requests (ADR 0014 §D4).
//
// It runs one ticker with three passes: tell admins about a new request, notice when the catalog
// can finally serve a requested title, and tell the people who asked. Every pass is best-effort —
// a Telegram send that fails is logged and retried on the next tick, never fatal (R-GO-10).
//
// It lives in main-bot because both DM directions are prose, and main-bot is the only
// customer-facing bot (ADR 0004 R-VOICE-1).
package contentnotify

import (
	"context"
	"log/slog"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

const (
	defaultInterval = 45 * time.Second
	defaultBatch    = 20
	// defaultDebounce is the quiet period a followed series must go without a new episode before
	// its followers are told (ADR 0014 §D3) — so a bulk import of 20 episodes is one DM, not 20.
	defaultDebounce = 15 * time.Minute
)

// AdminAlert is a new request no admin has been told about yet. Year and Rating come from
// title_cards, so they are zero for a title nothing has hydrated yet — the card drops them.
type AdminAlert struct {
	ID                int64
	TMDBID            uint64
	Kind              catalog.Kind
	Title             string
	Poster            string
	Year              int32
	Rating            int32
	IMDBID            string
	RequesterTG       int64
	RequesterName     string
	RequesterUsername string
	// SubtitleLang is the requester's taste answer (ISO 639-1), empty when they never picked one.
	SubtitleLang string
}

// Admin is one recipient of the new-request alert, with the locale their copy is written in.
type Admin struct {
	TelegramID int64
	Locale     string
}

// Target is one (tmdb_id, kind) that at least one pending request is waiting on.
type Target struct {
	TMDBID uint64
	Kind   catalog.Kind
}

// RequesterNotice is a fulfilled request whose requester has not had the good news yet.
type RequesterNotice struct {
	ID          int64
	UserID      int64
	RequesterTG int64
	TMDBID      uint64
	Kind        catalog.Kind
	Title       string
	Poster      string
	Year        int32
	Rating      int32
	// Locale is the requester's, so the DM is written in one language instead of two.
	Locale string
}

// FollowedSeries is one series with a follower, plus the max episode the reconciler last recorded.
type FollowedSeries struct {
	TMDBID    uint64
	MaxSeason int32
	MaxEp     int32
}

// FollowNotice is one follower due a "new episode" DM: their series has a higher max than they
// have seen and has been quiet past the debounce window.
type FollowNotice struct {
	UserID      int64
	RequesterTG int64
	TMDBID      uint64
	MaxSeason   int32
	MaxEp       int32
	Title       string
	Poster      string
	Year        int32
	NewCount    int32
	Locale      string
}

// Store is the persistence the reconciler needs. Declared here, by the consumer, and small
// (R-GO-5). *pg.Store satisfies it.
type Store interface {
	PendingRequestAdminAlerts(ctx context.Context, limit int32) ([]AdminAlert, error)
	AdminRecipients(ctx context.Context) ([]Admin, error)
	MarkRequestAdminsNotified(ctx context.Context, id int64) error
	MarkRequestChannelPosted(ctx context.Context, id, messageID int64) error
	SettledRequestChannelPosts(ctx context.Context, limit int32) ([]ChannelCard, error)
	ClearRequestChannelMessage(ctx context.Context, id int64) error

	// FulfillPlayableRequests marks pending rows whose requester can actually open a file.
	// A hardsub-only title stays open for a non-Persian subtitle language.
	FulfillPlayableRequests(ctx context.Context, limit int32) (int, error)

	FulfilledRequestsToNotify(ctx context.Context, limit int32) ([]RequesterNotice, error)
	MarkRequesterNotified(ctx context.Context, id int64) error

	// Series follow (ADR 0014 §D3).
	FollowedSeriesToScan(ctx context.Context, limit int32) ([]FollowedSeries, error)
	SeriesMaxServeableEpisode(ctx context.Context, tmdbID uint64) (season, episode int32, err error)
	BumpFollowedSeriesMax(ctx context.Context, tmdbID uint64, season, episode int32) (moved bool, err error)
	FollowNoticesDue(ctx context.Context, cutoff time.Time, limit int32) ([]FollowNotice, error)
	AdvanceFollowSeen(ctx context.Context, userID int64, tmdbID uint64, season, episode int32) error
	// SyncFollowSeenToSeriesMax dedups a "request ready" DM against the follow reconciler.
	SyncFollowSeenToSeriesMax(ctx context.Context, userID int64, tmdbID uint64) error
}

// ChannelCard is a settled request whose staff-channel post still has its action keyboard.
type ChannelCard struct {
	ID        int64
	MessageID int64
	Status    int16
	Title     string
	TMDBID    uint64
	Kind      catalog.Kind
}

// ForumTopics are the three topic thread ids of the staff request supergroup (ADR 0036).
// A partial set is off: a card must not land in open when reject and done cannot receive it.
type ForumTopics struct {
	Open   int64
	Reject int64
	Done   int64
}

// Enabled reports whether all three topics are known.
func (t ForumTopics) Enabled() bool {
	return t.Open > 0 && t.Reject > 0 && t.Done > 0
}

// AlertPost is one request card with reject / re-check buttons (ADR 0023). The adapter in
// main-bot attaches the keyboard; this package never imports Telegram. ThreadID is the open
// topic when the inbox is a forum; zero posts into the chat itself (a channel, or an admin DM).
type AlertPost struct {
	ChatID    int64
	ThreadID  int64
	PhotoURL  string
	Caption   string
	RequestID int64
	Locale    string
}

// Messenger sends one prose DM. *tg.Client satisfies it through a thin adapter in main-bot (this
// package stays free of any Telegram import). SendPhotoHTML posts a photo by URL with an HTML
// caption; callers fall back to SendHTML when it fails or there is no poster. PostAlert is the
// staff-facing card (channel or admin DM) with action buttons.
type Messenger interface {
	SendHTML(ctx context.Context, chatID int64, text string) error
	SendPhotoHTML(ctx context.Context, chatID int64, photoURL, caption string) error
	PostAlert(ctx context.Context, p AlertPost) (messageID int64, err error)
	// CloseAlert ends a staff card. threadID non-zero moves it into that forum topic
	// (ADR 0036); zero edits the caption in place and strips the keyboard.
	CloseAlert(ctx context.Context, chatID, messageID int64, outcome string, threadID int64) error
}

// posterSize is the width the app itself asks for on a detail header (doc 12 §5.1), so a poster
// Telegram has already cached for one surface serves the other.
const posterSize = "w500"

// dmRich sends a captioned photo when there is a poster, falling back to a plain HTML DM — both
// when there is no poster and when the photo send fails, because a TMDB URL Telegram will not
// fetch must not swallow the notification itself.
func (r *Reconciler) dmRich(ctx context.Context, chatID int64, posterPath, body string) error {
	if url := catalog.PosterURL(posterPath, posterSize); url != "" {
		if err := r.dm.SendPhotoHTML(ctx, chatID, url, body); err == nil {
			return nil
		}
	}
	return r.dm.SendHTML(ctx, chatID, body)
}

// Reconciler is the ticker and its passes.
type Reconciler struct {
	store     Store
	dm        Messenger
	log       *slog.Logger
	interval  time.Duration
	batch     int32
	debounce  time.Duration
	channelID int64
	topics    ForumTopics
}

// WithForum tells the reconciler which topic a new card is posted into, and where a settled
// card moves (ADR 0036). A disabled set keeps the single-feed channel behaviour.
func (r *Reconciler) WithForum(t ForumTopics) *Reconciler {
	r.topics = t
	return r
}

// New builds a Reconciler with the default 45s cadence. A zero debounce falls back to 15 minutes
// (ADR 0014 §D3). channelID is the staff inbox (ADR 0023); zero keeps per-admin DMs.
func New(store Store, dm Messenger, log *slog.Logger, debounce time.Duration, channelID int64) *Reconciler {
	if debounce <= 0 {
		debounce = defaultDebounce
	}
	return &Reconciler{
		store: store, dm: dm, log: log,
		interval: defaultInterval, batch: defaultBatch, debounce: debounce,
		channelID: channelID,
	}
}

// Run ticks until ctx is cancelled. It is one of main-bot's errgroup goroutines (R-GO-11).
func (r *Reconciler) Run(ctx context.Context) error {
	r.log.Info("content-request reconciler started", "interval", r.interval)
	t := time.NewTicker(r.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			r.tick(ctx)
		}
	}
}

func (r *Reconciler) tick(ctx context.Context) {
	r.alertAdmins(ctx)
	r.detectFulfilment(ctx)
	r.closeSettledChannelPosts(ctx)
	r.notifyRequesters(ctx)
	r.scanFollowedSeries(ctx)
	r.flushFollowNotices(ctx)
}

// detectFulfilment flips pending requests the requester can actually play.
func (r *Reconciler) detectFulfilment(ctx context.Context) {
	n, err := r.store.FulfillPlayableRequests(ctx, r.batch)
	if err != nil {
		r.log.Warn("content-request fulfilment", "err", err)
		return
	}
	if n > 0 {
		r.log.Info("content request fulfilled", "requests", n)
	}
}

// notifyRequesters DMs each requester once their title is serveable.
func (r *Reconciler) notifyRequesters(ctx context.Context) {
	notices, err := r.store.FulfilledRequestsToNotify(ctx, r.batch)
	if err != nil {
		r.log.Warn("content-request requester notice: list", "err", err)
		return
	}
	for _, n := range notices {
		if ctx.Err() != nil {
			return
		}
		if err := r.dmRich(ctx, n.RequesterTG, n.Poster, formatRequesterNotice(n)); err != nil {
			r.log.Warn("content-request requester notice: send", "err", err, "request", n.ID)
			continue
		}
		if err := r.store.MarkRequesterNotified(ctx, n.ID); err != nil {
			r.log.Warn("content-request requester notice: mark", "err", err, "request", n.ID)
		}
		// A series request also created a follow (ADR 0014 §D2). Catch it up so the follow flush
		// does not immediately send a near-duplicate "new episode" DM.
		if n.Kind == catalog.KindSeries {
			if err := r.store.SyncFollowSeenToSeriesMax(ctx, n.UserID, n.TMDBID); err != nil {
				r.log.Warn("content-request requester notice: sync follow", "err", err, "request", n.ID)
			}
		}
	}
}

// scanFollowedSeries records, for every followed series, the highest episode that now has a ready
// file. A rise resets the shared debounce clock (BumpFollowedSeriesMax).
func (r *Reconciler) scanFollowedSeries(ctx context.Context) {
	series, err := r.store.FollowedSeriesToScan(ctx, r.batch)
	if err != nil {
		r.log.Warn("follow scan: list", "err", err)
		return
	}
	for _, s := range series {
		if ctx.Err() != nil {
			return
		}
		season, episode, err := r.store.SeriesMaxServeableEpisode(ctx, s.TMDBID)
		if err != nil {
			r.log.Warn("follow scan: max episode", "err", err, "tmdb", s.TMDBID)
			continue
		}
		if season == s.MaxSeason && episode == s.MaxEp {
			continue
		}
		if _, err := r.store.BumpFollowedSeriesMax(ctx, s.TMDBID, season, episode); err != nil {
			r.log.Warn("follow scan: bump", "err", err, "tmdb", s.TMDBID)
		}
	}
}

// flushFollowNotices DMs each follower once their series has been quiet for the debounce window,
// naming the latest episode and how many more arrived with it.
func (r *Reconciler) flushFollowNotices(ctx context.Context) {
	cutoff := time.Now().Add(-r.debounce)
	notices, err := r.store.FollowNoticesDue(ctx, cutoff, r.batch)
	if err != nil {
		r.log.Warn("follow flush: list", "err", err)
		return
	}
	for _, n := range notices {
		if ctx.Err() != nil {
			return
		}
		if err := r.dmRich(ctx, n.RequesterTG, n.Poster, formatFollowNotice(n)); err != nil {
			r.log.Warn("follow flush: send", "err", err, "tmdb", n.TMDBID, "user", n.UserID)
			continue
		}
		if err := r.store.AdvanceFollowSeen(ctx, n.UserID, n.TMDBID, n.MaxSeason, n.MaxEp); err != nil {
			r.log.Warn("follow flush: advance", "err", err, "tmdb", n.TMDBID, "user", n.UserID)
		}
	}
}
