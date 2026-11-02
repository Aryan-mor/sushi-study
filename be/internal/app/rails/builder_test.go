package rails_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/app/rails"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

// fakeStore records what a build did, and can be told to fail one fill.
type fakeStore struct {
	calls    []string
	failOn   string
	rows     int64
	rolledTo []string
}

func (f *fakeStore) note(name string) error {
	f.calls = append(f.calls, name)
	if f.failOn == name {
		return errors.New("boom")
	}
	return nil
}

func (f *fakeStore) ClearTab(_ context.Context, _ catalog.Kind) error { return f.note("clear") }

func (f *fakeStore) FillNewest(_ context.Context, _ catalog.RailKind, _ catalog.Kind, _, _ int32) (int64, error) {
	return f.rows, f.note("newest")
}

func (f *fakeStore) FillMostWatched(_ context.Context, _ catalog.RailKind, _ catalog.Kind, _, _, _, _ int32) (int64, error) {
	return f.rows, f.note("watched")
}

func (f *fakeStore) FillByTMDBIDs(_ context.Context, _ catalog.RailKind, _ catalog.Kind, _ int32, _ []uint64, _ int32) (int64, error) {
	return f.rows, f.note("tmdb")
}

func (f *fakeStore) FillRandom(_ context.Context, _ catalog.RailKind, _ catalog.Kind, _, _ int32) (int64, error) {
	return f.rows, f.note("random")
}

func (f *fakeStore) BumpSeq(_ context.Context) (uint64, error) {
	return 42, f.note("bump")
}

// fakeTrending is a TrendingSource that returns fixed ids, or fails once told to.
type fakeTrending struct {
	ids []uint64
	err error
}

func (f *fakeTrending) TrendingWeek(_ context.Context, _ catalog.Kind) ([]uint64, error) {
	return f.ids, f.err
}

// tx is a TxRunner that records whether the work committed or rolled back, which is the whole
// property under test: readers must never see the cleared-but-not-yet-refilled state.
type tx struct {
	store     *fakeStore
	committed bool
	rolled    bool
}

func (t *tx) run(ctx context.Context, fn func(rails.Store) error) error {
	if err := fn(t.store); err != nil {
		t.rolled = true
		return err
	}
	t.committed = true
	return nil
}

func TestBuildTabCommitsOnceEveryRailIsFilled(t *testing.T) {
	t.Parallel()

	store := &fakeStore{rows: 20}
	runner := &tx{store: store}

	res, err := rails.New(runner.run, nil, rails.DefaultPolicy()).BuildTab(context.Background(), catalog.KindMovie)
	if err != nil {
		t.Fatalf("BuildTab: %v", err)
	}
	if !runner.committed || runner.rolled {
		t.Fatalf("committed=%v rolled=%v, want a single commit", runner.committed, runner.rolled)
	}
	if res.Seq != 42 {
		t.Errorf("Seq = %d, want the bumped watermark", res.Seq)
	}
	if store.calls[0] != "clear" {
		t.Errorf("first call was %q, want the tab cleared first", store.calls[0])
	}
	if last := store.calls[len(store.calls)-1]; last != "bump" {
		t.Errorf("last call was %q, want the watermark bumped last", last)
	}
}

// The bug this replaced: ClearTab ran outside any transaction, so a failure anywhere after it left
// the tab empty and the home screen blank until the next scheduled build.
func TestBuildTabRollsBackAndLeavesNothingHalfBuilt(t *testing.T) {
	t.Parallel()

	store := &fakeStore{rows: 20, failOn: "watched"}
	runner := &tx{store: store}

	res, err := rails.New(runner.run, nil, rails.DefaultPolicy()).BuildTab(context.Background(), catalog.KindSeries)
	if err == nil {
		t.Fatal("BuildTab succeeded with a failing fill")
	}
	if runner.committed {
		t.Error("committed a build that failed partway")
	}
	if !runner.rolled {
		t.Error("did not roll back a build that failed partway")
	}
	// Nothing survived the rollback, so nothing is reported as placed.
	if res.Slider != 0 || res.Watched != 0 || res.Trending != 0 || res.Filler != 0 || res.Seq != 0 {
		t.Errorf("Result = %+v, want it empty after a rollback", res)
	}
	if res.Tab != catalog.KindSeries {
		t.Errorf("Tab = %v, want the tab that was attempted", res.Tab)
	}
}

func TestBuildTabRefusesSomethingThatIsNotATab(t *testing.T) {
	t.Parallel()

	runner := &tx{store: &fakeStore{}}
	if _, err := rails.New(runner.run, nil, rails.DefaultPolicy()).BuildTab(context.Background(), catalog.Kind(9)); err == nil {
		t.Fatal("accepted a kind that is not a tab")
	}
	if runner.committed || runner.rolled {
		t.Error("opened a transaction for a request that was invalid before any work started")
	}
}

// A short trending rail is topped up with random titles, and that top-up is part of the same
// transaction as everything else.
func TestShortTrendingRailIsToppedUpInsideTheSameTransaction(t *testing.T) {
	t.Parallel()

	store := &fakeStore{rows: 1}
	runner := &tx{store: store}

	res, err := rails.New(runner.run, nil, rails.DefaultPolicy()).BuildTab(context.Background(), catalog.KindMovie)
	if err != nil {
		t.Fatalf("BuildTab: %v", err)
	}
	if res.Filler == 0 {
		t.Error("a one-card trending rail was shipped without filler")
	}
	var sawRandom bool
	for _, c := range store.calls {
		if c == "random" {
			sawRandom = true
		}
	}
	if !sawRandom {
		t.Error("filler was reported but FillRandom was never called")
	}
	if !runner.committed {
		t.Error("the top-up landed outside the build's transaction")
	}
}

// A healthy TMDB trending list is placed via the intersection, not the all-time-plays top-up.
func TestTrendingUsesTheTMDBIntersectionWhenItIsHealthy(t *testing.T) {
	t.Parallel()

	store := &fakeStore{rows: 18}
	runner := &tx{store: store}
	trending := &fakeTrending{ids: []uint64{1, 2, 3}}

	res, err := rails.New(runner.run, trending, rails.DefaultPolicy()).BuildTab(context.Background(), catalog.KindMovie)
	if err != nil {
		t.Fatalf("BuildTab: %v", err)
	}
	if res.Trending != 18 || res.Filler != 0 {
		t.Errorf("Trending = %d, Filler = %d, want a full rail from the intersection alone", res.Trending, res.Filler)
	}
	var sawTMDB, sawWatchedForTrending bool
	for _, c := range store.calls {
		if c == "tmdb" {
			sawTMDB = true
		}
	}
	// "watched" also fires once for rail 3 (most watched); a second call would mean the top-up
	// ran even though the intersection alone was already full.
	watchedCalls := 0
	for _, c := range store.calls {
		if c == "watched" {
			watchedCalls++
		}
	}
	sawWatchedForTrending = watchedCalls > 1
	if !sawTMDB {
		t.Error("FillByTMDBIDs was never called with a configured TrendingSource")
	}
	if sawWatchedForTrending {
		t.Error("the all-time-plays top-up ran even though the TMDB intersection filled the rail")
	}
}

// A TMDB outage must not fail the rebuild (R-RAIL-3): it degrades to the same floor a thin
// intersection would need anyway.
func TestTrendingDegradesToTheFloorOnATMDBError(t *testing.T) {
	t.Parallel()

	store := &fakeStore{rows: 20}
	runner := &tx{store: store}
	trending := &fakeTrending{err: errors.New("tmdb is down")}

	res, err := rails.New(runner.run, trending, rails.DefaultPolicy()).BuildTab(context.Background(), catalog.KindMovie)
	if err != nil {
		t.Fatalf("BuildTab: %v, want a TMDB error to degrade rather than fail the build", err)
	}
	if !runner.committed {
		t.Error("a TMDB error rolled back the whole rebuild instead of degrading")
	}
	if res.Trending == 0 {
		t.Error("Trending = 0, want the all-time-plays floor to have filled it")
	}
}

func TestShortMostWatchedRailIsToppedUpInsideTheSameTransaction(t *testing.T) {
	t.Parallel()

	store := &fakeStore{rows: 1}
	runner := &tx{store: store}

	res, err := rails.New(runner.run, nil, rails.DefaultPolicy()).BuildTab(context.Background(), catalog.KindMovie)
	if err != nil {
		t.Fatalf("BuildTab: %v", err)
	}
	if res.Watched < int64(rails.DefaultPolicy().MinRailSize) && res.Filler == 0 {
		t.Error("a short most-watched rail was shipped without filler")
	}
	var sawRandom bool
	for _, c := range store.calls {
		if c == "random" {
			sawRandom = true
		}
	}
	if !sawRandom {
		t.Error("most-watched floor needed FillRandom")
	}
	if !runner.committed {
		t.Error("the top-up landed outside the build's transaction")
	}
}

func TestFullMostWatchedRailSkipsRandomFiller(t *testing.T) {
	t.Parallel()

	store := &fakeStore{rows: 18}
	runner := &tx{store: store}

	if _, err := rails.New(runner.run, nil, rails.DefaultPolicy()).BuildTab(context.Background(), catalog.KindMovie); err != nil {
		t.Fatalf("BuildTab: %v", err)
	}
	for _, c := range store.calls {
		if c == "random" {
			t.Fatal("FillRandom ran on rails that already met MinRailSize")
		}
	}
}

func TestRebuildAllBuildsMovieThenSeries(t *testing.T) {
	t.Parallel()

	store := &fakeStore{rows: 20}
	runner := &tx{store: store}
	movie, series, err := rails.New(runner.run, nil, rails.DefaultPolicy()).RebuildAll(context.Background())
	if err != nil {
		t.Fatalf("RebuildAll: %v", err)
	}
	if movie.Tab != catalog.KindMovie || series.Tab != catalog.KindSeries {
		t.Errorf("tabs = %s/%s, want movie then series", movie.Tab, series.Tab)
	}
	clears := 0
	for _, c := range store.calls {
		if c == "clear" {
			clears++
		}
	}
	if clears != 2 {
		t.Errorf("clears = %d, want one per tab", clears)
	}
}
