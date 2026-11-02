package pg

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// The retention sweeps (db/queries/env/janitor.sql, internal/app/janitor). Each deletes at most
// batch rows older than the cutoff and reports how many it removed, which is how the janitor knows
// whether to come round again.

// PruneSearchEvents drops search history past the window the R-SEC-11 cap reads.
func (s *Store) PruneSearchEvents(ctx context.Context, olderThan time.Time, batch int32) (int64, error) {
	n, err := s.q.PruneSearchEvents(ctx, PruneSearchEventsParams{
		Cutoff: stamp(olderThan), BatchSize: batch,
	})
	if err != nil {
		return 0, fmt.Errorf("prune search_events: %w", err)
	}
	return n, nil
}

// PrunePlayEvents drops play history past the most-watched window.
func (s *Store) PrunePlayEvents(ctx context.Context, olderThan time.Time, batch int32) (int64, error) {
	n, err := s.q.PrunePlayEvents(ctx, PrunePlayEventsParams{
		Cutoff: stamp(olderThan), BatchSize: batch,
	})
	if err != nil {
		return 0, fmt.Errorf("prune play_events: %w", err)
	}
	return n, nil
}

// PruneBrowseTitles expires the TMDB title cache.
func (s *Store) PruneBrowseTitles(ctx context.Context, olderThan time.Time, batch int32) (int64, error) {
	n, err := s.q.PruneBrowseTitles(ctx, PruneBrowseTitlesParams{
		Cutoff: stamp(olderThan), BatchSize: batch,
	})
	if err != nil {
		return 0, fmt.Errorf("prune browse_titles: %w", err)
	}
	return n, nil
}

// PruneBrowsePeople expires the TMDB person cache.
func (s *Store) PruneBrowsePeople(ctx context.Context, olderThan time.Time, batch int32) (int64, error) {
	n, err := s.q.PruneBrowsePeople(ctx, PruneBrowsePeopleParams{
		Cutoff: stamp(olderThan), BatchSize: batch,
	})
	if err != nil {
		return 0, fmt.Errorf("prune browse_people: %w", err)
	}
	return n, nil
}

// PruneOrphanDeliveries drops delivery rows whose reader chat is no longer bound to anybody -- the
// only delivery rows that can never be useful again.
func (s *Store) PruneOrphanDeliveries(ctx context.Context, olderThan time.Time, batch int32) (int64, error) {
	n, err := s.q.PruneOrphanDeliveries(ctx, PruneOrphanDeliveriesParams{
		Cutoff: stamp(olderThan), BatchSize: batch,
	})
	if err != nil {
		return 0, fmt.Errorf("prune deliveries: %w", err)
	}
	return n, nil
}

// PruneSettledContentRequests drops fulfilled/unavailable requests past the retention window
// (ADR 0014). Pending rows are left alone.
func (s *Store) PruneSettledContentRequests(ctx context.Context, olderThan time.Time, batch int32) (int64, error) {
	n, err := s.q.PruneSettledContentRequests(ctx, PruneSettledContentRequestsParams{
		Cutoff: stamp(olderThan), BatchSize: batch,
	})
	if err != nil {
		return 0, fmt.Errorf("prune content_requests: %w", err)
	}
	return n, nil
}

// PruneOrphanFollowedSeriesState drops debounce rows for series nobody follows any more.
func (s *Store) PruneOrphanFollowedSeriesState(ctx context.Context, olderThan time.Time, batch int32) (int64, error) {
	n, err := s.q.PruneOrphanFollowedSeriesState(ctx, PruneOrphanFollowedSeriesStateParams{
		Cutoff: stamp(olderThan), BatchSize: batch,
	})
	if err != nil {
		return 0, fmt.Errorf("prune followed_series_state: %w", err)
	}
	return n, nil
}

func stamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}
