package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

// BrowseItem returns a TMDB-only title from env browse_titles (ADR 0009).
func (s *Store) BrowseItem(ctx context.Context, tmdbID uint64, kind catalog.Kind) (catalog.Item, bool, error) {
	row, err := s.q.BrowseTitleByTMDB(ctx, BrowseTitleByTMDBParams{TmdbID: int64(tmdbID), Kind: int16(kind)})
	if errors.Is(err, pgx.ErrNoRows) {
		return catalog.Item{}, false, nil
	}
	if err != nil {
		return catalog.Item{}, false, fmt.Errorf("browse %d/%d: %w", tmdbID, kind, err)
	}
	var releasedOn time.Time
	if row.ReleasedOn.Valid {
		releasedOn = row.ReleasedOn.Time
	}
	item := catalog.Item{
		Row: catalog.Row{
			TMDBID: uint64(row.TmdbID),
			Kind:   catalog.Kind(row.Kind),
			Title:  row.Title,
			Year:   uint32(row.Year),
			Rating: uint32(row.Rating),
			Poster: row.Poster,
		},
		Overview:   row.Overview,
		ReleasedOn: releasedOn,
		Detail:     catalog.UnmarshalDetail(row.Detail),
	}
	related, collection, err := s.titleExtras(ctx, item)
	if err != nil {
		return catalog.Item{}, false, err
	}
	item.Related = related
	item.Collection = collection
	return item, true, nil
}

// UpsertBrowseItem persists a TMDB title page into browse_titles.
func (s *Store) UpsertBrowseItem(ctx context.Context, item catalog.Item) error {
	var released pgtype.Date
	if !item.ReleasedOn.IsZero() {
		released = pgtype.Date{Time: item.ReleasedOn, Valid: true}
	}
	err := s.q.UpsertBrowseTitle(ctx, UpsertBrowseTitleParams{
		TmdbID:     int64(item.TMDBID),
		Kind:       int16(item.Kind),
		Title:      item.Title,
		Year:       int32(item.Year),
		Rating:     int16(item.Rating),
		Poster:     item.Poster,
		Overview:   item.Overview,
		ReleasedOn: released,
		Detail:     catalog.MarshalDetail(item.Detail),
	})
	if err != nil {
		return fmt.Errorf("upsert browse %d/%d: %w", item.TMDBID, item.Kind, err)
	}
	return nil
}

// BrowsePerson returns a cached person page from browse_people.
func (s *Store) BrowsePerson(ctx context.Context, tmdbID uint64) (catalog.PersonPage, bool, error) {
	row, err := s.q.BrowsePersonByTMDB(ctx, int64(tmdbID))
	if errors.Is(err, pgx.ErrNoRows) {
		return catalog.PersonPage{}, false, nil
	}
	if err != nil {
		return catalog.PersonPage{}, false, fmt.Errorf("browse person %d: %w", tmdbID, err)
	}
	return catalog.UnmarshalPersonPage(
		uint64(row.TmdbID), row.Name, row.Profile, row.Biography, row.Movies, row.Series,
	), true, nil
}

// UpsertBrowsePerson persists TMDB person identity + filmography rows.
func (s *Store) UpsertBrowsePerson(ctx context.Context, page catalog.PersonPage) error {
	err := s.q.UpsertBrowsePerson(ctx, UpsertBrowsePersonParams{
		TmdbID:    int64(page.TMDBID),
		Name:      page.Name,
		Profile:   page.Profile,
		Biography: page.Biography,
		Movies:    catalog.MarshalRows(page.Movies),
		Series:    catalog.MarshalRows(page.Series),
	})
	if err != nil {
		return fmt.Errorf("upsert browse person %d: %w", page.TMDBID, err)
	}
	return nil
}

// CompactRowsAnyKind joins catalog rows for mixed movie/series id lists (person filmography).
func (s *Store) CompactRowsAnyKind(ctx context.Context, ids []uint64) ([]catalog.Row, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.q.CompactRowsByTMDBAnyKind(ctx, int64s(ids))
	if err != nil {
		return nil, fmt.Errorf("compact any kind: %w", err)
	}
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
	return out, nil
}

// ListCatalog returns a page of library rows for Movies or Series.
func (s *Store) ListCatalog(ctx context.Context, kind catalog.Kind, sort int, sortDesc bool, genre string, year int, q string, limit, offset int32) ([]catalog.Row, error) {
	params := ListMoviesParams{
		Genre:    genre,
		Year:     int32(year),
		Q:        q,
		Sort:     int32(sort),
		SortDesc: sortDesc,
		Lim:      limit,
		Off:      offset,
	}
	var (
		rows []ListMoviesRow
		err  error
	)
	if kind == catalog.KindSeries {
		sr, listErr := s.q.ListSeries(ctx, ListSeriesParams{
			Genre: genre, Year: int32(year), Q: q, Sort: int32(sort),
			SortDesc: sortDesc, Lim: limit, Off: offset,
		})
		if listErr != nil {
			return nil, fmt.Errorf("list series: %w", listErr)
		}
		out := make([]catalog.Row, len(sr))
		for i, r := range sr {
			out[i] = catalog.Row{
				TMDBID: uint64(r.TmdbID), Kind: catalog.Kind(r.Kind),
				Title: r.Title, Year: uint32(r.Year), Rating: uint32(r.Rating), Poster: r.Poster,
			}
		}
		return out, nil
	}
	rows, err = s.q.ListMovies(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list movies: %w", err)
	}
	out := make([]catalog.Row, len(rows))
	for i, r := range rows {
		out[i] = catalog.Row{
			TMDBID: uint64(r.TmdbID), Kind: catalog.Kind(r.Kind),
			Title: r.Title, Year: uint32(r.Year), Rating: uint32(r.Rating), Poster: r.Poster,
		}
	}
	return out, nil
}

// ListBoxsetMetas returns one card per TMDB collection: the collection's own id/name, not a
// member movie's.
func (s *Store) ListBoxsetMetas(ctx context.Context, q string, limit, offset int32) ([]catalog.BoxsetMeta, error) {
	rows, err := s.q.ListBoxsetMetas(ctx, ListBoxsetMetasParams{Q: q, Lim: limit, Off: offset})
	if err != nil {
		return nil, fmt.Errorf("list boxset metas: %w", err)
	}
	out := make([]catalog.BoxsetMeta, len(rows))
	for i, r := range rows {
		out[i] = catalog.BoxsetMeta{
			CollectionID: uint64(r.CollectionID), Name: r.Name, Poster: r.Poster, ItemCount: uint32(r.ItemCount),
		}
	}
	return out, nil
}

// ListBoxsetItems returns the members of one TMDB collection.
func (s *Store) ListBoxsetItems(ctx context.Context, collectionID uint64, limit, offset int32) ([]catalog.Row, error) {
	rows, err := s.q.ListBoxsetItems(ctx, ListBoxsetItemsParams{
		CollectionID: int64(collectionID), Lim: limit, Off: offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list boxset items %d: %w", collectionID, err)
	}
	out := make([]catalog.Row, len(rows))
	for i, r := range rows {
		out[i] = catalog.Row{
			TMDBID: uint64(r.TmdbID), Kind: catalog.Kind(r.Kind),
			Title: r.Title, Year: uint32(r.Year), Rating: uint32(r.Rating), Poster: r.Poster,
		}
	}
	return out, nil
}

// ListUserFlags returns favorites or watch-later rows for a user.
func (s *Store) ListUserFlags(ctx context.Context, userID int64, flag, q string, limit, offset int32) ([]catalog.Row, error) {
	rows, err := s.q.ListUserFlags(ctx, ListUserFlagsParams{
		UserID: userID, Flag: flag, Q: q, Lim: limit, Off: offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", flag, err)
	}
	out := make([]catalog.Row, len(rows))
	for i, r := range rows {
		out[i] = catalog.Row{
			TMDBID: uint64(r.TmdbID), Kind: catalog.Kind(r.Kind),
			Title: r.Title, Year: uint32(r.Year), Rating: uint32(r.Rating), Poster: r.Poster,
		}
	}
	return out, nil
}

// ListForYou is followed/favourited series with at least one serveable episode this user has not
// finished (LIST_SCOPE_FOR_YOU) -- new episodes not seen yet and unfinished series alike.
func (s *Store) ListForYou(ctx context.Context, userID int64, limit, offset int32) ([]catalog.Row, error) {
	rows, err := s.q.ForYouRows(ctx, ForYouRowsParams{
		UserID: userID, Lim: limit, Off: offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list for you: %w", err)
	}
	out := make([]catalog.Row, len(rows))
	for i, r := range rows {
		out[i] = catalog.Row{
			TMDBID: uint64(r.TmdbID), Kind: catalog.Kind(r.Kind),
			Title: r.Title, Year: uint32(r.Year), Rating: uint32(r.Rating), Poster: r.Poster,
		}
	}
	return out, nil
}

// LatestWatchLater is the newest watch-later of this kind. Missing is not found, not an error.
func (s *Store) LatestWatchLater(ctx context.Context, userID int64, kind catalog.Kind) (catalog.Row, bool, error) {
	row, err := s.q.LatestWatchLater(ctx, LatestWatchLaterParams{
		UserID: userID, Kind: int16(kind),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return catalog.Row{}, false, nil
	}
	if err != nil {
		return catalog.Row{}, false, fmt.Errorf("latest watch-later: %w", err)
	}
	return catalog.Row{
		TMDBID: uint64(row.TmdbID), Kind: catalog.Kind(row.Kind),
		Title: row.Title, Year: uint32(row.Year), Rating: uint32(row.Rating), Poster: row.Poster,
	}, true, nil
}

// UserItemFlags reads one title's favourite / watch-later pair for the title page (ADR 0032).
func (s *Store) UserItemFlags(
	ctx context.Context, userID int64, tmdbID uint64, kind catalog.Kind,
) (favorite, watchLater bool, err error) {
	row, err := s.q.UserItemFlags(ctx, UserItemFlagsParams{
		UserID: userID, TmdbID: int64(tmdbID), Kind: int16(kind),
	})
	if err != nil {
		return false, false, fmt.Errorf("user item flags: %w", err)
	}
	return row.Favorite, row.WatchLater, nil
}

// SetFavorite upserts the favorite flag.
func (s *Store) SetFavorite(ctx context.Context, userID int64, tmdbID uint64, kind catalog.Kind, on bool) error {
	return s.q.UpsertUserFavorite(ctx, UpsertUserFavoriteParams{
		UserID: userID, TmdbID: int64(tmdbID), Kind: int16(kind), Favorite: on,
	})
}

// SetWatchLater upserts the watch_later flag.
func (s *Store) SetWatchLater(ctx context.Context, userID int64, tmdbID uint64, kind catalog.Kind, on bool) error {
	return s.q.UpsertUserLater(ctx, UpsertUserLaterParams{
		UserID: userID, TmdbID: int64(tmdbID), Kind: int16(kind), WatchLater: on,
	})
}

// SetNotInterested upserts the not_interested flag. Favorite and watch-later stay as they were.
func (s *Store) SetNotInterested(ctx context.Context, userID int64, tmdbID uint64, kind catalog.Kind, on bool) error {
	return s.q.UpsertUserNotInterested(ctx, UpsertUserNotInterestedParams{
		UserID: userID, TmdbID: int64(tmdbID), Kind: int16(kind), NotInterested: on,
	})
}

// ListNotInterested returns titles this viewer hid from Home. Identity only: title metadata is unused.
func (s *Store) ListNotInterested(ctx context.Context, userID int64) ([]catalog.Row, error) {
	rows, err := s.q.ListNotInterested(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list not interested: %w", err)
	}
	out := make([]catalog.Row, len(rows))
	for i, r := range rows {
		out[i] = catalog.Row{TMDBID: uint64(r.TmdbID), Kind: catalog.Kind(r.Kind)}
	}
	return out, nil
}

// ListPlaylists returns playlist metas for a user.
func (s *Store) ListPlaylists(ctx context.Context, userID int64) ([]catalog.PlaylistMeta, error) {
	rows, err := s.q.ListPlaylists(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list playlists: %w", err)
	}
	out := make([]catalog.PlaylistMeta, len(rows))
	for i, r := range rows {
		out[i] = catalog.PlaylistMeta{ID: r.ID, Name: r.Name, ItemCount: uint32(r.ItemCount)}
	}
	return out, nil
}

// ListPlaylistItems returns members of one playlist.
func (s *Store) ListPlaylistItems(ctx context.Context, playlistID int64, limit, offset int32) ([]catalog.Row, error) {
	rows, err := s.q.ListPlaylistItems(ctx, ListPlaylistItemsParams{
		PlaylistID: playlistID, Lim: limit, Off: offset,
	})
	if err != nil {
		return nil, fmt.Errorf("playlist items %d: %w", playlistID, err)
	}
	out := make([]catalog.Row, len(rows))
	for i, r := range rows {
		out[i] = catalog.Row{
			TMDBID: uint64(r.TmdbID), Kind: catalog.Kind(r.Kind),
			Title: r.Title, Year: uint32(r.Year), Rating: uint32(r.Rating), Poster: r.Poster,
		}
	}
	return out, nil
}
