// Command backfilltrailers writes TMDB trailer keys into library items.detail.trailerKey for the
// existing catalog (docs/12 §4, ADR 0031). On-demand hydration piggybacks on whatever already
// triggers a re-fetch (missing logo/country/people/related, catalog.DetailNeedsHydrate) and does
// not check trailerKey on its own, so an already-hydrated item would otherwise never pick this up.
// One-off, run by hand; env replica is read-only (R-LIB-3), this talks to LIBRARY_DATABASE_URL only.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tmdb"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
	"github.com/Aryan-mor/sushi-study/be/internal/platform/config"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("backfill", "err", err)
		os.Exit(1)
	}
}

type itemRef struct {
	tmdbID int64
	kind   catalog.Kind
}

func run(log *slog.Logger) error {
	cfg, err := config.LoadIndexer(os.Getenv("SUSHI_ENV_FILE"))
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()

	lib, err := pgxpool.New(ctx, cfg.LibraryURL)
	if err != nil {
		return fmt.Errorf("library pool: %w", err)
	}
	defer lib.Close()

	items, err := itemsMissingTrailer(ctx, lib)
	if err != nil {
		return err
	}
	log.Info("items", "n", len(items))
	wrote, none, err := fillTrailers(ctx, lib, tmdb.New(cfg.TMDBKey), items)
	if err != nil {
		return err
	}
	log.Info("done", "wrote", wrote, "none", none)
	return nil
}

func itemsMissingTrailer(ctx context.Context, lib *pgxpool.Pool) ([]itemRef, error) {
	rows, err := lib.Query(ctx,
		`SELECT tmdb_id, kind FROM items WHERE COALESCE(detail->>'trailerKey', '') = '' ORDER BY tmdb_id`)
	if err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	defer rows.Close()
	var out []itemRef
	for rows.Next() {
		var id int64
		var kind int16
		if err := rows.Scan(&id, &kind); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		out = append(out, itemRef{tmdbID: id, kind: catalog.Kind(kind)})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	return out, nil
}

func fillTrailers(ctx context.Context, lib *pgxpool.Pool, api *tmdb.Client, items []itemRef) (wrote, none int64, err error) {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(6)
	var wroteN, noneN atomic.Int64
	for _, it := range items {
		g.Go(func() error {
			return fillOne(gctx, lib, api, it, &wroteN, &noneN)
		})
	}
	if err := g.Wait(); err != nil {
		return 0, 0, err
	}
	return wroteN.Load(), noneN.Load(), nil
}

func fillOne(ctx context.Context, lib *pgxpool.Pool, api *tmdb.Client, it itemRef, wrote, none *atomic.Int64) error {
	item, err := api.TitlePage(ctx, uint64(it.tmdbID), it.kind)
	if err != nil {
		return fmt.Errorf("tmdb %d: %w", it.tmdbID, err)
	}
	key := item.Detail.TrailerKey
	if key == "" {
		key = catalog.TrailerNone
		none.Add(1)
	}
	tag, err := lib.Exec(ctx, `
		UPDATE items
		SET detail = jsonb_set(COALESCE(detail, '{}'::jsonb), '{trailerKey}', to_jsonb($1::text), true),
		    updated_seq = nextval('catalog_seq'),
		    updated_at = now()
		WHERE tmdb_id = $2 AND kind = $3
		  AND COALESCE(detail->>'trailerKey','') IS DISTINCT FROM $1`,
		key, it.tmdbID, int16(it.kind))
	if err != nil {
		return fmt.Errorf("update %d: %w", it.tmdbID, err)
	}
	if tag.RowsAffected() > 0 {
		wrote.Add(1)
	}
	return nil
}
