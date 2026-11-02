package api

import (
	"context"
	"fmt"
	"unicode/utf8"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
)

// TitleHydrator loads a full title page from TMDB (ADR 0009). Declared by the consumer.
type TitleHydrator interface {
	TitlePage(ctx context.Context, tmdbID uint64, kind catalog.Kind) (catalog.Item, error)
}

// PersonHydrator loads person filmography from TMDB.
type PersonHydrator interface {
	PersonPage(ctx context.Context, personID uint64) (catalog.PersonPage, error)
}

// BrowseStore is env browse_titles + browse_people + list helpers.
type BrowseStore interface {
	BrowseItem(ctx context.Context, tmdbID uint64, kind catalog.Kind) (catalog.Item, bool, error)
	UpsertBrowseItem(ctx context.Context, item catalog.Item) error
	BrowsePerson(ctx context.Context, tmdbID uint64) (catalog.PersonPage, bool, error)
	UpsertBrowsePerson(ctx context.Context, page catalog.PersonPage) error
	CompactRowsAnyKind(ctx context.Context, ids []uint64) ([]catalog.Row, error)
	ListCatalog(ctx context.Context, kind catalog.Kind, sort int, sortDesc bool, genre string, year int, q string, limit, offset int32) ([]catalog.Row, error)
	// ListBoxsetMetas is one card per TMDB collection (LIST_SCOPE_BOXSETS): the collection's own
	// id/name, never a member movie's.
	ListBoxsetMetas(ctx context.Context, q string, limit, offset int32) ([]catalog.BoxsetMeta, error)
	// ListBoxsetItems is the members of one TMDB collection (LIST_SCOPE_BOXSET).
	ListBoxsetItems(ctx context.Context, collectionID uint64, limit, offset int32) ([]catalog.Row, error)
	ListUserFlags(ctx context.Context, userID int64, flag, q string, limit, offset int32) ([]catalog.Row, error)
	// ListForYou is followed/favourited series with an unfinished serveable episode (LIST_SCOPE_FOR_YOU).
	ListForYou(ctx context.Context, userID int64, limit, offset int32) ([]catalog.Row, error)
	// LatestWatchLater is the most recently filed watch-later of this kind, for the home slider.
	LatestWatchLater(ctx context.Context, userID int64, kind catalog.Kind) (catalog.Row, bool, error)
	// UserItemFlags is the favourite / watch-later pair for one title, for the title page
	// (ADR 0032). A user who has never touched it reads false, false.
	UserItemFlags(ctx context.Context, userID int64, tmdbID uint64, kind catalog.Kind) (favorite, watchLater bool, err error)
	SetFavorite(ctx context.Context, userID int64, tmdbID uint64, kind catalog.Kind, on bool) error
	SetWatchLater(ctx context.Context, userID int64, tmdbID uint64, kind catalog.Kind, on bool) error
	// SetNotInterested hides a title from Home. Search still returns it. on=false puts it back.
	SetNotInterested(ctx context.Context, userID int64, tmdbID uint64, kind catalog.Kind, on bool) error
	// ListNotInterested is every title this viewer hid from Home.
	ListNotInterested(ctx context.Context, userID int64) ([]catalog.Row, error)
	// SetFollow toggles a series follow (ADR 0014 §D3). Kind is always series, so it takes none.
	SetFollow(ctx context.Context, userID int64, tmdbID uint64, on bool) error
	// SetProgress upserts watch progress for one episode. Fire-and-forget; no reply.
	SetProgress(ctx context.Context, userID, episodeID, fileID int64, positionS, durationS int32) error
	// SetEpisodeDone is a manual mark (WatchedEvent). Position stays 0; no play_event.
	SetEpisodeDone(ctx context.Context, userID, episodeID int64, done bool) error
	// EpisodeProgress is the viewer's resume for one episode, for /files.
	EpisodeProgress(ctx context.Context, userID, episodeID int64) (catalog.EpisodeProgress, bool, error)
	// WatchedStateSince is the cross-device sync delta (docs/11 §6.1): rows changed since watermark.
	WatchedStateSince(ctx context.Context, userID, watermark int64, limit int32) ([]catalog.WatchedState, error)
	ListPlaylists(ctx context.Context, userID int64) ([]catalog.PlaylistMeta, error)
	ListPlaylistItems(ctx context.Context, playlistID int64, limit, offset int32) ([]catalog.Row, error)
}

const listPageSize = 40

// Person answers must fit a Telegram text envelope (wire.MaxTextPayload). Full TMDB bios plus
// 20+20 filmography rows overflow; document send is not wired yet on client or apid. Cap here
// (R-META-6) so /person always returns a readable text message.
const (
	personFilmographyCap = 12
	personBiographyCap   = 800
	// ItemRes extras must stay inside wire.MaxTextPayload even on a long-running series
	// (ADR 0028). Related/collection/people/overview used to blow the envelope after the
	// episode tree was already thinned.
	itemRelatedCap    = 8
	itemCollectionCap = 8
	itemPeopleCap     = 8
	itemOverviewCap   = 600
)

// itemToRes maps a domain Item onto ItemRes.
func itemToRes(item catalog.Item) *sushiv1.ItemRes {
	seasons, episodes := wireSeasonTree(item)
	var releasedOn int64
	if !item.ReleasedOn.IsZero() {
		releasedOn = item.ReleasedOn.Unix()
	}
	return &sushiv1.ItemRes{
		Row:            protoRow(item.Row),
		Overview:       clipRunes(item.Overview, itemOverviewCap),
		ReleasedOn:     releasedOn,
		Episodes:       episodes,
		Logo:           catalog.WireLogo(item.Detail.Logo),
		Backdrop:       item.Detail.Backdrop,
		Genres:         item.Detail.Genres,
		RuntimeS:       item.Detail.RuntimeS,
		People:         protoPeople(clipPeople(item.Detail.People, itemPeopleCap)),
		Related:        protoRows(clipRows(item.Related, itemRelatedCap)),
		CollectionName: item.Detail.CollectionName,
		Collection:     protoRows(clipRows(item.Collection, itemCollectionCap)),
		Seasons:        seasons,
		TrailerKey:     catalog.WireTrailer(item.Detail.TrailerKey),
		Country:        catalog.WireCountry(item.Detail.Country),
	}
}

func clipPeople(people []catalog.Person, n int) []catalog.Person {
	if n < 0 || len(people) <= n {
		return people
	}
	return people[:n]
}

func wireSeasonTree(item catalog.Item) ([]*sushiv1.Season, []*sushiv1.Episode) {
	if item.Kind != catalog.KindSeries {
		return nil, protoEpisodes(item.Episodes)
	}
	return seasonIndex(item.Episodes), protoEpisodes(playTarget(item.Episodes))
}

func playTarget(episodes []catalog.Episode) []catalog.Episode {
	for _, e := range episodes {
		if e.SeasonNo > 0 {
			return []catalog.Episode{e}
		}
	}
	if len(episodes) == 0 {
		return nil
	}
	return episodes[:1]
}

func seasonIndex(episodes []catalog.Episode) []*sushiv1.Season {
	if len(episodes) == 0 {
		return nil
	}
	out := []*sushiv1.Season{}
	var cur *sushiv1.Season
	for _, e := range episodes {
		if cur == nil || cur.SeasonNo != e.SeasonNo {
			cur = &sushiv1.Season{SeasonNo: e.SeasonNo}
			out = append(out, cur)
		}
		cur.EpisodeCount++
	}
	return out
}

func protoEpisodes(episodes []catalog.Episode) []*sushiv1.Episode {
	out := make([]*sushiv1.Episode, len(episodes))
	for i, e := range episodes {
		out[i] = protoEpisode(e)
	}
	return out
}

func preferCatalogRows(preview []catalog.Row, catalogRows []catalog.Row) []catalog.Row {
	byID := map[uint64]catalog.Row{}
	for _, r := range catalogRows {
		byID[r.TMDBID] = r
	}
	out := make([]catalog.Row, 0, len(preview))
	seen := map[uint64]struct{}{}
	for _, r := range preview {
		if r.TMDBID == 0 {
			continue
		}
		if _, ok := seen[r.TMDBID]; ok {
			continue
		}
		seen[r.TMDBID] = struct{}{}
		if c, ok := byID[r.TMDBID]; ok {
			out = append(out, c)
			continue
		}
		out = append(out, r)
	}
	return out
}

func idsOf(rows []catalog.Row) []uint64 {
	out := make([]uint64, 0, len(rows))
	for _, r := range rows {
		if r.TMDBID > 0 {
			out = append(out, r.TMDBID)
		}
	}
	return out
}

// Person answers /person (ADR 0009): browse_people cache, else TMDB + upsert.
func (a *API) Person(ctx context.Context, _ Principal, req *sushiv1.PersonReq) (*sushiv1.PersonRes, error) {
	if req.GetTmdbId() == 0 {
		return nil, fmt.Errorf("person: tmdb_id required")
	}
	page, err := a.personFromBrowse(ctx, req.GetTmdbId())
	if err != nil {
		return nil, err
	}
	if page.Name == "" && len(page.Movies) == 0 && len(page.Series) == 0 {
		return nil, ErrNotFound
	}
	movies, series := page.Movies, page.Series
	if a.browse != nil {
		all := append(idsOf(movies), idsOf(series)...)
		hits, err := a.browse.CompactRowsAnyKind(ctx, all)
		if err != nil {
			return nil, fmt.Errorf("person: %w", err)
		}
		movies = preferCatalogRows(movies, hits)
		series = preferCatalogRows(series, hits)
	}
	movies = clipRows(movies, personFilmographyCap)
	series = clipRows(series, personFilmographyCap)
	return &sushiv1.PersonRes{
		TmdbId:    page.TMDBID,
		Name:      page.Name,
		Profile:   page.Profile,
		Biography: clipRunes(page.Biography, personBiographyCap),
		Movies:    protoRows(movies),
		Series:    protoRows(series),
	}, nil
}

func boxsetMetasToRes(metas []catalog.BoxsetMeta) []*sushiv1.BoxsetMeta {
	out := make([]*sushiv1.BoxsetMeta, len(metas))
	for i, m := range metas {
		out[i] = &sushiv1.BoxsetMeta{
			CollectionId: int64(m.CollectionID), Name: m.Name, Poster: m.Poster, ItemCount: m.ItemCount,
		}
	}
	return out
}

func clipRows(rows []catalog.Row, n int) []catalog.Row {
	if n < 0 || len(rows) <= n {
		return rows
	}
	return rows[:n]
}

func clipRunes(s string, n int) string {
	if n <= 0 || s == "" {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

func (a *API) personFromBrowse(ctx context.Context, tmdbID uint64) (catalog.PersonPage, error) {
	if a.browse != nil {
		cached, ok, err := a.browse.BrowsePerson(ctx, tmdbID)
		if err != nil {
			return catalog.PersonPage{}, fmt.Errorf("person: %w", err)
		}
		if ok && (cached.Name != "" || len(cached.Movies) > 0 || len(cached.Series) > 0) {
			return cached, nil
		}
	}
	if a.person == nil {
		return catalog.PersonPage{}, nil
	}
	page, err := a.person.PersonPage(ctx, tmdbID)
	if err != nil {
		return catalog.PersonPage{}, fmt.Errorf("person: %w", err)
	}
	if page.Name == "" && len(page.Movies) == 0 && len(page.Series) == 0 {
		return catalog.PersonPage{}, nil
	}
	if page.TMDBID == 0 {
		page.TMDBID = tmdbID
	}
	if a.browse != nil {
		if err := a.browse.UpsertBrowsePerson(ctx, page); err != nil {
			return catalog.PersonPage{}, fmt.Errorf("person: %w", err)
		}
	}
	return page, nil
}

// List answers /list (ADR 0009).
func (a *API) List(ctx context.Context, p Principal, req *sushiv1.ListReq) (*sushiv1.ListRes, error) {
	offset := int32(req.GetCursor())
	limit := int32(listPageSize)
	if req.GetScope() == sushiv1.ListScope_LIST_SCOPE_MY_VIDEOS {
		return a.listMyVideos(ctx, p, limit, offset)
	}
	if a.browse == nil {
		return &sushiv1.ListRes{}, nil
	}
	sort, sortDesc := catalogListSort(req.GetScope(), int(req.GetSort()), req.GetSortDesc())
	genre, year, q := req.GetGenre(), int(req.GetYear()), req.GetQ()
	rows, res, err := a.listByScope(ctx, p, req, sort, sortDesc, genre, year, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}
	if res != nil {
		return res, nil
	}
	var next uint32
	if len(rows) >= listPageSize {
		next = uint32(offset) + uint32(len(rows))
	}
	return &sushiv1.ListRes{Rows: protoRows(rows), Cursor: next}, nil
}

// listByScope resolves one /list scope to either plain rows, or (for scopes with their own card
// shape — BOXSETS, PLAYLISTS) a fully-built ListRes returned early.
func (a *API) listByScope(
	ctx context.Context, p Principal, req *sushiv1.ListReq,
	sort int, sortDesc bool, genre string, year int, q string, limit, offset int32,
) ([]catalog.Row, *sushiv1.ListRes, error) {
	switch req.GetScope() {
	case sushiv1.ListScope_LIST_SCOPE_MOVIES:
		rows, err := a.browse.ListCatalog(ctx, catalog.KindMovie, sort, sortDesc, genre, year, q, limit, offset)
		return rows, nil, err
	case sushiv1.ListScope_LIST_SCOPE_SERIES:
		rows, err := a.browse.ListCatalog(ctx, catalog.KindSeries, sort, sortDesc, genre, year, q, limit, offset)
		return rows, nil, err
	case sushiv1.ListScope_LIST_SCOPE_BOXSETS:
		metas, err := a.browse.ListBoxsetMetas(ctx, q, limit, offset)
		if err != nil {
			return nil, nil, err
		}
		return nil, &sushiv1.ListRes{Boxsets: boxsetMetasToRes(metas)}, nil
	case sushiv1.ListScope_LIST_SCOPE_BOXSET:
		if req.GetCollectionId() == 0 {
			return nil, nil, fmt.Errorf("collection_id required")
		}
		rows, err := a.browse.ListBoxsetItems(ctx, uint64(req.GetCollectionId()), limit, offset)
		return rows, nil, err
	case sushiv1.ListScope_LIST_SCOPE_FAVORITES:
		rows, err := a.browse.ListUserFlags(ctx, p.UserID, "favorite", q, limit, offset)
		return rows, nil, err
	case sushiv1.ListScope_LIST_SCOPE_LATER:
		rows, err := a.browse.ListUserFlags(ctx, p.UserID, "later", q, limit, offset)
		return rows, nil, err
	case sushiv1.ListScope_LIST_SCOPE_FOR_YOU:
		rows, err := a.browse.ListForYou(ctx, p.UserID, limit, offset)
		return rows, nil, err
	case sushiv1.ListScope_LIST_SCOPE_PLAYLISTS:
		metas, err := a.browse.ListPlaylists(ctx, p.UserID)
		if err != nil {
			return nil, nil, err
		}
		out := make([]*sushiv1.PlaylistMeta, len(metas))
		for i, m := range metas {
			out[i] = &sushiv1.PlaylistMeta{PlaylistId: m.ID, Name: m.Name, ItemCount: m.ItemCount}
		}
		return nil, &sushiv1.ListRes{Playlists: out}, nil
	case sushiv1.ListScope_LIST_SCOPE_PLAYLIST:
		if req.GetPlaylistId() == 0 {
			return nil, nil, fmt.Errorf("playlist_id required")
		}
		rows, err := a.browse.ListPlaylistItems(ctx, req.GetPlaylistId(), limit, offset)
		return rows, nil, err
	case sushiv1.ListScope_LIST_SCOPE_UNSPECIFIED:
		return nil, nil, fmt.Errorf("scope not specified")
	default:
		return nil, nil, fmt.Errorf("unknown scope %d", req.GetScope())
	}
}

// catalogListSort: Movies/Series with no sort become YEAR desc (premiere / newest episode).
func catalogListSort(scope sushiv1.ListScope, sort int, desc bool) (int, bool) {
	if sort != 0 {
		return sort, desc
	}
	if scope == sushiv1.ListScope_LIST_SCOPE_MOVIES || scope == sushiv1.ListScope_LIST_SCOPE_SERIES {
		return int(sushiv1.ListSort_LIST_SORT_YEAR), true
	}
	return int(sushiv1.ListSort_LIST_SORT_NAME), false
}

// Events applies fire-and-forget fav / later / follow / prog / watched writes (docs/11, ADR 0014 §D3). The
// client never waits for a reply, but a storage failure is still worth surfacing: the transport
// logs the returned error rather than dropping it. One bad event never stops the rest of the batch.
func (a *API) Events(ctx context.Context, p Principal, req *sushiv1.Events) error {
	if a.browse == nil && a.personal == nil {
		return nil
	}
	var firstErr error
	for _, ev := range req.GetEvents() {
		if err := a.applyEvent(ctx, p, ev); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (a *API) applyEvent(ctx context.Context, p Principal, ev *sushiv1.Event) error {
	if handled, err := a.applyPersonalEvent(ctx, p, ev); handled {
		return err
	}
	if a.browse == nil {
		return nil
	}
	switch body := ev.GetBody().(type) {
	case *sushiv1.Event_Fav:
		return a.setTitleFlag(ctx, p.UserID, body.Fav.GetKind(), body.Fav.GetTmdbId(), body.Fav.GetOn(), a.browse.SetFavorite)
	case *sushiv1.Event_Later:
		return a.setTitleFlag(ctx, p.UserID, body.Later.GetKind(), body.Later.GetTmdbId(), body.Later.GetOn(), a.browse.SetWatchLater)
	case *sushiv1.Event_NotInterested:
		return a.setTitleFlag(ctx, p.UserID, body.NotInterested.GetKind(), body.NotInterested.GetTmdbId(), body.NotInterested.GetOn(), a.browse.SetNotInterested)
	case *sushiv1.Event_Follow:
		// Follow is series-only, so there is no kind to validate.
		return a.browse.SetFollow(ctx, p.UserID, body.Follow.GetTmdbId(), body.Follow.GetOn())
	case *sushiv1.Event_Prog:
		return a.browse.SetProgress(ctx, p.UserID, body.Prog.GetEpisodeId(), body.Prog.GetFileId(),
			body.Prog.GetPositionS(), body.Prog.GetDurationS())
	case *sushiv1.Event_Watched:
		return a.browse.SetEpisodeDone(ctx, p.UserID, body.Watched.GetEpisodeId(), body.Watched.GetDone())
	default:
		return nil
	}
}

func (a *API) setTitleFlag(
	ctx context.Context, userID int64, kind sushiv1.Kind, tmdbID uint64, on bool,
	set func(context.Context, int64, uint64, catalog.Kind, bool) error,
) error {
	parsed, err := itemKind(kind)
	if err != nil {
		return nil //nolint:nilerr // unknown kind is skipped; one bad event must not fail the batch
	}
	return set(ctx, userID, tmdbID, parsed, on)
}

// watchedSyncPageSize caps one Sync reply so a heavy watcher's whole history still fits a single
// text envelope. SyncRes.watched_more tells the client to call again immediately rather than wait
// for the next launch (docs/11 §6.1).
const watchedSyncPageSize = 500

// Sync answers the cross-device watched-state delta (docs/11 §6.1). It is v1 of the MSG_TYPE_SYNC
// slot: catalog delta (docs/11 §6) is not built and is not part of this reply.
func (a *API) Sync(ctx context.Context, p Principal, req *sushiv1.SyncReq) (*sushiv1.SyncRes, error) {
	if a.browse == nil {
		out := &sushiv1.SyncRes{WatchedWatermark: req.GetWatchedWatermark()}
		return out, a.addPlaybackPrefs(ctx, p.UserID, out)
	}
	rows, err := a.browse.WatchedStateSince(ctx, p.UserID, req.GetWatchedWatermark(), watchedSyncPageSize+1)
	if err != nil {
		return nil, err
	}
	more := len(rows) > watchedSyncPageSize
	if more {
		rows = rows[:watchedSyncPageSize]
	}
	out := &sushiv1.SyncRes{
		Watched:          make([]*sushiv1.WatchedState, len(rows)),
		WatchedWatermark: req.GetWatchedWatermark(),
		WatchedMore:      more,
	}
	for i, r := range rows {
		w := &sushiv1.WatchedState{Done: r.Done}
		if r.Kind == catalog.MediaPersonal {
			w.PersonalId = r.EpisodeID
		} else {
			w.EpisodeId = r.EpisodeID
		}
		out.Watched[i] = w
	}
	if len(rows) > 0 {
		out.WatchedWatermark = rows[len(rows)-1].Seq
	}
	return out, a.addPlaybackPrefs(ctx, p.UserID, out)
}
