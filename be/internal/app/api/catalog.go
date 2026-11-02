package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
)

// ErrNotFound means the requested item is not in our catalog. Maps onto the protocol's own
// ERR_CODE_NOT_FOUND (protocol.proto), reserved since the start but never used until now.
var ErrNotFound = errors.New("api: not found")

// CatalogStore is what Item, Files, and Episodes need from storage. Declared here, by the
// consumer, and small (R-GO-5).
type CatalogStore interface {
	Item(ctx context.Context, tmdbID uint64, kind catalog.Kind) (catalog.Item, bool, error)
	Files(ctx context.Context, episodeID int64) ([]catalog.File, error)
	EpisodesForSeason(ctx context.Context, tmdbID uint64, kind catalog.Kind, seasonNo, limit, offset int32) ([]catalog.Episode, int32, error)
	FirstPlayableEpisode(ctx context.Context, tmdbID uint64, kind catalog.Kind) (catalog.Episode, bool, error)
}

// Item opens a title: the page fetched once and cached until a catalog delta touches it
// (docs/12 §4). Misses and incomplete extras hydrate from TMDB (ADR 0009).
func (a *API) Item(ctx context.Context, p Principal, req *sushiv1.ItemReq) (*sushiv1.ItemRes, error) {
	kind, err := itemKind(req.GetKind())
	if err != nil {
		return nil, err
	}

	item, found, err := a.catalog.Item(ctx, req.GetTmdbId(), kind)
	if err != nil {
		return nil, fmt.Errorf("item: %w", err)
	}
	fromCatalog := found
	if !found {
		item, found, err = a.itemFromBrowse(ctx, req.GetTmdbId(), kind)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, ErrNotFound
		}
	}
	if fromCatalog {
		item = a.enrichCatalogItem(ctx, item)
	}

	res := itemToRes(item)
	if err := a.applySeriesPlayTarget(ctx, item, res); err != nil {
		return nil, err
	}
	res.Favorite, res.WatchLater = a.userFlags(ctx, p.UserID, req.GetTmdbId(), kind)
	return res, nil
}

// applySeriesPlayTarget replaces the first-by-S/E stub with the first episode that has a ready
// file. One Piece S01E01 may not be ingested; Request-vs-Play keys off /files for this id.
func (a *API) applySeriesPlayTarget(ctx context.Context, item catalog.Item, res *sushiv1.ItemRes) error {
	if item.Kind != catalog.KindSeries {
		return nil
	}
	ep, ok, err := a.catalog.FirstPlayableEpisode(ctx, item.TMDBID, item.Kind)
	if err != nil {
		return fmt.Errorf("item play-target: %w", err)
	}
	if ok {
		res.Episodes = []*sushiv1.Episode{protoEpisode(ep)}
	}
	return nil
}

// userFlags reads the viewer's own bookmark state for this title (ADR 0032).
//
// A failure here is answered false, false rather than failing the whole title page: an unreadable
// bookmark icon is a cosmetic loss, and a page that will not open is not. An unbound principal
// (UserID zero) has no flags to read.
func (a *API) userFlags(ctx context.Context, userID int64, tmdbID uint64, kind catalog.Kind) (favorite, watchLater bool) {
	if userID == 0 || a.browse == nil {
		return false, false
	}
	fav, later, err := a.browse.UserItemFlags(ctx, userID, tmdbID, kind)
	if err != nil {
		return false, false
	}
	return fav, later
}

func (a *API) itemFromBrowse(ctx context.Context, tmdbID uint64, kind catalog.Kind) (catalog.Item, bool, error) {
	var have catalog.Item
	had := false
	if a.browse != nil {
		item, found, err := a.browse.BrowseItem(ctx, tmdbID, kind)
		if err != nil {
			return catalog.Item{}, false, fmt.Errorf("item: %w", err)
		}
		if found && !catalog.DetailNeedsHydrate(item.Detail) {
			return item, true, nil
		}
		have, had = item, found
	}
	if a.hydrate == nil {
		return have, had, nil
	}
	item, err := a.hydrate.TitlePage(ctx, tmdbID, kind)
	if err != nil {
		if had {
			return have, true, nil
		}
		return catalog.Item{}, false, fmt.Errorf("item: %w", err)
	}
	if item.TMDBID == 0 || item.Title == "" {
		return have, had, nil
	}
	if a.browse != nil {
		return a.storeHydratedItem(ctx, tmdbID, kind, item)
	}
	return a.applyRelated(ctx, item), true, nil
}

func (a *API) storeHydratedItem(ctx context.Context, tmdbID uint64, kind catalog.Kind, item catalog.Item) (catalog.Item, bool, error) {
	if item.Detail.Logo == "" {
		item.Detail = catalog.MarkLogoAbsent(item.Detail)
	}
	if item.Detail.Country == "" {
		item.Detail = catalog.MarkCountryAbsent(item.Detail)
	}
	if item.Detail.TrailerKey == "" {
		item.Detail = catalog.MarkTrailerAbsent(item.Detail)
	}
	if item.Detail.Network == "" {
		item.Detail = catalog.MarkNetworkAbsent(item.Detail)
	}
	if err := a.browse.UpsertBrowseItem(ctx, item); err != nil {
		return catalog.Item{}, false, fmt.Errorf("item: %w", err)
	}
	if again, ok, err := a.browse.BrowseItem(ctx, tmdbID, kind); err == nil && ok {
		return again, true, nil
	}
	return a.applyRelated(ctx, item), true, nil
}

// enrichCatalogItem fills missing people/related/logo from TMDB in-memory (R-LIB-1: no library write).
func (a *API) enrichCatalogItem(ctx context.Context, item catalog.Item) catalog.Item {
	if a.hydrate == nil || !catalog.DetailNeedsHydrate(item.Detail) {
		return item
	}
	tm, err := a.hydrate.TitlePage(ctx, item.TMDBID, item.Kind)
	if err != nil || tm.TMDBID == 0 {
		return item
	}
	item.Detail = catalog.MergeDetail(item.Detail, tm.Detail)
	if item.Detail.Logo == "" {
		item.Detail = catalog.MarkLogoAbsent(item.Detail)
	}
	if item.Detail.Country == "" {
		item.Detail = catalog.MarkCountryAbsent(item.Detail)
	}
	if item.Detail.TrailerKey == "" {
		item.Detail = catalog.MarkTrailerAbsent(item.Detail)
	}
	if item.Detail.Network == "" {
		item.Detail = catalog.MarkNetworkAbsent(item.Detail)
	}
	if item.Overview == "" {
		item.Overview = tm.Overview
	}
	return a.applyRelated(ctx, item)
}

func (a *API) applyRelated(ctx context.Context, item catalog.Item) catalog.Item {
	var hits []catalog.Row
	if a.browse != nil && len(item.Detail.SimilarTMDB) > 0 {
		rows, err := a.browse.CompactRowsAnyKind(ctx, item.Detail.SimilarTMDB)
		if err == nil {
			hits = rows
		}
	}
	item.Related = catalog.ComposeRelated(item.Detail, hits)
	return item
}

// Files answers one episode's pick-list: its own request, its own short TTL, deliberately split
// from Item because the description is immutable and the file list is not (docs/12 §5). When the
// caller is bound, it also carries that viewer's resume for this episode if one exists.
func (a *API) Files(ctx context.Context, p Principal, req *sushiv1.FilesReq) (*sushiv1.FilesRes, error) {
	files, err := a.catalog.Files(ctx, req.GetEpisodeId())
	if err != nil {
		return nil, fmt.Errorf("files: %w", err)
	}

	out := make([]*sushiv1.File, len(files))
	for i, f := range files {
		out[i] = &sushiv1.File{
			FileId:       f.ID,
			QualityLabel: f.QualityLabel,
			DisplayName:  f.DisplayName,
			Height:       f.Height,
			AudioLangs:   f.AudioLangs,
			SubLangs:     f.SubLangs,
			SizeBytes:    f.SizeBytes,
			DurationS:    f.DurationS,
			State:        fileState(f.State),
		}
	}
	res := &sushiv1.FilesRes{Files: out}
	if a.browse != nil && p.UserID != 0 && req.GetEpisodeId() != 0 {
		prog, ok, err := a.browse.EpisodeProgress(ctx, p.UserID, req.GetEpisodeId())
		if err == nil && ok {
			res.PositionS = prog.PositionS
			res.Done = prog.Done
			res.LastFileId = prog.LastFileID
		}
	}
	return res, nil
}

// episodesPageSize keeps one /episodes answer inside wire.MaxTextPayload (ADR 0028).
const episodesPageSize = 40

// Episodes answers one page of one season (ADR 0028). Unknown title or empty season is an empty
// list, not an error — same as Files.
func (a *API) Episodes(ctx context.Context, _ Principal, req *sushiv1.EpisodesReq) (*sushiv1.EpisodesRes, error) {
	kind, err := itemKind(req.GetKind())
	if err != nil {
		return nil, err
	}
	page := req.GetPage()
	if page < 0 {
		page = 0
	}
	limit := int32(episodesPageSize)
	offset := page * limit
	episodes, total, err := a.catalog.EpisodesForSeason(ctx, req.GetTmdbId(), kind, req.GetSeasonNo(), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("episodes: %w", err)
	}
	var pages int32
	if total > 0 {
		pages = (total + limit - 1) / limit
	}
	out := make([]*sushiv1.Episode, len(episodes))
	for i, e := range episodes {
		out[i] = protoEpisode(e)
	}
	return &sushiv1.EpisodesRes{Episodes: out, Page: page, Pages: pages}, nil
}

func protoEpisode(e catalog.Episode) *sushiv1.Episode {
	return &sushiv1.Episode{
		EpisodeId: e.ID,
		SeasonNo:  e.SeasonNo,
		EpisodeNo: e.EpisodeNo,
		Title:     e.Title,
	}
}

func protoRow(r catalog.Row) *sushiv1.Row {
	return &sushiv1.Row{
		TmdbId: r.TMDBID,
		Kind:   protoKind(r.Kind),
		Title:  r.Title,
		Year:   r.Year,
		Rating: r.Rating,
		Poster: r.Poster,
	}
}

func protoPeople(people []catalog.Person) []*sushiv1.Person {
	out := make([]*sushiv1.Person, len(people))
	for i, p := range people {
		out[i] = &sushiv1.Person{Name: p.Name, Role: p.Role, Profile: p.Profile, TmdbId: p.TMDBID}
	}
	return out
}

func itemKind(k sushiv1.Kind) (catalog.Kind, error) {
	switch k {
	case sushiv1.Kind_KIND_MOVIE:
		return catalog.KindMovie, nil
	case sushiv1.Kind_KIND_SERIES:
		return catalog.KindSeries, nil
	case sushiv1.Kind_KIND_UNSPECIFIED:
		// Defaulting would hide a client bug behind an answer that looks right.
		return catalog.KindUnknown, fmt.Errorf("item: kind not specified")
	default:
		return catalog.KindUnknown, fmt.Errorf("item: unknown kind %d", k)
	}
}

func fileState(s catalog.FileState) sushiv1.FileState {
	switch s {
	case catalog.FileReady:
		return sushiv1.FileState_FILE_STATE_READY
	case catalog.FilePending:
		return sushiv1.FileState_FILE_STATE_PENDING
	case catalog.FileUnavailable:
		return sushiv1.FileState_FILE_STATE_UNAVAILABLE
	case catalog.FileUnknown:
		return sushiv1.FileState_FILE_STATE_UNSPECIFIED
	default:
		return sushiv1.FileState_FILE_STATE_UNSPECIFIED
	}
}
