package api_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
)

// fakeRails is the in-process store these tests run against. The API layer is testable without a
// database and without Telegram, which is the property R-API-3 exists to protect.
type fakeRails struct {
	seq   uint64
	rails map[catalog.RailKind][]catalog.Row
	err   error
}

func (f *fakeRails) Rail(_ context.Context, kind catalog.RailKind, _ catalog.Kind) ([]catalog.Row, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rails[kind], nil
}

func (f *fakeRails) RailSeq(context.Context) (uint64, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.seq, nil
}

func row(id uint64, title string) catalog.Row {
	return catalog.Row{TMDBID: id, Kind: catalog.KindMovie, Title: title, Year: 2020, Rating: 70}
}

// newAPI builds an API for tests that only exercise Home: a no-op delivery store and permissive
// limits, so Home tests do not have to know anything about delivery bookkeeping.
func newAPI(rails api.RailStore) *api.API {
	return api.New(rails, &fakeDeliveries{}, &fakeCatalog{}, &fakeSearch{}, api.RateLimits{PerHour: 30, MaxConcurrent: 2, SearchPerHour: 60})
}

func TestHomeReturnsEveryRail(t *testing.T) {
	t.Parallel()

	store := &fakeRails{
		seq: 9,
		rails: map[catalog.RailKind][]catalog.Row{
			catalog.RailSlider:      {row(1, "a"), row(2, "b")},
			catalog.RailMostWatched: {row(3, "c")},
			catalog.RailTrending:    {row(4, "d")},
		},
	}

	res, err := newAPI(store).Home(context.Background(), api.Principal{}, &sushiv1.HomeReq{
		Tab: sushiv1.Tab_TAB_MOVIES,
	})
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	if len(res.GetRails()) != 3 {
		t.Fatalf("got %d rails, want 3", len(res.GetRails()))
	}
	if res.GetSeq() != 9 {
		t.Errorf("seq = %d, want 9", res.GetSeq())
	}
	if res.GetTtlSeconds() != api.HomeTTL {
		t.Errorf("ttl = %d, want %d", res.GetTtlSeconds(), api.HomeTTL)
	}
	if k := res.GetRails()[0].GetKind(); k != sushiv1.RailKind_RAIL_KIND_SLIDER {
		t.Errorf("first rail = %v, want the slider", k)
	}
}

func TestHomePutsLatestWatchLaterFirstOnTheSlider(t *testing.T) {
	t.Parallel()

	later := catalog.Row{TMDBID: 550, Kind: catalog.KindMovie, Title: "Fight Club", Year: 1999}
	store := &fakeRails{
		seq: 3,
		rails: map[catalog.RailKind][]catalog.Row{
			catalog.RailSlider:      {row(1, "a"), later},
			catalog.RailMostWatched: {row(2, "b")},
		},
	}
	svc := newAPI(store).WithBrowse(&fakeBrowse{later: []catalog.Row{later}})
	res, err := svc.Home(context.Background(), api.Principal{UserID: 9}, &sushiv1.HomeReq{
		Tab: sushiv1.Tab_TAB_MOVIES,
	})
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	slider := res.GetRails()[0].GetRows()
	if len(slider) < 1 || slider[0].GetTmdbId() != 550 {
		t.Fatalf("slider[0] = %+v, want TMDB 550 first", slider)
	}
	seen := 0
	for _, r := range slider {
		if r.GetTmdbId() == 550 {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("Fight Club appeared %d times on the slider", seen)
	}
}

func TestHomeSkipsTheAnswerWhenTheClientIsCurrent(t *testing.T) {
	t.Parallel()

	store := &fakeRails{seq: 5, rails: map[catalog.RailKind][]catalog.Row{
		catalog.RailSlider: {row(1, "a")},
	}}

	res, err := newAPI(store).Home(context.Background(), api.Principal{}, &sushiv1.HomeReq{
		Tab:      sushiv1.Tab_TAB_MOVIES,
		SinceSeq: 5,
	})
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	// Re-sending three rails the client would discard costs a message it could have spent on
	// something the user actually asked for.
	if len(res.GetRails()) != 0 {
		t.Errorf("got %d rails for an up-to-date client, want none", len(res.GetRails()))
	}
	if res.GetSeq() != 5 {
		t.Errorf("seq = %d, want the current watermark", res.GetSeq())
	}
}

func TestHomeDropsNotInterested(t *testing.T) {
	t.Parallel()

	hidden := row(7, "nope")
	store := &fakeRails{
		seq: 2,
		rails: map[catalog.RailKind][]catalog.Row{
			catalog.RailSlider:      {hidden, row(1, "a")},
			catalog.RailMostWatched: {hidden, row(2, "b")},
			catalog.RailTrending:    {hidden},
		},
	}
	svc := newAPI(store).WithBrowse(&fakeBrowse{notInterested: []catalog.Row{hidden}})
	res, err := svc.Home(context.Background(), api.Principal{UserID: 9}, &sushiv1.HomeReq{
		Tab: sushiv1.Tab_TAB_MOVIES,
	})
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	for _, rail := range res.GetRails() {
		for _, r := range rail.GetRows() {
			if r.GetTmdbId() == 7 {
				t.Fatalf("hidden title still on %v", rail.GetKind())
			}
		}
	}
	if len(res.GetNotInterested()) != 1 || res.GetNotInterested()[0].GetTmdbId() != 7 {
		t.Fatalf("not_interested = %+v", res.GetNotInterested())
	}
	// Trending was only the hidden title, so that rail is omitted (R-RAIL-5).
	for _, rail := range res.GetRails() {
		if rail.GetKind() == sushiv1.RailKind_RAIL_KIND_TRENDING {
			t.Fatal("empty trending rail was sent")
		}
	}
}

func TestHomeShowsAnItemOnce(t *testing.T) {
	t.Parallel()

	// The same film is genuinely in all three rails - with a few thousand titles this is common,
	// and a home screen showing it three times reads as broken, not popular (R-RAIL-1).
	dup := row(42, "everywhere")
	store := &fakeRails{
		seq: 1,
		rails: map[catalog.RailKind][]catalog.Row{
			catalog.RailSlider:      {dup, row(1, "a")},
			catalog.RailMostWatched: {dup, row(2, "b")},
			catalog.RailTrending:    {dup},
		},
	}

	res, err := newAPI(store).Home(context.Background(), api.Principal{}, &sushiv1.HomeReq{
		Tab: sushiv1.Tab_TAB_MOVIES,
	})
	if err != nil {
		t.Fatalf("Home: %v", err)
	}

	seen := map[uint64]int{}
	for _, rail := range res.GetRails() {
		for _, r := range rail.GetRows() {
			seen[r.GetTmdbId()]++
		}
	}
	if seen[42] != 1 {
		t.Errorf("tmdb 42 appears %d times, want exactly 1", seen[42])
	}
	// And the rail left with nothing is dropped rather than sent empty (R-RAIL-5).
	for _, rail := range res.GetRails() {
		if len(rail.GetRows()) == 0 {
			t.Errorf("rail %v was sent with no rows", rail.GetKind())
		}
	}
}

func TestHomeRejectsAnUnspecifiedTab(t *testing.T) {
	t.Parallel()

	_, err := newAPI(&fakeRails{}).Home(context.Background(), api.Principal{}, &sushiv1.HomeReq{})
	if err == nil {
		t.Fatal("want an error; defaulting the tab would hide a client bug behind a screen that looks right")
	}
}

func TestHomeSurfacesStoreFailures(t *testing.T) {
	t.Parallel()

	want := errors.New("database is down")
	_, err := newAPI(&fakeRails{err: want}).Home(context.Background(), api.Principal{}, &sushiv1.HomeReq{
		Tab: sushiv1.Tab_TAB_MOVIES,
	})
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want it to wrap %v", err, want)
	}
}
