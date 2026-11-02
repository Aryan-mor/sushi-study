// Package pg is the sqlc-generated query layer for Sushi's own database, plus the thin wrapper
// that turns its rows into domain types.
//
// Since ADR 0003 this database is entirely ours: no permission walls, no schema shared with another
// product, and no filters undoing another product's semantics — normalisation happens once, in
// fetcherd, on the way in.
package pg

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
	"github.com/Aryan-mor/sushi-study/be/internal/platform/secretbox"
)

// Store is Sushi's database.
type Store struct {
	q       *Queries
	pool    *pgxpool.Pool
	secrets secretbox.Box
}

// NewStore wraps a pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{q: New(pool), pool: pool}
}

// WithSecrets hands the store the key for values sealed at rest: the personal-bot token main-bot
// keeps so it can re-issue a channel handoff (env/017), and the user's Gemini API key which apid
// opens on /prefs so the app can translate subtitles (env/028, doc 15 §12, platform/secretbox).
//
// Callers are main-bot (seal/open tokens and Gemini keys) and apid (open Gemini keys only).
// Every other daemon gets a store with no key, so a future read of a stored secret fails closed
// instead of quietly succeeding -- the smallest version of "only the process that needs a secret
// can read it".
func (s *Store) WithSecrets(box secretbox.Box) *Store {
	s.secrets = box
	return s
}

// Rail returns one materialised rail, in the order its build decided.
func (s *Store) Rail(ctx context.Context, kind catalog.RailKind, tab catalog.Kind) ([]catalog.Row, error) {
	rows, err := s.q.RailRows(ctx, RailRowsParams{Kind: int16(kind), Tab: int16(tab)})
	if err != nil {
		return nil, fmt.Errorf("rail %d/%d: %w", kind, tab, err)
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

// RailSeq returns the watermark the current rails were built at.
func (s *Store) RailSeq(ctx context.Context) (uint64, error) {
	seq, err := s.q.RailSeq(ctx)
	if err != nil {
		return 0, fmt.Errorf("rail seq: %w", err)
	}
	if seq < 0 {
		return 0, fmt.Errorf("rail seq: negative value %d", seq)
	}
	return uint64(seq), nil
}

// BumpSeq advances the rails watermark, which is how a client learns its cached home is stale.
func (s *Store) BumpSeq(ctx context.Context) (uint64, error) {
	seq, err := s.q.BumpRailSeq(ctx)
	if err != nil {
		return 0, fmt.Errorf("bump rail seq: %w", err)
	}
	return uint64(seq), nil
}

// ClearTab removes every rail of a tab so a rebuild starts clean. That is what makes the "not
// already on this screen" check in each fill enforce R-RAIL-1 at build time rather than hoping the
// read path catches a duplicate.
func (s *Store) ClearTab(ctx context.Context, tab catalog.Kind) error {
	if err := s.q.ClearTab(ctx, int16(tab)); err != nil {
		return fmt.Errorf("clear tab %s: %w", tab, err)
	}
	return nil
}

// FillNewest places the newest titles of a tab into a rail.
func (s *Store) FillNewest(ctx context.Context, kind catalog.RailKind, tab catalog.Kind, rankFrom, limit int32) (int64, error) {
	n, err := s.q.FillRailNewest(ctx, FillRailNewestParams{
		Kind:     int16(kind),
		Tab:      int16(tab),
		RankFrom: rankFrom,
		ItemKind: int16(tab),
		RowLimit: limit,
	})
	if err != nil {
		return 0, fmt.Errorf("fill newest %s: %w", tab, err)
	}
	return n, nil
}

// FillMostWatched places the most-played titles of a tab into a rail.
func (s *Store) FillMostWatched(ctx context.Context, kind catalog.RailKind, tab catalog.Kind, rankFrom, windowDays, minPlays, limit int32) (int64, error) {
	n, err := s.q.FillRailMostWatched(ctx, FillRailMostWatchedParams{
		Kind:       int16(kind),
		Tab:        int16(tab),
		RankFrom:   rankFrom,
		ItemKind:   int16(tab),
		WindowDays: windowDays,
		MinPlays:   minPlays,
		RowLimit:   limit,
	})
	if err != nil {
		return 0, fmt.Errorf("fill most watched %s: %w", tab, err)
	}
	return n, nil
}

// FillByTMDBIDs places TMDB's trending ids into a rail, in the order given, keeping only titles we
// actually carry (a playable file) and not already on this screen.
func (s *Store) FillByTMDBIDs(ctx context.Context, kind catalog.RailKind, tab catalog.Kind, rankFrom int32, tmdbIDs []uint64, limit int32) (int64, error) {
	n, err := s.q.FillRailByTMDBIDs(ctx, FillRailByTMDBIDsParams{
		Kind:     int16(kind),
		Tab:      int16(tab),
		RankFrom: rankFrom,
		TmdbIds:  int64s(tmdbIDs),
		ItemKind: int16(tab),
		RowLimit: limit,
	})
	if err != nil {
		return 0, fmt.Errorf("fill by tmdb ids %s: %w", tab, err)
	}
	return n, nil
}

// FillRandom tops up a short rail with titles not already on the screen.
func (s *Store) FillRandom(ctx context.Context, kind catalog.RailKind, tab catalog.Kind, rankFrom, limit int32) (int64, error) {
	n, err := s.q.FillRailRandom(ctx, FillRailRandomParams{
		Kind:     int16(kind),
		Tab:      int16(tab),
		RankFrom: rankFrom,
		ItemKind: int16(tab),
		RowLimit: limit,
	})
	if err != nil {
		return 0, fmt.Errorf("fill random %s: %w", tab, err)
	}
	return n, nil
}

// InTx runs fn against a Store bound to one transaction, committing only if fn returns nil.
//
// This is rails.TxRunner's real implementation: a tab rebuild clears the whole tab before refilling
// it, and outside a transaction every home request in that window sees a blank screen.
func (s *Store) InTx(ctx context.Context, fn func(*Store) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	// Rollback after a successful Commit returns pgx.ErrTxClosed, so this needs no "did we commit"
	// flag: it is here to cover the panic and early-return paths, which are the ones that would
	// otherwise leak a transaction until idle_in_transaction_session_timeout noticed. Either
	// outcome is the same to us -- the transaction is over.
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck // the transaction is over either way; see above

	if err := fn(&Store{q: s.q.WithTx(tx), pool: s.pool, secrets: s.secrets}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
