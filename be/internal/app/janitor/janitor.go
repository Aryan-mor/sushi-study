// Package janitor keeps the tables that only ever grew from only ever growing.
//
// Five of them had no retention at all (docs/06 §7). `play_events` and `search_events` are
// append-only and read over a window -- thirty days for most-watched, one hour for the search cap
// -- so everything older than that window is weight nothing will ever read. `browse_titles` and
// `browse_people` are TMDB caches whose `fetched_at` nothing consulted, which is also why a
// re-rated or renamed title stayed wrong for ever. `deliveries` accumulates a row per (reader,
// file) and nothing removed the ones belonging to readers that are no longer bound to anybody.
//
// The policy lives here; the DELETEs live in the adapter (db/queries/env/janitor.sql).
package janitor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Store is the retention sweeps. Each returns how many rows it removed, which is what lets a sweep
// know whether to come round again.
type Store interface {
	PruneSearchEvents(ctx context.Context, olderThan time.Time, batch int32) (int64, error)
	PrunePlayEvents(ctx context.Context, olderThan time.Time, batch int32) (int64, error)
	PruneBrowseTitles(ctx context.Context, olderThan time.Time, batch int32) (int64, error)
	PruneBrowsePeople(ctx context.Context, olderThan time.Time, batch int32) (int64, error)
	PruneOrphanDeliveries(ctx context.Context, olderThan time.Time, batch int32) (int64, error)
	PruneSettledContentRequests(ctx context.Context, olderThan time.Time, batch int32) (int64, error)
	PruneOrphanFollowedSeriesState(ctx context.Context, olderThan time.Time, batch int32) (int64, error)
}

// Policy is how long each table is kept. Every number is generous against the window that actually
// reads the table, because the cost of keeping a row too long is disk and the cost of dropping one
// too early is a wrong answer.
type Policy struct {
	// SearchEvents is read over one hour (R-SEC-11's cap). A day is already 24x the window.
	SearchEvents time.Duration
	// PlayEvents is read over rails.Policy.MostWatchedWindowDays, 30 by default. Six months leaves
	// room to widen that window without having thrown the history away first.
	PlayEvents time.Duration
	// BrowseTitles and BrowsePeople are caches of somebody else's data. Dropping a stale row costs
	// one TMDB call the next time that title is opened, and buys a title that is no longer wrong.
	BrowseTitles time.Duration
	BrowsePeople time.Duration
	// Deliveries only ever removes rows whose reader chat has no live binding -- a delivered row is
	// the record that a file already sits in a chat, and dropping a live one costs a re-download.
	Deliveries time.Duration
	// ContentRequests is how long a settled (fulfilled/unavailable) request is kept after it was
	// made (ADR 0014). Pending requests are never pruned.
	ContentRequests time.Duration
	// FollowedSeriesState is the grace before an orphaned debounce row is swept -- long enough not
	// to race a follow still being created.
	FollowedSeriesState time.Duration

	// Batch bounds one DELETE. A sweep that locks a table for a minute is its own outage.
	Batch int32
	// Every is how often Run sweeps.
	Every time.Duration
	// MaxBatches bounds one sweep of one table, so a first run against years of backlog spreads
	// over several sweeps instead of holding the database for all of them.
	MaxBatches int
}

// DefaultPolicy is the retention agreed in docs/06 §7.
func DefaultPolicy() Policy {
	return Policy{
		SearchEvents:        24 * time.Hour,
		PlayEvents:          180 * 24 * time.Hour,
		BrowseTitles:        14 * 24 * time.Hour,
		BrowsePeople:        30 * 24 * time.Hour,
		Deliveries:          90 * 24 * time.Hour,
		ContentRequests:     90 * 24 * time.Hour,
		FollowedSeriesState: 24 * time.Hour,
		Batch:               2000,
		Every:               time.Hour,
		MaxBatches:          50,
	}
}

// Result is what one sweep removed, per table.
type Result struct {
	SearchEvents        int64
	PlayEvents          int64
	BrowseTitles        int64
	BrowsePeople        int64
	Deliveries          int64
	ContentRequests     int64
	FollowedSeriesState int64
}

// Total is every row the sweep removed.
func (r Result) Total() int64 {
	return r.SearchEvents + r.PlayEvents + r.BrowseTitles + r.BrowsePeople + r.Deliveries +
		r.ContentRequests + r.FollowedSeriesState
}

// Janitor sweeps on a schedule. Never on a request path.
type Janitor struct {
	store  Store
	policy Policy
	log    *slog.Logger
	now    func() time.Time
}

// New builds a Janitor.
func New(store Store, policy Policy, log *slog.Logger) *Janitor {
	return &Janitor{store: store, policy: policy, log: log, now: time.Now}
}

// Run sweeps every Policy.Every until ctx is done.
//
// A failed sweep is logged and the next one is scheduled as usual: retention falling behind is not
// a reason to take apid down with it, and the tables it could not trim will still be there in an
// hour. Only ctx ends this.
func (j *Janitor) Run(ctx context.Context) error {
	ticker := time.NewTicker(j.policy.Every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			res, err := j.Sweep(ctx)
			switch {
			case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
				return err
			case err != nil:
				j.log.Error("janitor sweep failed", "err", err, "removed", res.Total())
			case res.Total() > 0:
				j.log.Info("janitor swept",
					"search_events", res.SearchEvents,
					"play_events", res.PlayEvents,
					"browse_titles", res.BrowseTitles,
					"browse_people", res.BrowsePeople,
					"deliveries", res.Deliveries,
					"content_requests", res.ContentRequests,
					"followed_series_state", res.FollowedSeriesState)
			}
		}
	}
}

// Sweep runs every prune once, oldest-first, and reports what it removed. It stops at the first
// failure so a broken sweep is loud rather than partially silent.
func (j *Janitor) Sweep(ctx context.Context) (Result, error) {
	now := j.now()
	var res Result
	var err error

	if res.SearchEvents, err = j.drain(ctx, j.store.PruneSearchEvents, now.Add(-j.policy.SearchEvents)); err != nil {
		return res, fmt.Errorf("janitor: search_events: %w", err)
	}
	if res.PlayEvents, err = j.drain(ctx, j.store.PrunePlayEvents, now.Add(-j.policy.PlayEvents)); err != nil {
		return res, fmt.Errorf("janitor: play_events: %w", err)
	}
	if res.BrowseTitles, err = j.drain(ctx, j.store.PruneBrowseTitles, now.Add(-j.policy.BrowseTitles)); err != nil {
		return res, fmt.Errorf("janitor: browse_titles: %w", err)
	}
	if res.BrowsePeople, err = j.drain(ctx, j.store.PruneBrowsePeople, now.Add(-j.policy.BrowsePeople)); err != nil {
		return res, fmt.Errorf("janitor: browse_people: %w", err)
	}
	if res.Deliveries, err = j.drain(ctx, j.store.PruneOrphanDeliveries, now.Add(-j.policy.Deliveries)); err != nil {
		return res, fmt.Errorf("janitor: deliveries: %w", err)
	}
	if res.ContentRequests, err = j.drain(ctx, j.store.PruneSettledContentRequests, now.Add(-j.policy.ContentRequests)); err != nil {
		return res, fmt.Errorf("janitor: content_requests: %w", err)
	}
	if res.FollowedSeriesState, err = j.drain(ctx, j.store.PruneOrphanFollowedSeriesState, now.Add(-j.policy.FollowedSeriesState)); err != nil {
		return res, fmt.Errorf("janitor: followed_series_state: %w", err)
	}
	return res, nil
}

type prune func(ctx context.Context, olderThan time.Time, batch int32) (int64, error)

// drain calls one prune until it returns a short batch -- meaning there is nothing left older than
// the cutoff -- or until MaxBatches, so one table's backlog cannot monopolise a sweep.
func (j *Janitor) drain(ctx context.Context, fn prune, cutoff time.Time) (int64, error) {
	var total int64
	for i := 0; i < j.policy.MaxBatches; i++ {
		n, err := fn(ctx, cutoff, j.policy.Batch)
		if err != nil {
			return total, err
		}
		total += n
		if n < int64(j.policy.Batch) {
			return total, nil
		}
	}
	return total, nil
}
