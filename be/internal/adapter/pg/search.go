package pg

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

// SearchItems runs one page of the pg_trgm query (docs/12 §6).
func (s *Store) SearchItems(ctx context.Context, query string, offset, limit uint32) ([]catalog.Row, error) {
	rows, err := s.q.SearchItems(ctx, SearchItemsParams{
		Query: query, RowOffset: int32(offset), RowLimit: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("search items %q: %w", query, err)
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

// RecordSearch logs one committed query against the R-SEC-11 hourly cap.
func (s *Store) RecordSearch(ctx context.Context, userID int64) error {
	if err := s.q.RecordSearch(ctx, userID); err != nil {
		return fmt.Errorf("record search for %d: %w", userID, err)
	}
	if err := s.BumpLatestDevice(ctx, userID); err != nil {
		return err
	}
	return nil
}

// SearchCountSince is the R-SEC-11 hourly cap check.
func (s *Store) SearchCountSince(ctx context.Context, userID int64, since time.Time) (int, error) {
	n, err := s.q.SearchCountSince(ctx, SearchCountSinceParams{
		UserID: userID, At: pgtype.Timestamptz{Time: since, Valid: true},
	})
	if err != nil {
		return 0, fmt.Errorf("search count for %d: %w", userID, err)
	}
	return int(n), nil
}

// CatalogRowsByTMDBAnyKind returns compact catalog rows for TMDB ids (Persian alias search path).
func (s *Store) CatalogRowsByTMDBAnyKind(ctx context.Context, ids []uint64) ([]catalog.Row, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.q.CatalogRowsByTMDBAnyKind(ctx, int64s(ids))
	if err != nil {
		return nil, fmt.Errorf("catalog rows: %w", err)
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

// CatalogKeysByTMDB returns (tmdb_id, kind) pairs present in replicated items (ADR 0010).
func (s *Store) CatalogKeysByTMDB(ctx context.Context, ids []uint64) ([]catalog.RowKey, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.q.CatalogKeysByTMDB(ctx, int64s(ids))
	if err != nil {
		return nil, fmt.Errorf("catalog keys: %w", err)
	}
	out := make([]catalog.RowKey, len(rows))
	for i, r := range rows {
		out[i] = catalog.RowKey{TMDBID: uint64(r.TmdbID), Kind: catalog.Kind(r.Kind)}
	}
	return out, nil
}
