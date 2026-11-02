// Command backfilllastair writes TMDB last episode air dates into library items.detail.lastAirDate.
// Env replica is read-only (R-LIB-3); this talks to LIBRARY_DATABASE_URL only.
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

	ids, err := seriesIDs(ctx, lib)
	if err != nil {
		return err
	}
	log.Info("series", "n", len(ids))
	wrote, miss, err := fillLastAir(ctx, lib, tmdb.New(cfg.TMDBKey), ids)
	if err != nil {
		return err
	}
	log.Info("done", "wrote", wrote, "miss", miss)
	return nil
}

func seriesIDs(ctx context.Context, lib *pgxpool.Pool) ([]int64, error) {
	rows, err := lib.Query(ctx, `SELECT tmdb_id FROM items WHERE kind = $1 ORDER BY tmdb_id`, int16(catalog.KindSeries))
	if err != nil {
		return nil, fmt.Errorf("list series: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list series: %w", err)
	}
	return ids, nil
}

func fillLastAir(ctx context.Context, lib *pgxpool.Pool, api *tmdb.Client, ids []int64) (wrote, miss int64, err error) {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(6)
	var wroteN, missN atomic.Int64
	for _, id := range ids {
		g.Go(func() error {
			return fillOne(gctx, lib, api, id, &wroteN, &missN)
		})
	}
	if err := g.Wait(); err != nil {
		return 0, 0, err
	}
	return wroteN.Load(), missN.Load(), nil
}

func fillOne(ctx context.Context, lib *pgxpool.Pool, api *tmdb.Client, id int64, wrote, miss *atomic.Int64) error {
	day, err := api.SeriesLastAirDate(ctx, uint64(id))
	if err != nil {
		return fmt.Errorf("tmdb %d: %w", id, err)
	}
	if day == "" {
		miss.Add(1)
		return nil
	}
	tag, err := lib.Exec(ctx, `
		UPDATE items
		SET detail = jsonb_set(COALESCE(detail, '{}'::jsonb), '{lastAirDate}', to_jsonb($1::text), true),
		    updated_seq = nextval('catalog_seq'),
		    updated_at = now()
		WHERE tmdb_id = $2 AND kind = $3
		  AND COALESCE(detail->>'lastAirDate','') IS DISTINCT FROM $1`,
		day, id, int16(catalog.KindSeries))
	if err != nil {
		return fmt.Errorf("update %d: %w", id, err)
	}
	if tag.RowsAffected() > 0 {
		wrote.Add(1)
	}
	return nil
}
