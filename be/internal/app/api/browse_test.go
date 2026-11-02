package api_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
)

type fakeHydrate struct {
	item catalog.Item
}

func (f *fakeHydrate) TitlePage(_ context.Context, tmdbID uint64, kind catalog.Kind) (catalog.Item, error) {
	if f.item.TMDBID == tmdbID && f.item.Kind == kind {
		return f.item, nil
	}
	return catalog.Item{}, nil
}

type fakeBrowse struct {
	items  map[itemKey]catalog.Item
	people map[uint64]catalog.PersonPage
	// flags is the watch-later / favourite state Item should surface (ADR 0032).
	flags            map[itemKey]userFlags
	flagsErr         error
	later            []catalog.Row
	progs            map[int64]catalog.EpisodeProgress
	progressErr      error
	lastProgUser     int64
	watched          []catalog.WatchedState
	watchedErr       error
	forYou           []catalog.Row
	forYouErr        error
	notInterested    []catalog.Row
	boxsetMetas      []catalog.BoxsetMeta
	boxsetMetasErr   error
	boxsetItems      []catalog.Row
	boxsetItemsErr   error
	lastListSort     int
	lastListSortDesc bool
	lastListKind     catalog.Kind
}

type userFlags struct{ favorite, watchLater bool }

func (f *fakeBrowse) BrowseItem(_ context.Context, tmdbID uint64, kind catalog.Kind) (catalog.Item, bool, error) {
	item, ok := f.items[itemKey{tmdbID, kind}]
	return item, ok, nil
}

func (f *fakeBrowse) UpsertBrowseItem(_ context.Context, item catalog.Item) error {
	if f.items == nil {
		f.items = map[itemKey]catalog.Item{}
	}
	f.items[itemKey{item.TMDBID, item.Kind}] = item
	return nil
}

func (f *fakeBrowse) BrowsePerson(_ context.Context, tmdbID uint64) (catalog.PersonPage, bool, error) {
	page, ok := f.people[tmdbID]
	return page, ok, nil
}

func (f *fakeBrowse) UpsertBrowsePerson(_ context.Context, page catalog.PersonPage) error {
	if f.people == nil {
		f.people = map[uint64]catalog.PersonPage{}
	}
	f.people[page.TMDBID] = page
	return nil
}

func (f *fakeBrowse) CompactRowsAnyKind(context.Context, []uint64) ([]catalog.Row, error) {
	return nil, nil
}

func (f *fakeBrowse) ListCatalog(_ context.Context, kind catalog.Kind, sort int, sortDesc bool, _ string, _ int, _ string, _ int32, _ int32) ([]catalog.Row, error) {
	f.lastListKind = kind
	f.lastListSort = sort
	f.lastListSortDesc = sortDesc
	return nil, nil
}

func (f *fakeBrowse) ListBoxsetMetas(context.Context, string, int32, int32) ([]catalog.BoxsetMeta, error) {
	return f.boxsetMetas, f.boxsetMetasErr
}

func (f *fakeBrowse) ListBoxsetItems(context.Context, uint64, int32, int32) ([]catalog.Row, error) {
	return f.boxsetItems, f.boxsetItemsErr
}

func (f *fakeBrowse) ListUserFlags(context.Context, int64, string, string, int32, int32) ([]catalog.Row, error) {
	return nil, nil
}

func (f *fakeBrowse) ListForYou(context.Context, int64, int32, int32) ([]catalog.Row, error) {
	return f.forYou, f.forYouErr
}

func (f *fakeBrowse) LatestWatchLater(_ context.Context, _ int64, kind catalog.Kind) (catalog.Row, bool, error) {
	for _, r := range f.later {
		if r.Kind == kind {
			return r, true, nil
		}
	}
	return catalog.Row{}, false, nil
}

func (f *fakeBrowse) UserItemFlags(_ context.Context, _ int64, tmdbID uint64, kind catalog.Kind) (bool, bool, error) {
	if f.flagsErr != nil {
		return false, false, f.flagsErr
	}
	fl := f.flags[itemKey{tmdbID, kind}]
	return fl.favorite, fl.watchLater, nil
}

func (f *fakeBrowse) SetFavorite(context.Context, int64, uint64, catalog.Kind, bool) error {
	return nil
}

func (f *fakeBrowse) SetWatchLater(context.Context, int64, uint64, catalog.Kind, bool) error {
	return nil
}

func (f *fakeBrowse) SetNotInterested(context.Context, int64, uint64, catalog.Kind, bool) error {
	return nil
}

func (f *fakeBrowse) ListNotInterested(context.Context, int64) ([]catalog.Row, error) {
	return f.notInterested, nil
}

func (f *fakeBrowse) SetFollow(context.Context, int64, uint64, bool) error {
	return nil
}

func (f *fakeBrowse) SetEpisodeDone(_ context.Context, userID, episodeID int64, done bool) error {
	if f.progs == nil {
		f.progs = map[int64]catalog.EpisodeProgress{}
	}
	f.progs[episodeID] = catalog.EpisodeProgress{Done: done}
	f.lastProgUser = userID
	return nil
}

func (f *fakeBrowse) SetProgress(_ context.Context, userID, episodeID, fileID int64, positionS, durationS int32) error {
	if f.progs == nil {
		f.progs = map[int64]catalog.EpisodeProgress{}
	}
	f.progs[episodeID] = catalog.EpisodeProgress{
		PositionS:  positionS,
		Done:       catalog.EpisodeWatched(positionS, durationS),
		LastFileID: fileID,
	}
	f.lastProgUser = userID
	return nil
}

func (f *fakeBrowse) EpisodeProgress(_ context.Context, _, episodeID int64) (catalog.EpisodeProgress, bool, error) {
	if f.progressErr != nil {
		return catalog.EpisodeProgress{}, false, f.progressErr
	}
	p, ok := f.progs[episodeID]
	return p, ok, nil
}

func (f *fakeBrowse) WatchedStateSince(_ context.Context, _, watermark int64, limit int32) ([]catalog.WatchedState, error) {
	if f.watchedErr != nil {
		return nil, f.watchedErr
	}
	out := make([]catalog.WatchedState, 0, len(f.watched))
	for _, w := range f.watched {
		if w.Seq > watermark {
			out = append(out, w)
		}
	}
	if int32(len(out)) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeBrowse) ListPlaylists(context.Context, int64) ([]catalog.PlaylistMeta, error) {
	return nil, nil
}

func (f *fakeBrowse) ListPlaylistItems(context.Context, int64, int32, int32) ([]catalog.Row, error) {
	return nil, nil
}

// The title page has to be able to read a watch-later flag the device never set, because the
// announce channel's button writes it server-side (ADR 0032). Without this, tapping "watch later"
// in the channel and then opening the title would show an empty bookmark.
func TestItemCarriesTheViewersOwnFlags(t *testing.T) {
	t.Parallel()

	item := catalog.Item{Row: catalog.Row{TMDBID: 603, Kind: catalog.KindMovie, Title: "The Matrix"}}
	catalogStore := &fakeCatalog{items: map[itemKey]catalog.Item{{603, catalog.KindMovie}: item}}
	browse := &fakeBrowse{flags: map[itemKey]userFlags{
		{603, catalog.KindMovie}: {favorite: true, watchLater: true},
	}}
	a := api.New(&fakeRails{}, &fakeDeliveries{}, catalogStore, &fakeSearch{},
		api.RateLimits{PerHour: 30, MaxConcurrent: 2, SearchPerHour: 60}).WithBrowse(browse)

	req := &sushiv1.ItemReq{TmdbId: 603, Kind: sushiv1.Kind_KIND_MOVIE}
	res, err := a.Item(context.Background(), api.Principal{UserID: 7}, req)
	if err != nil {
		t.Fatalf("Item: %v", err)
	}
	if !res.GetWatchLater() || !res.GetFavorite() {
		t.Errorf("watch_later=%v favorite=%v, want both true", res.GetWatchLater(), res.GetFavorite())
	}

	// An unbound principal has no flags to read, and must not be looked up as user zero.
	res, err = a.Item(context.Background(), api.Principal{}, req)
	if err != nil {
		t.Fatalf("Item unbound: %v", err)
	}
	if res.GetWatchLater() || res.GetFavorite() {
		t.Error("an unbound caller was given somebody's flags")
	}

	// An unreadable bookmark is cosmetic; a title page that will not open is not.
	browse.flagsErr = errors.New("db down")
	res, err = a.Item(context.Background(), api.Principal{UserID: 7}, req)
	if err != nil {
		t.Fatalf("a failed flag read should not fail the title page: %v", err)
	}
	if res.GetRow().GetTitle() != "The Matrix" {
		t.Error("the page should still render without the flags")
	}
}

// Cross-device watched-state sync (docs/11 §6.1): the reply only carries what changed since the
// caller's watermark, and the new watermark is the last row's seq, not just "did anything change".
func TestSyncReturnsOnlyRowsPastWatermark(t *testing.T) {
	t.Parallel()
	browse := &fakeBrowse{watched: []catalog.WatchedState{
		{EpisodeID: 1, Done: true, Seq: 10},
		{EpisodeID: 2, Done: false, Seq: 20},
		{EpisodeID: 3, Done: true, Seq: 30},
	}}
	a := api.New(&fakeRails{}, &fakeDeliveries{}, &fakeCatalog{}, &fakeSearch{},
		api.RateLimits{PerHour: 30, MaxConcurrent: 2, SearchPerHour: 60}).WithBrowse(browse)

	res, err := a.Sync(context.Background(), api.Principal{UserID: 7}, &sushiv1.SyncReq{WatchedWatermark: 10})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(res.GetWatched()) != 2 || res.GetWatched()[0].GetEpisodeId() != 2 || res.GetWatched()[1].GetEpisodeId() != 3 {
		t.Fatalf("watched = %+v, want episodes 2 and 3", res.GetWatched())
	}
	if res.GetWatchedWatermark() != 30 {
		t.Fatalf("watermark = %d, want 30 (the last row's seq)", res.GetWatchedWatermark())
	}
	if res.GetWatchedMore() {
		t.Error("watched_more should be false when nothing was capped")
	}

	// Nothing new: watermark echoes back unchanged rather than resetting to zero.
	res, err = a.Sync(context.Background(), api.Principal{UserID: 7}, &sushiv1.SyncReq{WatchedWatermark: 30})
	if err != nil {
		t.Fatalf("Sync at head: %v", err)
	}
	if len(res.GetWatched()) != 0 || res.GetWatchedWatermark() != 30 {
		t.Fatalf("Sync at head = %+v, want empty watched and watermark unchanged at 30", res)
	}
}

func TestItemHydratesFromTMDBWhenMissing(t *testing.T) {
	t.Parallel()
	catalogStore := &fakeCatalog{items: map[itemKey]catalog.Item{}}
	browse := &fakeBrowse{}
	hydrate := &fakeHydrate{item: catalog.Item{
		Row:      catalog.Row{TMDBID: 999, Kind: catalog.KindMovie, Title: "Ghost", Year: 2024, Poster: "g"},
		Overview: "from tmdb",
		Detail: catalog.Detail{
			Logo:           "lg",
			Genres:         "Action",
			People:         []catalog.Person{{Name: "Ada", TMDBID: 1}},
			RelatedPreview: []catalog.Row{{TMDBID: 1, Kind: catalog.KindMovie, Title: "Rel"}},
			SimilarTMDB:    []uint64{1},
		},
		Related: []catalog.Row{{TMDBID: 1, Kind: catalog.KindMovie, Title: "Rel"}},
	}}
	a := api.New(&fakeRails{}, &fakeDeliveries{}, catalogStore, &fakeSearch{}, api.RateLimits{PerHour: 30, MaxConcurrent: 2, SearchPerHour: 60}).
		WithBrowse(browse).
		WithHydrate(hydrate, nil)

	res, err := a.Item(context.Background(), api.Principal{}, &sushiv1.ItemReq{
		TmdbId: 999, Kind: sushiv1.Kind_KIND_MOVIE,
	})
	if err != nil {
		t.Fatalf("Item: %v", err)
	}
	if res.GetRow().GetTitle() != "Ghost" || res.GetOverview() != "from tmdb" {
		t.Fatalf("res = %+v", res)
	}
	if res.GetLogo() != "lg" || res.GetGenres() != "Action" {
		t.Fatalf("logo/genres logo=%q genres=%q", res.GetLogo(), res.GetGenres())
	}
	stored, ok := browse.items[itemKey{999, catalog.KindMovie}]
	if !ok {
		t.Fatal("expected browse upsert")
	}
	if stored.Detail.Logo != "lg" || stored.Detail.Genres != "Action" {
		t.Fatalf("persisted detail incomplete %+v", stored.Detail)
	}

	// Reopen without hydrator: browse cache must serve logo/genres.
	a2 := api.New(&fakeRails{}, &fakeDeliveries{}, catalogStore, &fakeSearch{}, api.RateLimits{PerHour: 30, MaxConcurrent: 2, SearchPerHour: 60}).
		WithBrowse(browse)
	res2, err := a2.Item(context.Background(), api.Principal{}, &sushiv1.ItemReq{
		TmdbId: 999, Kind: sushiv1.Kind_KIND_MOVIE,
	})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if res2.GetLogo() != "lg" || res2.GetGenres() != "Action" {
		t.Fatalf("reopen logo=%q genres=%q", res2.GetLogo(), res2.GetGenres())
	}
}

func TestItemCatalogHitSkipsHydrateWhenExtrasComplete(t *testing.T) {
	t.Parallel()
	catalogStore := &fakeCatalog{items: map[itemKey]catalog.Item{
		{603, catalog.KindMovie}: {
			Row:      catalog.Row{TMDBID: 603, Kind: catalog.KindMovie, Title: "The Matrix"},
			Episodes: []catalog.Episode{{ID: 1}},
			Detail: catalog.Detail{
				Logo:           "word",
				People:         []catalog.Person{{Name: "Keanu", TMDBID: 6384}},
				RelatedPreview: []catalog.Row{{TMDBID: 604, Title: "Reloaded"}},
				Country:        "US",
			},
			Related: []catalog.Row{{TMDBID: 604, Title: "Reloaded"}},
		},
	}}
	hydrate := &fakeHydrate{item: catalog.Item{
		Row: catalog.Row{TMDBID: 603, Kind: catalog.KindMovie, Title: "SHOULD NOT SEE"},
	}}
	a := api.New(&fakeRails{}, &fakeDeliveries{}, catalogStore, &fakeSearch{}, api.RateLimits{PerHour: 30, MaxConcurrent: 2, SearchPerHour: 60}).
		WithHydrate(hydrate, nil)

	res, err := a.Item(context.Background(), api.Principal{}, &sushiv1.ItemReq{
		TmdbId: 603, Kind: sushiv1.Kind_KIND_MOVIE,
	})
	if err != nil {
		t.Fatalf("Item: %v", err)
	}
	if res.GetRow().GetTitle() != "The Matrix" {
		t.Fatalf("got %q", res.GetRow().GetTitle())
	}
	if len(res.GetPeople()) != 1 || res.GetPeople()[0].GetName() != "Keanu" {
		t.Fatalf("people = %+v", res.GetPeople())
	}
}

func TestItemCatalogHitHydratesIncompleteExtras(t *testing.T) {
	t.Parallel()
	catalogStore := &fakeCatalog{items: map[itemKey]catalog.Item{
		{603, catalog.KindMovie}: {
			Row:      catalog.Row{TMDBID: 603, Kind: catalog.KindMovie, Title: "The Matrix"},
			Episodes: []catalog.Episode{{ID: 1}},
		},
	}}
	hydrate := &fakeHydrate{item: catalog.Item{
		Row: catalog.Row{TMDBID: 603, Kind: catalog.KindMovie, Title: "ignored"},
		Detail: catalog.Detail{
			Logo:           "word",
			People:         []catalog.Person{{Name: "Keanu", Role: "Neo"}},
			RelatedPreview: []catalog.Row{{TMDBID: 604, Kind: catalog.KindMovie, Title: "Reloaded"}},
			SimilarTMDB:    []uint64{604},
		},
	}}
	a := api.New(&fakeRails{}, &fakeDeliveries{}, catalogStore, &fakeSearch{}, api.RateLimits{PerHour: 30, MaxConcurrent: 2, SearchPerHour: 60}).
		WithHydrate(hydrate, nil)

	res, err := a.Item(context.Background(), api.Principal{}, &sushiv1.ItemReq{
		TmdbId: 603, Kind: sushiv1.Kind_KIND_MOVIE,
	})
	if err != nil {
		t.Fatalf("Item: %v", err)
	}
	if res.GetRow().GetTitle() != "The Matrix" {
		t.Fatalf("title replaced: %q", res.GetRow().GetTitle())
	}
	if res.GetLogo() != "word" || len(res.GetPeople()) != 1 || res.GetPeople()[0].GetName() != "Keanu" {
		t.Fatalf("extras logo=%q people=%+v", res.GetLogo(), res.GetPeople())
	}
	if len(res.GetRelated()) != 1 || res.GetRelated()[0].GetTmdbId() != 604 {
		t.Fatalf("related = %+v", res.GetRelated())
	}
}

func TestItemRehydratesThinBrowseRow(t *testing.T) {
	t.Parallel()
	catalogStore := &fakeCatalog{items: map[itemKey]catalog.Item{}}
	browse := &fakeBrowse{items: map[itemKey]catalog.Item{
		{999, catalog.KindMovie}: {
			Row: catalog.Row{TMDBID: 999, Kind: catalog.KindMovie, Title: "Ghost"},
		},
	}}
	hydrate := &fakeHydrate{item: catalog.Item{
		Row:      catalog.Row{TMDBID: 999, Kind: catalog.KindMovie, Title: "Ghost", Year: 2024, Poster: "g"},
		Overview: "from tmdb",
		Detail: catalog.Detail{
			Logo:           "lg",
			People:         []catalog.Person{{Name: "Ada"}},
			RelatedPreview: []catalog.Row{{TMDBID: 1, Kind: catalog.KindMovie, Title: "Rel"}},
			SimilarTMDB:    []uint64{1},
		},
	}}
	a := api.New(&fakeRails{}, &fakeDeliveries{}, catalogStore, &fakeSearch{}, api.RateLimits{PerHour: 30, MaxConcurrent: 2, SearchPerHour: 60}).
		WithBrowse(browse).
		WithHydrate(hydrate, nil)

	res, err := a.Item(context.Background(), api.Principal{}, &sushiv1.ItemReq{
		TmdbId: 999, Kind: sushiv1.Kind_KIND_MOVIE,
	})
	if err != nil {
		t.Fatalf("Item: %v", err)
	}
	if res.GetLogo() != "lg" || len(res.GetPeople()) != 1 || res.GetOverview() != "from tmdb" {
		t.Fatalf("res extras incomplete: logo=%q people=%+v overview=%q", res.GetLogo(), res.GetPeople(), res.GetOverview())
	}
	if len(browse.items[itemKey{999, catalog.KindMovie}].Detail.People) != 1 {
		t.Fatal("expected browse upsert of hydrated detail")
	}
}

type fakePersonHydrate struct {
	page  catalog.PersonPage
	calls int
}

func (f *fakePersonHydrate) PersonPage(_ context.Context, personID uint64) (catalog.PersonPage, error) {
	f.calls++
	if f.page.TMDBID == personID {
		return f.page, nil
	}
	return catalog.PersonPage{}, nil
}

func TestPersonHydratesAndPersists(t *testing.T) {
	t.Parallel()
	browse := &fakeBrowse{}
	person := &fakePersonHydrate{page: catalog.PersonPage{
		TMDBID: 6384,
		Name:   "Keanu Reeves",
		Movies: []catalog.Row{{TMDBID: 603, Kind: catalog.KindMovie, Title: "The Matrix"}},
	}}
	a := api.New(&fakeRails{}, &fakeDeliveries{}, &fakeCatalog{}, &fakeSearch{}, api.RateLimits{PerHour: 30, MaxConcurrent: 2, SearchPerHour: 60}).
		WithBrowse(browse).
		WithHydrate(nil, person)

	res, err := a.Person(context.Background(), api.Principal{}, &sushiv1.PersonReq{TmdbId: 6384})
	if err != nil {
		t.Fatalf("Person: %v", err)
	}
	if res.GetName() != "Keanu Reeves" || len(res.GetMovies()) != 1 {
		t.Fatalf("res = %+v", res)
	}
	if _, ok := browse.people[6384]; !ok {
		t.Fatal("expected browse_people upsert")
	}
	if person.calls != 1 {
		t.Fatalf("calls = %d", person.calls)
	}
}

func TestPersonClipsFilmographyForWire(t *testing.T) {
	t.Parallel()
	movies := make([]catalog.Row, 20)
	series := make([]catalog.Row, 20)
	for i := range movies {
		movies[i] = catalog.Row{TMDBID: uint64(1000 + i), Kind: catalog.KindMovie, Title: "M"}
		series[i] = catalog.Row{TMDBID: uint64(2000 + i), Kind: catalog.KindSeries, Title: "S"}
	}
	bio := strings.Repeat("x", 2000)
	browse := &fakeBrowse{people: map[uint64]catalog.PersonPage{
		1: {TMDBID: 1, Name: "Big", Biography: bio, Movies: movies, Series: series},
	}}
	a := api.New(&fakeRails{}, &fakeDeliveries{}, &fakeCatalog{}, &fakeSearch{}, api.RateLimits{PerHour: 30, MaxConcurrent: 2, SearchPerHour: 60}).
		WithBrowse(browse)

	res, err := a.Person(context.Background(), api.Principal{}, &sushiv1.PersonReq{TmdbId: 1})
	if err != nil {
		t.Fatalf("Person: %v", err)
	}
	if len(res.GetMovies()) != 12 || len(res.GetSeries()) != 12 {
		t.Fatalf("want 12+12 filmography, got %d+%d", len(res.GetMovies()), len(res.GetSeries()))
	}
	if utf8.RuneCountInString(res.GetBiography()) != 800 {
		t.Fatalf("biography runes = %d, want 800", utf8.RuneCountInString(res.GetBiography()))
	}
}

func TestPersonUsesCache(t *testing.T) {
	t.Parallel()
	browse := &fakeBrowse{people: map[uint64]catalog.PersonPage{
		6384: {
			TMDBID: 6384,
			Name:   "Keanu Reeves",
			Movies: []catalog.Row{{TMDBID: 603, Kind: catalog.KindMovie, Title: "The Matrix"}},
		},
	}}
	person := &fakePersonHydrate{page: catalog.PersonPage{TMDBID: 6384, Name: "SHOULD NOT SEE"}}
	a := api.New(&fakeRails{}, &fakeDeliveries{}, &fakeCatalog{}, &fakeSearch{}, api.RateLimits{PerHour: 30, MaxConcurrent: 2, SearchPerHour: 60}).
		WithBrowse(browse).
		WithHydrate(nil, person)

	res, err := a.Person(context.Background(), api.Principal{}, &sushiv1.PersonReq{TmdbId: 6384})
	if err != nil {
		t.Fatalf("Person: %v", err)
	}
	if res.GetName() != "Keanu Reeves" {
		t.Fatalf("got %q", res.GetName())
	}
	if person.calls != 0 {
		t.Fatalf("cache miss hit TMDB: calls=%d", person.calls)
	}
}

func TestItemCatalogHitHydratesPeopleWithoutIDs(t *testing.T) {
	t.Parallel()
	catalogStore := &fakeCatalog{items: map[itemKey]catalog.Item{
		{603, catalog.KindMovie}: {
			Row:      catalog.Row{TMDBID: 603, Kind: catalog.KindMovie, Title: "The Matrix"},
			Episodes: []catalog.Episode{{ID: 1}},
			Detail: catalog.Detail{
				People:         []catalog.Person{{Name: "Keanu"}},
				RelatedPreview: []catalog.Row{{TMDBID: 604, Title: "Reloaded"}},
			},
			Related: []catalog.Row{{TMDBID: 604, Title: "Reloaded"}},
		},
	}}
	hydrate := &fakeHydrate{item: catalog.Item{
		Row: catalog.Row{TMDBID: 603, Kind: catalog.KindMovie, Title: "ignored"},
		Detail: catalog.Detail{
			People: []catalog.Person{{Name: "Keanu Reeves", TMDBID: 6384, Role: "Neo"}},
		},
	}}
	a := api.New(&fakeRails{}, &fakeDeliveries{}, catalogStore, &fakeSearch{}, api.RateLimits{PerHour: 30, MaxConcurrent: 2, SearchPerHour: 60}).
		WithHydrate(hydrate, nil)

	res, err := a.Item(context.Background(), api.Principal{}, &sushiv1.ItemReq{
		TmdbId: 603, Kind: sushiv1.Kind_KIND_MOVIE,
	})
	if err != nil {
		t.Fatalf("Item: %v", err)
	}
	if len(res.GetPeople()) != 1 || res.GetPeople()[0].GetTmdbId() != 6384 {
		t.Fatalf("people = %+v", res.GetPeople())
	}
}

func TestEventsAppliesProg(t *testing.T) {
	t.Parallel()

	browse := &fakeBrowse{}
	a := api.New(&fakeRails{}, &fakeDeliveries{}, &fakeCatalog{}, &fakeSearch{},
		api.RateLimits{PerHour: 30, MaxConcurrent: 2, SearchPerHour: 60}).WithBrowse(browse)

	err := a.Events(context.Background(), api.Principal{UserID: 7}, &sushiv1.Events{
		Events: []*sushiv1.Event{{
			Body: &sushiv1.Event_Prog{Prog: &sushiv1.ProgEvent{
				EpisodeId: 900, PositionS: 1200, FileId: 3, DurationS: 7200,
			}},
		}},
	})
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	got, ok := browse.progs[900]
	if !ok {
		t.Fatal("prog was not stored")
	}
	if got.PositionS != 1200 || got.LastFileID != 3 || got.Done {
		t.Errorf("progress = %+v, want position 1200 file 3 not done", got)
	}
	if browse.lastProgUser != 7 {
		t.Errorf("user = %d, want 7", browse.lastProgUser)
	}

	err = a.Events(context.Background(), api.Principal{UserID: 7}, &sushiv1.Events{
		Events: []*sushiv1.Event{{
			Body: &sushiv1.Event_Prog{Prog: &sushiv1.ProgEvent{
				EpisodeId: 900, PositionS: 6500, FileId: 3, DurationS: 7200,
			}},
		}},
	})
	if err != nil {
		t.Fatalf("Events done: %v", err)
	}
	if !browse.progs[900].Done {
		t.Error("90% must mark done")
	}
}

func TestEventsAppliesWatchedMark(t *testing.T) {
	t.Parallel()

	browse := &fakeBrowse{}
	a := api.New(&fakeRails{}, &fakeDeliveries{}, &fakeCatalog{}, &fakeSearch{},
		api.RateLimits{PerHour: 30, MaxConcurrent: 2, SearchPerHour: 60}).WithBrowse(browse)

	err := a.Events(context.Background(), api.Principal{UserID: 7}, &sushiv1.Events{
		Events: []*sushiv1.Event{{
			Body: &sushiv1.Event_Watched{Watched: &sushiv1.WatchedEvent{
				EpisodeId: 591, Done: true,
			}},
		}},
	})
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	got, ok := browse.progs[591]
	if !ok || !got.Done || got.PositionS != 0 {
		t.Fatalf("watched mark = %+v ok=%v, want done position 0", got, ok)
	}
	if browse.lastProgUser != 7 {
		t.Errorf("user = %d, want 7", browse.lastProgUser)
	}

	err = a.Events(context.Background(), api.Principal{UserID: 7}, &sushiv1.Events{
		Events: []*sushiv1.Event{{
			Body: &sushiv1.Event_Watched{Watched: &sushiv1.WatchedEvent{
				EpisodeId: 591, Done: false,
			}},
		}},
	})
	if err != nil {
		t.Fatalf("Events unwatch: %v", err)
	}
	if browse.progs[591].Done {
		t.Error("unwatch must clear done")
	}
}

func TestListForYouRoutesToBrowseStore(t *testing.T) {
	t.Parallel()

	browse := &fakeBrowse{forYou: []catalog.Row{
		{TMDBID: 42, Kind: catalog.KindSeries, Title: "unfinished show"},
	}}
	a := api.New(&fakeRails{}, &fakeDeliveries{}, &fakeCatalog{}, &fakeSearch{},
		api.RateLimits{PerHour: 30, MaxConcurrent: 2, SearchPerHour: 60}).WithBrowse(browse)

	res, err := a.List(context.Background(), api.Principal{UserID: 7}, &sushiv1.ListReq{
		Scope: sushiv1.ListScope_LIST_SCOPE_FOR_YOU,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(res.GetRows()) != 1 || res.GetRows()[0].GetTmdbId() != 42 {
		t.Fatalf("rows = %+v, want the one for-you row", res.GetRows())
	}
}

func TestListForYouPropagatesStoreError(t *testing.T) {
	t.Parallel()

	browse := &fakeBrowse{forYouErr: errors.New("db down")}
	a := api.New(&fakeRails{}, &fakeDeliveries{}, &fakeCatalog{}, &fakeSearch{},
		api.RateLimits{PerHour: 30, MaxConcurrent: 2, SearchPerHour: 60}).WithBrowse(browse)

	if _, err := a.List(context.Background(), api.Principal{UserID: 7}, &sushiv1.ListReq{
		Scope: sushiv1.ListScope_LIST_SCOPE_FOR_YOU,
	}); err == nil {
		t.Fatal("List: want error when the store fails")
	}
}

func TestListBoxsetsReturnsMetaCardsNotMemberRows(t *testing.T) {
	t.Parallel()

	browse := &fakeBrowse{boxsetMetas: []catalog.BoxsetMeta{
		{CollectionID: 9, Name: "The Matrix Collection", Poster: "matrix.jpg", ItemCount: 4},
	}}
	a := api.New(&fakeRails{}, &fakeDeliveries{}, &fakeCatalog{}, &fakeSearch{},
		api.RateLimits{PerHour: 30, MaxConcurrent: 2, SearchPerHour: 60}).WithBrowse(browse)

	res, err := a.List(context.Background(), api.Principal{UserID: 7}, &sushiv1.ListReq{
		Scope: sushiv1.ListScope_LIST_SCOPE_BOXSETS,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(res.GetRows()) != 0 {
		t.Fatalf("rows = %+v, want none: BOXSETS answers with boxset cards, not member rows", res.GetRows())
	}
	if len(res.GetBoxsets()) != 1 {
		t.Fatalf("boxsets = %+v, want the one collection card", res.GetBoxsets())
	}
	got := res.GetBoxsets()[0]
	if got.GetCollectionId() != 9 || got.GetName() != "The Matrix Collection" || got.GetItemCount() != 4 {
		t.Fatalf("boxset card = %+v, want the collection's own identity", got)
	}
}

func TestListBoxsetsPropagatesStoreError(t *testing.T) {
	t.Parallel()

	browse := &fakeBrowse{boxsetMetasErr: errors.New("db down")}
	a := api.New(&fakeRails{}, &fakeDeliveries{}, &fakeCatalog{}, &fakeSearch{},
		api.RateLimits{PerHour: 30, MaxConcurrent: 2, SearchPerHour: 60}).WithBrowse(browse)

	if _, err := a.List(context.Background(), api.Principal{UserID: 7}, &sushiv1.ListReq{
		Scope: sushiv1.ListScope_LIST_SCOPE_BOXSETS,
	}); err == nil {
		t.Fatal("List: want error when the store fails")
	}
}

func TestListBoxsetItemsRoutesToBrowseStore(t *testing.T) {
	t.Parallel()

	browse := &fakeBrowse{boxsetItems: []catalog.Row{
		{TMDBID: 603, Kind: catalog.KindMovie, Title: "The Matrix"},
	}}
	a := api.New(&fakeRails{}, &fakeDeliveries{}, &fakeCatalog{}, &fakeSearch{},
		api.RateLimits{PerHour: 30, MaxConcurrent: 2, SearchPerHour: 60}).WithBrowse(browse)

	res, err := a.List(context.Background(), api.Principal{UserID: 7}, &sushiv1.ListReq{
		Scope:        sushiv1.ListScope_LIST_SCOPE_BOXSET,
		CollectionId: 9,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(res.GetRows()) != 1 || res.GetRows()[0].GetTmdbId() != 603 {
		t.Fatalf("rows = %+v, want the one collection member", res.GetRows())
	}
}

func TestListBoxsetItemsRequiresCollectionId(t *testing.T) {
	t.Parallel()

	browse := &fakeBrowse{}
	a := api.New(&fakeRails{}, &fakeDeliveries{}, &fakeCatalog{}, &fakeSearch{},
		api.RateLimits{PerHour: 30, MaxConcurrent: 2, SearchPerHour: 60}).WithBrowse(browse)

	if _, err := a.List(context.Background(), api.Principal{UserID: 7}, &sushiv1.ListReq{
		Scope: sushiv1.ListScope_LIST_SCOPE_BOXSET,
	}); err == nil {
		t.Fatal("List: want error when collection_id is missing")
	}
}

func TestListCatalogUnspecifiedSortUsesYearDesc(t *testing.T) {
	t.Parallel()

	cases := []struct {
		scope sushiv1.ListScope
		kind  catalog.Kind
	}{
		{sushiv1.ListScope_LIST_SCOPE_MOVIES, catalog.KindMovie},
		{sushiv1.ListScope_LIST_SCOPE_SERIES, catalog.KindSeries},
	}
	for _, tc := range cases {
		t.Run(tc.scope.String(), func(t *testing.T) {
			t.Parallel()
			browse := &fakeBrowse{}
			a := api.New(&fakeRails{}, &fakeDeliveries{}, &fakeCatalog{}, &fakeSearch{},
				api.RateLimits{PerHour: 30, MaxConcurrent: 2, SearchPerHour: 60}).WithBrowse(browse)
			if _, err := a.List(context.Background(), api.Principal{UserID: 7}, &sushiv1.ListReq{
				Scope: tc.scope,
			}); err != nil {
				t.Fatalf("List: %v", err)
			}
			if browse.lastListKind != tc.kind {
				t.Errorf("kind = %d, want %d", browse.lastListKind, tc.kind)
			}
			if browse.lastListSort != int(sushiv1.ListSort_LIST_SORT_YEAR) || !browse.lastListSortDesc {
				t.Errorf("sort = %d desc=%v, want YEAR desc", browse.lastListSort, browse.lastListSortDesc)
			}
		})
	}
}

func TestListMoviesExplicitNameKeepsName(t *testing.T) {
	t.Parallel()

	browse := &fakeBrowse{}
	a := api.New(&fakeRails{}, &fakeDeliveries{}, &fakeCatalog{}, &fakeSearch{},
		api.RateLimits{PerHour: 30, MaxConcurrent: 2, SearchPerHour: 60}).WithBrowse(browse)
	if _, err := a.List(context.Background(), api.Principal{UserID: 7}, &sushiv1.ListReq{
		Scope: sushiv1.ListScope_LIST_SCOPE_MOVIES,
		Sort:  sushiv1.ListSort_LIST_SORT_NAME,
	}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if browse.lastListSort != int(sushiv1.ListSort_LIST_SORT_NAME) || browse.lastListSortDesc {
		t.Errorf("sort = %d desc=%v, want NAME asc", browse.lastListSort, browse.lastListSortDesc)
	}
}

func TestListFavoritesUnspecifiedDoesNotCallCatalogYearSort(t *testing.T) {
	t.Parallel()

	browse := &fakeBrowse{}
	a := api.New(&fakeRails{}, &fakeDeliveries{}, &fakeCatalog{}, &fakeSearch{},
		api.RateLimits{PerHour: 30, MaxConcurrent: 2, SearchPerHour: 60}).WithBrowse(browse)
	if _, err := a.List(context.Background(), api.Principal{UserID: 7}, &sushiv1.ListReq{
		Scope: sushiv1.ListScope_LIST_SCOPE_FAVORITES,
	}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if browse.lastListSort != 0 {
		t.Errorf("ListCatalog sort = %d, want unused", browse.lastListSort)
	}
}
