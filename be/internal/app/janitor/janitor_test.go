package janitor_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/app/janitor"
)

type call struct {
	cutoff time.Time
	batch  int32
}

// fakeStore hands out a fixed number of rows per table, in batches, the way a real DELETE ... LIMIT
// does: full batches until the backlog runs out.
type fakeStore struct {
	remaining map[string]int64
	calls     map[string][]call
	fail      string
}

func newFakeStore() *fakeStore {
	return &fakeStore{remaining: map[string]int64{}, calls: map[string][]call{}}
}

func (f *fakeStore) prune(table string, cutoff time.Time, batch int32) (int64, error) {
	f.calls[table] = append(f.calls[table], call{cutoff: cutoff, batch: batch})
	if f.fail == table {
		return 0, errors.New("boom")
	}
	left := f.remaining[table]
	if left > int64(batch) {
		f.remaining[table] = left - int64(batch)
		return int64(batch), nil
	}
	f.remaining[table] = 0
	return left, nil
}

func (f *fakeStore) PruneSearchEvents(_ context.Context, c time.Time, b int32) (int64, error) {
	return f.prune("search_events", c, b)
}

func (f *fakeStore) PrunePlayEvents(_ context.Context, c time.Time, b int32) (int64, error) {
	return f.prune("play_events", c, b)
}

func (f *fakeStore) PruneBrowseTitles(_ context.Context, c time.Time, b int32) (int64, error) {
	return f.prune("browse_titles", c, b)
}

func (f *fakeStore) PruneBrowsePeople(_ context.Context, c time.Time, b int32) (int64, error) {
	return f.prune("browse_people", c, b)
}

func (f *fakeStore) PruneOrphanDeliveries(_ context.Context, c time.Time, b int32) (int64, error) {
	return f.prune("deliveries", c, b)
}

func (f *fakeStore) PruneSettledContentRequests(_ context.Context, c time.Time, b int32) (int64, error) {
	return f.prune("content_requests", c, b)
}

func (f *fakeStore) PruneOrphanFollowedSeriesState(_ context.Context, c time.Time, b int32) (int64, error) {
	return f.prune("followed_series_state", c, b)
}

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testPolicy() janitor.Policy {
	p := janitor.DefaultPolicy()
	p.Batch = 10
	p.MaxBatches = 5
	return p
}

func TestSweepDrainsEachTableUntilTheBacklogIsGone(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.remaining["search_events"] = 25 // three batches: 10, 10, 5
	store.remaining["play_events"] = 4    // one short batch

	res, err := janitor.New(store, testPolicy(), quietLog()).Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if res.SearchEvents != 25 {
		t.Errorf("search_events removed = %d, want 25", res.SearchEvents)
	}
	if n := len(store.calls["search_events"]); n != 3 {
		t.Errorf("search_events took %d batches, want 3", n)
	}
	if res.PlayEvents != 4 {
		t.Errorf("play_events removed = %d, want 4", res.PlayEvents)
	}
	if n := len(store.calls["play_events"]); n != 1 {
		t.Errorf("play_events took %d batches, want 1 -- a short batch means the backlog is gone", n)
	}
	if res.Total() != 29 {
		t.Errorf("Total = %d, want 29", res.Total())
	}
}

// One table with years of backlog must not hold the database for a whole sweep; the rest is picked
// up next time round.
func TestSweepStopsAtMaxBatches(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.remaining["search_events"] = 10_000

	res, err := janitor.New(store, testPolicy(), quietLog()).Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if got, want := res.SearchEvents, int64(50); got != want { // 5 batches of 10
		t.Errorf("removed = %d, want %d", got, want)
	}
	if n := len(store.calls["search_events"]); n != 5 {
		t.Errorf("took %d batches, want the MaxBatches cap of 5", n)
	}
}

// Each table gets the cutoff its own retention says, not one shared cutoff.
func TestSweepUsesPerTableRetention(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	policy := testPolicy()
	before := time.Now()
	if _, err := janitor.New(store, policy, quietLog()).Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	after := time.Now()

	for table, retention := range map[string]time.Duration{
		"search_events": policy.SearchEvents,
		"play_events":   policy.PlayEvents,
		"browse_titles": policy.BrowseTitles,
		"browse_people": policy.BrowsePeople,
		"deliveries":    policy.Deliveries,
	} {
		calls := store.calls[table]
		if len(calls) == 0 {
			t.Fatalf("%s was never swept", table)
		}
		cutoff := calls[0].cutoff
		upper := after.Add(-retention)
		lower := before.Add(-retention).Add(-time.Minute)
		if cutoff.After(upper) || cutoff.Before(lower) {
			t.Errorf("%s cutoff = %v, want roughly now minus %v", table, cutoff, retention)
		}
	}
}

func TestSweepStopsAtTheFirstFailure(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.fail = "play_events"
	store.remaining["browse_titles"] = 5

	_, err := janitor.New(store, testPolicy(), quietLog()).Sweep(context.Background())
	if err == nil {
		t.Fatal("Sweep succeeded with a failing prune")
	}
	if len(store.calls["browse_titles"]) != 0 {
		t.Error("kept sweeping after a failure; a broken sweep should be loud, not partial")
	}
}

func TestRunStopsWhenTheContextDoes(t *testing.T) {
	t.Parallel()

	policy := testPolicy()
	policy.Every = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := janitor.New(newFakeStore(), policy, quietLog()).Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v, want context.Canceled", err)
	}
}
