package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

// Item resolves a title page by its wire address (tmdb_id, kind), episode tree included.
func (s *Store) Item(ctx context.Context, tmdbID uint64, kind catalog.Kind) (catalog.Item, bool, error) {
	row, err := s.q.ItemByTMDB(ctx, ItemByTMDBParams{TmdbID: int64(tmdbID), Kind: int16(kind)})
	if errors.Is(err, pgx.ErrNoRows) {
		return catalog.Item{}, false, nil
	}
	if err != nil {
		return catalog.Item{}, false, fmt.Errorf("item %d/%d: %w", tmdbID, kind, err)
	}

	episodes, err := s.q.EpisodesForItem(ctx, row.ID)
	if err != nil {
		return catalog.Item{}, false, fmt.Errorf("episodes for item %d/%d: %w", tmdbID, kind, err)
	}

	var releasedOn time.Time
	if row.ReleasedOn.Valid {
		releasedOn = row.ReleasedOn.Time
	}

	out := catalog.Item{
		Row: catalog.Row{
			TMDBID: uint64(row.TmdbID),
			Kind:   catalog.Kind(row.Kind),
			Title:  row.Title,
			Year:   uint32(row.Year),
			Rating: uint32(row.Rating),
			Poster: row.Poster,
		},
		Overview:   row.Overview,
		OverviewFa: row.OverviewFa,
		ReleasedOn: releasedOn,
		Episodes:   episodesFrom(episodes),
		Detail:     catalog.UnmarshalDetail(row.Detail),
	}
	related, collection, err := s.titleExtras(ctx, out)
	if err != nil {
		return catalog.Item{}, false, err
	}
	out.Related = related
	out.Collection = collection
	return out, true, nil
}

func episodesFrom(episodes []EpisodesForItemRow) []catalog.Episode {
	out := make([]catalog.Episode, len(episodes))
	for i, e := range episodes {
		out[i] = catalog.Episode{
			ID:        e.ID,
			SeasonNo:  e.SeasonNo,
			EpisodeNo: e.EpisodeNo,
			Title:     e.Title,
		}
	}
	return out
}

func (s *Store) titleExtras(ctx context.Context, item catalog.Item) ([]catalog.Row, []catalog.Row, error) {
	related, err := s.mergeRelated(ctx, item)
	if err != nil {
		return nil, nil, err
	}
	var collection []catalog.Row
	if item.Detail.CollectionID > 0 {
		rows, err := s.q.CompactRowsByCollection(ctx, CompactRowsByCollectionParams{
			CollectionID: int64(item.Detail.CollectionID),
			ExcludeTmdb:  int64(item.TMDBID),
			Kind:         int16(item.Kind),
		})
		if err != nil {
			return nil, nil, fmt.Errorf("collection for item %d/%d: %w", item.TMDBID, item.Kind, err)
		}
		collection = compactCollection(rows)
	}
	return related, collection, nil
}

// mergeRelated keeps SimilarTMDB order: catalog compact row when present, else RelatedPreview.
func (s *Store) mergeRelated(ctx context.Context, item catalog.Item) ([]catalog.Row, error) {
	ids := int64s(item.Detail.SimilarTMDB)
	var hits []catalog.Row
	if len(ids) > 0 {
		rows, err := s.q.CompactRowsByTMDB(ctx, CompactRowsByTMDBParams{
			Kind:    int16(item.Kind),
			TmdbIds: ids,
		})
		if err != nil {
			return nil, fmt.Errorf("related for item %d/%d: %w", item.TMDBID, item.Kind, err)
		}
		hits = compactTMDB(rows)
	}
	return catalog.ComposeRelated(item.Detail, hits), nil
}

func int64s(ids []uint64) []int64 {
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id > 0 {
			out = append(out, int64(id))
		}
	}
	return out
}

func compactTMDB(rows []CompactRowsByTMDBRow) []catalog.Row {
	out := make([]catalog.Row, len(rows))
	for i, r := range rows {
		out[i] = catalog.Row{
			TMDBID: uint64(r.TmdbID),
			Kind:   catalog.Kind(r.Kind),
			Title:  r.Title,
			Year:   uint32(r.Year),
			Rating: uint32(r.Rating),
			Poster: r.Poster,
		}
	}
	return out
}

func compactCollection(rows []CompactRowsByCollectionRow) []catalog.Row {
	out := make([]catalog.Row, len(rows))
	for i, r := range rows {
		out[i] = catalog.Row{
			TMDBID: uint64(r.TmdbID),
			Kind:   catalog.Kind(r.Kind),
			Title:  r.Title,
			Year:   uint32(r.Year),
			Rating: uint32(r.Rating),
			Poster: r.Poster,
		}
	}
	return out
}

// Files returns one episode's pick-list. An unknown or emptied-out episode id answers an empty
// list rather than an error -- there is no meaningful difference here between "bad id" and "no
// files yet", and inventing one would be a special case nothing needs.
func (s *Store) Files(ctx context.Context, episodeID int64) ([]catalog.File, error) {
	rows, err := s.q.FilesForEpisode(ctx, episodeID)
	if err != nil {
		return nil, fmt.Errorf("files for episode %d: %w", episodeID, err)
	}
	out := make([]catalog.File, len(rows))
	for i, r := range rows {
		out[i] = fileFromRow(r.ID, r.QualityLabel, r.DisplayName, r.Height, r.AudioLangs, r.SubLangs, r.SizeBytes, r.DurationS, r.State)
	}
	return out, nil
}

// EpisodesForSeason returns one page of a series season. Unknown title is empty, not an error.
func (s *Store) EpisodesForSeason(ctx context.Context, tmdbID uint64, kind catalog.Kind, seasonNo, limit, offset int32) ([]catalog.Episode, int32, error) {
	row, err := s.q.ItemByTMDB(ctx, ItemByTMDBParams{TmdbID: int64(tmdbID), Kind: int16(kind)})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("episodes item %d/%d: %w", tmdbID, kind, err)
	}
	if limit <= 0 {
		limit = 40
	}
	if offset < 0 {
		offset = 0
	}
	total, err := s.q.CountEpisodesForItemSeason(ctx, CountEpisodesForItemSeasonParams{
		ItemID: row.ID, SeasonNo: seasonNo,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("count episodes tmdb=%d s%d: %w", tmdbID, seasonNo, err)
	}
	rows, err := s.q.EpisodesForItemSeason(ctx, EpisodesForItemSeasonParams{
		ItemID: row.ID, SeasonNo: seasonNo, Lim: limit, Off: offset,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("episodes tmdb=%d s%d: %w", tmdbID, seasonNo, err)
	}
	return episodesFromSeason(rows), total, nil
}

// FirstPlayableEpisode is the header Play target: first season>0 episode with a ready file.
func (s *Store) FirstPlayableEpisode(ctx context.Context, tmdbID uint64, kind catalog.Kind) (catalog.Episode, bool, error) {
	row, err := s.q.FirstPlayableEpisodeForItem(ctx, FirstPlayableEpisodeForItemParams{
		TmdbID: int64(tmdbID), Kind: int16(kind),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return catalog.Episode{}, false, nil
	}
	if err != nil {
		return catalog.Episode{}, false, fmt.Errorf("play-target tmdb=%d: %w", tmdbID, err)
	}
	return catalog.Episode{ID: row.ID, SeasonNo: row.SeasonNo, EpisodeNo: row.EpisodeNo, Title: row.Title}, true, nil
}

func episodesFromSeason(rows []EpisodesForItemSeasonRow) []catalog.Episode {
	out := make([]catalog.Episode, len(rows))
	for i, e := range rows {
		out[i] = catalog.Episode{
			ID: e.ID, SeasonNo: e.SeasonNo, EpisodeNo: e.EpisodeNo, Title: e.Title,
		}
	}
	return out
}

// FileCopy is one Telegram copy of a file — clone or backup — that catalog-bot can copyMessage from.
type FileCopy struct {
	ChannelID int64
	MessageID int64
	Role      string
}

// FileCopies lists clone first, then backups (env/catalog.sql FileLocations). Empty means the
// file has no copy this process can reach.
func (s *Store) FileCopies(ctx context.Context, fileID int64) ([]FileCopy, error) {
	rows, err := s.q.FileLocations(ctx, fileID)
	if err != nil {
		return nil, fmt.Errorf("file copies %d: %w", fileID, err)
	}
	out := make([]FileCopy, len(rows))
	for i, r := range rows {
		out[i] = FileCopy{ChannelID: r.ChannelID, MessageID: r.MessageID, Role: r.Role}
	}
	return out, nil
}

// File returns one files row. Unknown id is not found, not an error.
func (s *Store) File(ctx context.Context, fileID int64) (catalog.File, bool, error) {
	row, err := s.q.FileByID(ctx, fileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return catalog.File{}, false, nil
	}
	if err != nil {
		return catalog.File{}, false, fmt.Errorf("file %d: %w", fileID, err)
	}
	return fileFromRow(row.ID, row.QualityLabel, row.DisplayName, row.Height, row.AudioLangs, row.SubLangs, row.SizeBytes, row.DurationS, row.State), true, nil
}

// FileOrigin is a files row plus the title and episode it belongs to — enough for a delivery
// caption without a second round-trip that can lose the TMDB id.
type FileOrigin struct {
	File         catalog.File
	TMDBID       uint64
	Kind         catalog.Kind
	Title        string
	Year         uint32
	SeasonNo     int32
	EpisodeNo    int32
	EpisodeTitle string
}

// FileOrigin loads one file and the item/episode it plays. Unknown id is not found, not an error.
func (s *Store) FileOrigin(ctx context.Context, fileID int64) (FileOrigin, bool, error) {
	row, err := s.q.FileOrigin(ctx, fileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return FileOrigin{}, false, nil
	}
	if err != nil {
		return FileOrigin{}, false, fmt.Errorf("file origin %d: %w", fileID, err)
	}
	return FileOrigin{
		File:         fileFromRow(row.ID, row.QualityLabel, row.DisplayName, row.Height, row.AudioLangs, row.SubLangs, row.SizeBytes, row.DurationS, row.State),
		TMDBID:       uint64(row.TmdbID),
		Kind:         catalog.Kind(row.Kind),
		Title:        row.Title,
		Year:         uint32(row.Year),
		SeasonNo:     row.SeasonNo,
		EpisodeNo:    row.EpisodeNo,
		EpisodeTitle: row.EpisodeTitle,
	}, true, nil
}

// ItemStub is the title card when Item() cannot be loaded — enough for a caption, no overview.
func (o FileOrigin) ItemStub() catalog.Item {
	return catalog.Item{Row: catalog.Row{
		TMDBID: o.TMDBID, Kind: o.Kind, Title: o.Title, Year: o.Year,
	}}
}

func fileFromRow(id int64, quality, display string, height int32, audio, sub string, size int64, dur int32, state int16) catalog.File {
	return catalog.File{
		ID:           id,
		QualityLabel: quality,
		DisplayName:  display,
		Height:       height,
		AudioLangs:   audio,
		SubLangs:     sub,
		SizeBytes:    size,
		DurationS:    dur,
		State:        catalog.FileState(state),
	}
}
