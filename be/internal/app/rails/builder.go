// Package rails materialises the home screen.
//
// Every rail is computed — nothing here is editorial, which is what keeps "no admin surface in v1"
// honest (docs/10 Q17). The policy lives here; the SQL that carries it out lives in the adapter,
// so the catalog's UUID keys never leave the database layer (R-DATA-1).
package rails

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

// Store is what building a rail needs. Each Fill returns how many cards it actually placed, which
// is what lets the policy below notice a rail came back short.
//
// It is always a store bound to one transaction -- see TxRunner.
type Store interface {
	ClearTab(ctx context.Context, tab catalog.Kind) error
	FillNewest(ctx context.Context, kind catalog.RailKind, tab catalog.Kind, rankFrom, limit int32) (int64, error)
	FillMostWatched(ctx context.Context, kind catalog.RailKind, tab catalog.Kind, rankFrom, windowDays, minPlays, limit int32) (int64, error)
	FillByTMDBIDs(ctx context.Context, kind catalog.RailKind, tab catalog.Kind, rankFrom int32, tmdbIDs []uint64, limit int32) (int64, error)
	FillRandom(ctx context.Context, kind catalog.RailKind, tab catalog.Kind, rankFrom, limit int32) (int64, error)
	BumpSeq(ctx context.Context) (uint64, error)
}

// TrendingSource is TMDB's weekly trending list (docs/12 §2.3). Optional: a Builder built with a
// nil source skips straight to the same-catalog floor a thin intersection would fall back to
// anyway, which is also how a TMDB outage degrades (R-RAIL-3) rather than failing the rebuild.
type TrendingSource interface {
	TrendingWeek(ctx context.Context, tab catalog.Kind) ([]uint64, error)
}

// Policy is the shape of the home screen. Every number here is a product decision, so they live in
// one struct rather than scattered as literals.
type Policy struct {
	// SliderSize is the newest rail, per tab. Floor matches MinRailSize so home "New" is not a
	// two-card leftover after the banner takes the first few titles.
	SliderSize int32
	// RailSize is how many cards a normal rail holds. 16-20 is right at the edge of what fits in
	// one text message, which is why R-META-6 says pick it against the size table, not by feel.
	RailSize int32
	// MostWatchedWindowDays is the play-count window. Seven is too jumpy on a small catalog.
	MostWatchedWindowDays int32
	// MostWatchedMinPlays keeps a quiet week from crowning a title with two plays.
	MostWatchedMinPlays int32
	// MinRailSize is the floor: a rail thinner than this gets topped up rather than shipped as a
	// four-card row. Below it we would rather show random titles than an obviously broken rail.
	MinRailSize int32
}

// DefaultPolicy is the shape agreed in docs/10 Q17 and docs/12 §2.
//
// SliderSize matches MinRailSize so the client "New" row still has a floor after the banner
// takes the first few newest titles. A two-card slider left the rest of home looking broken.
func DefaultPolicy() Policy {
	return Policy{
		SliderSize:            8,
		RailSize:              18,
		MostWatchedWindowDays: 30,
		MostWatchedMinPlays:   3,
		MinRailSize:           8,
	}
}

// TxRunner scopes one rebuild to one database transaction: it calls fn with a Store bound to that
// transaction and commits only if fn returns nil. pg.Store.InTx is the real one.
//
// It is a constructor argument and not an option, because there is no correct build without it.
// BuildTab empties the whole tab before the fills refill it, so outside a transaction every home
// request landing in between gets a blank or half-built screen -- and `rail_state.seq`, the one
// signal a client has that its cached home is stale, is bumped only at the very end, so nothing
// even tells it that what it fetched was a rebuild in progress. FillRandom's `ORDER BY random()`
// over the whole catalog makes that window wide enough to land in.
type TxRunner func(ctx context.Context, fn func(Store) error) error

// Builder rebuilds rails on a schedule. Never on a request path (R-RAIL-2).
type Builder struct {
	run      TxRunner
	trending TrendingSource
	policy   Policy
}

// New builds a Builder over the transaction runner its rebuilds run inside. trending may be nil --
// see TrendingSource.
func New(run TxRunner, trending TrendingSource, policy Policy) *Builder {
	return &Builder{run: run, trending: trending, policy: policy}
}

// Result reports what a build produced, so the caller can log it and an operator can see at a
// glance whether a rail is being propped up by random filler.
type Result struct {
	Tab      catalog.Kind
	Slider   int64
	Watched  int64
	Trending int64
	Filler   int64
	Seq      uint64
}

// BuildTab rebuilds every rail of one tab, in one transaction.
//
// The whole tab is cleared first so the "not already on this screen" checks each fill performs
// start from a clean slate — that is what enforces R-RAIL-1, one appearance per item, at build
// time rather than hoping the read path catches it. Nothing outside the transaction ever sees that
// cleared state: readers keep the previous screen until the new one is complete, and a build that
// fails anywhere leaves the previous screen exactly as it was rather than a partial one.
func (b *Builder) BuildTab(ctx context.Context, tab catalog.Kind) (Result, error) {
	if tab != catalog.KindMovie && tab != catalog.KindSeries {
		return Result{}, fmt.Errorf("rails: %s is not a tab", tab)
	}

	var res Result
	err := b.run(ctx, func(store Store) error {
		var buildErr error
		res, buildErr = b.buildTab(ctx, store, tab)
		return buildErr
	})
	if err != nil {
		// Whatever the fills reported was rolled back with them, so there is nothing to report.
		return Result{Tab: tab}, err
	}
	return res, nil
}

func (b *Builder) buildTab(ctx context.Context, store Store, tab catalog.Kind) (Result, error) {
	if err := store.ClearTab(ctx, tab); err != nil {
		return Result{}, fmt.Errorf("rails: clear %s: %w", tab, err)
	}

	res := Result{Tab: tab}
	var err error

	if res.Slider, err = store.FillNewest(ctx, catalog.RailSlider, tab, 0, b.policy.SliderSize); err != nil {
		return res, fmt.Errorf("rails: slider: %w", err)
	}
	sliderFill, err := b.ensureMin(ctx, store, catalog.RailSlider, tab, res.Slider)
	if err != nil {
		return res, fmt.Errorf("rails: slider floor: %w", err)
	}
	res.Slider += sliderFill
	res.Filler += sliderFill

	res.Watched, err = store.FillMostWatched(ctx, catalog.RailMostWatched, tab, 0,
		b.policy.MostWatchedWindowDays, b.policy.MostWatchedMinPlays, b.policy.RailSize)
	if err != nil {
		return res, fmt.Errorf("rails: most watched: %w", err)
	}
	watchedFill, err := b.ensureMin(ctx, store, catalog.RailMostWatched, tab, res.Watched)
	if err != nil {
		return res, fmt.Errorf("rails: most watched floor: %w", err)
	}
	res.Watched += watchedFill
	res.Filler += watchedFill

	// TMDB weekly trending, intersected with the catalog (docs/12 §2.3), topped up if it comes
	// back thin. No TrendingSource configured, or TMDB unreachable, skips straight to the top-up.
	trending, trendingFill, err := b.fillTrending(ctx, store, tab)
	if err != nil {
		return res, err
	}
	res.Trending = trending
	res.Filler += trendingFill

	if res.Seq, err = store.BumpSeq(ctx); err != nil {
		return res, fmt.Errorf("rails: bump seq: %w", err)
	}
	return res, nil
}

// fillTrending fills the trending rail from TMDB's weekly list intersected with our catalog, then
// tops it up if fewer than MinRailSize survive the intersection: first from our own all-time play
// counts, then, if the catalog is too young even for that, at random (docs/12 §2.3).
func (b *Builder) fillTrending(ctx context.Context, store Store, tab catalog.Kind) (placed, filler int64, err error) {
	if b.trending != nil {
		ids, tErr := b.trending.TrendingWeek(ctx, tab)
		// A TMDB error degrades to the same floor a thin intersection would need anyway (R-RAIL-3)
		// -- placed stays 0 and the top-up below runs exactly as it would with no source at all.
		if tErr == nil && len(ids) > 0 {
			placed, err = store.FillByTMDBIDs(ctx, catalog.RailTrending, tab, 0, ids, b.policy.RailSize)
			if err != nil {
				return 0, 0, fmt.Errorf("rails: trending: %w", err)
			}
		}
	}

	if short := int64(b.policy.MinRailSize) - placed; short > 0 {
		topUp, wErr := store.FillMostWatched(ctx, catalog.RailTrending, tab, int32(placed),
			3650, 1, int32(min(short, int64(b.policy.RailSize)-placed)))
		if wErr != nil {
			return placed, 0, fmt.Errorf("rails: trending top-up: %w", wErr)
		}
		placed += topUp
	}

	filler, err = b.ensureMin(ctx, store, catalog.RailTrending, tab, placed)
	if err != nil {
		return placed, 0, fmt.Errorf("rails: trending filler: %w", err)
	}
	return placed + filler, filler, nil
}

// ensureMin tops a short rail with random playable titles not already on this tab. Random is
// rolled at build time and pinned (R-CACHE-2). Continue watching is client-owned and skipped.
func (b *Builder) ensureMin(ctx context.Context, store Store, kind catalog.RailKind, tab catalog.Kind, placed int64) (int64, error) {
	short := int64(b.policy.MinRailSize) - placed
	if short <= 0 {
		return 0, nil
	}
	n, err := store.FillRandom(ctx, kind, tab, int32(placed), int32(short))
	if err != nil {
		return 0, err
	}
	return n, nil
}

// RebuildAll materialises both home tabs. Hourly is enough (docs/12 R-RAIL-2); never on the
// request path. A failed tab is logged by the caller — the other tab still rebuilds.
func (b *Builder) RebuildAll(ctx context.Context) (movie, series Result, err error) {
	var errs []error
	movie, err = b.BuildTab(ctx, catalog.KindMovie)
	if err != nil {
		errs = append(errs, fmt.Errorf("movies: %w", err))
	}
	series, err = b.BuildTab(ctx, catalog.KindSeries)
	if err != nil {
		errs = append(errs, fmt.Errorf("series: %w", err))
	}
	return movie, series, errors.Join(errs...)
}

// Run rebuilds immediately, then every hour, until ctx is done. Immediate first pass is the
// point: fetcherd grows the catalog continuously, and a builder that waited an hour would leave
// home showing last week's rails (R-RAIL-2).
func (b *Builder) Run(ctx context.Context, log *slog.Logger) error {
	b.rebuildLogged(ctx, log)
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			b.rebuildLogged(ctx, log)
		}
	}
}

func (b *Builder) rebuildLogged(ctx context.Context, log *slog.Logger) {
	movie, series, err := b.RebuildAll(ctx)
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return
	case err != nil:
		log.Error("rails rebuild failed", "err", err, "movie_seq", movie.Seq, "series_seq", series.Seq)
	default:
		log.Info("rails rebuilt",
			"movie_slider", movie.Slider, "movie_watched", movie.Watched, "movie_trending", movie.Trending,
			"series_slider", series.Slider, "series_watched", series.Watched, "series_trending", series.Trending,
			"seq", series.Seq)
	}
}
