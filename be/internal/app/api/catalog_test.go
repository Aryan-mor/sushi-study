package api_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
)

type itemKey struct {
	tmdbID uint64
	kind   catalog.Kind
}

// fakeCatalog is the in-process store Item and Files run against, mirroring the real schema's
// shape closely enough to exercise it: an item that is not in the map behaves exactly like a
// query that found no row (R-API-3 -- testable without a database).
type fakeCatalog struct {
	items    map[itemKey]catalog.Item
	files    map[int64][]catalog.File
	itemErr  error
	filesErr error
}

func (f *fakeCatalog) Item(_ context.Context, tmdbID uint64, kind catalog.Kind) (catalog.Item, bool, error) {
	if f.itemErr != nil {
		return catalog.Item{}, false, f.itemErr
	}
	item, ok := f.items[itemKey{tmdbID, kind}]
	return item, ok, nil
}

func (f *fakeCatalog) Files(_ context.Context, episodeID int64) ([]catalog.File, error) {
	if f.filesErr != nil {
		return nil, f.filesErr
	}
	return f.files[episodeID], nil
}

func (f *fakeCatalog) EpisodesForSeason(_ context.Context, tmdbID uint64, kind catalog.Kind, seasonNo, limit, offset int32) ([]catalog.Episode, int32, error) {
	item, ok := f.items[itemKey{tmdbID, kind}]
	if !ok {
		return nil, 0, nil
	}
	var matching []catalog.Episode
	for _, e := range item.Episodes {
		if e.SeasonNo == seasonNo {
			matching = append(matching, e)
		}
	}
	total := int32(len(matching))
	if limit <= 0 {
		return matching, total, nil
	}
	if offset < 0 {
		offset = 0
	}
	if int(offset) >= len(matching) {
		return nil, total, nil
	}
	end := int(offset + limit)
	if end > len(matching) {
		end = len(matching)
	}
	return matching[offset:end], total, nil
}

func (f *fakeCatalog) FirstPlayableEpisode(_ context.Context, tmdbID uint64, kind catalog.Kind) (catalog.Episode, bool, error) {
	item, ok := f.items[itemKey{tmdbID, kind}]
	if !ok {
		return catalog.Episode{}, false, nil
	}
	for _, e := range item.Episodes {
		if e.SeasonNo <= 0 {
			continue
		}
		for _, file := range f.files[e.ID] {
			if file.State == catalog.FileReady {
				return e, true, nil
			}
		}
	}
	return catalog.Episode{}, false, nil
}

func catalogAPI(c *fakeCatalog) *api.API {
	return api.New(&fakeRails{}, &fakeDeliveries{}, c, &fakeSearch{}, api.RateLimits{PerHour: 30, MaxConcurrent: 2, SearchPerHour: 60})
}

func TestItemReturnsTheFullRecord(t *testing.T) {
	t.Parallel()

	released := time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC)
	store := &fakeCatalog{items: map[itemKey]catalog.Item{
		{603, catalog.KindMovie}: {
			Row:        catalog.Row{TMDBID: 603, Kind: catalog.KindMovie, Title: "The Matrix", Year: 1999, Rating: 83, Poster: "abc123"},
			Overview:   "A hacker discovers reality is a simulation.",
			ReleasedOn: released,
			Episodes:   []catalog.Episode{{ID: 900, SeasonNo: 0, EpisodeNo: 0, Title: ""}},
			Detail: catalog.Detail{
				Logo: "wordmark", Genres: "Action", RuntimeS: 136,
				People:         []catalog.Person{{Name: "Keanu", Role: "Neo"}},
				CollectionName: "The Matrix Collection",
			},
			Related:    []catalog.Row{{TMDBID: 604, Kind: catalog.KindMovie, Title: "Reloaded"}},
			Collection: []catalog.Row{{TMDBID: 605, Kind: catalog.KindMovie, Title: "Revolutions"}},
		},
	}}

	res, err := catalogAPI(store).Item(context.Background(), api.Principal{}, &sushiv1.ItemReq{
		TmdbId: 603, Kind: sushiv1.Kind_KIND_MOVIE,
	})
	if err != nil {
		t.Fatalf("Item: %v", err)
	}
	if res.GetRow().GetTitle() != "The Matrix" || res.GetRow().GetTmdbId() != 603 {
		t.Errorf("row = %+v, want The Matrix/603", res.GetRow())
	}
	if res.GetOverview() == "" {
		t.Error("overview is empty")
	}
	if res.GetReleasedOn() != released.Unix() {
		t.Errorf("releasedOn = %d, want %d", res.GetReleasedOn(), released.Unix())
	}
	if res.GetLogo() != "wordmark" || res.GetGenres() != "Action" || res.GetRuntimeS() != 136 {
		t.Errorf("detail extras logo=%q genres=%q runtime=%d", res.GetLogo(), res.GetGenres(), res.GetRuntimeS())
	}
	if len(res.GetPeople()) != 1 || res.GetPeople()[0].GetName() != "Keanu" {
		t.Errorf("people = %+v", res.GetPeople())
	}
	if len(res.GetRelated()) != 1 || res.GetRelated()[0].GetTmdbId() != 604 {
		t.Errorf("related = %+v", res.GetRelated())
	}
	if res.GetCollectionName() != "The Matrix Collection" || len(res.GetCollection()) != 1 {
		t.Errorf("collection name=%q items=%+v", res.GetCollectionName(), res.GetCollection())
	}
	if len(res.GetEpisodes()) != 1 || res.GetEpisodes()[0].GetEpisodeId() != 900 {
		t.Errorf("episodes = %+v, want exactly one entry with id 900", res.GetEpisodes())
	}
}

// A movie has no calendar season/episode structure, but the client still needs an episode id to
// ask Files with -- the single synthetic season-0/episode-0 entry is what supplies it (docs/06 §2).
func TestItemAlwaysReturnsAtLeastOneEpisode(t *testing.T) {
	t.Parallel()

	store := &fakeCatalog{items: map[itemKey]catalog.Item{
		{603, catalog.KindMovie}: {
			Row:      catalog.Row{TMDBID: 603, Kind: catalog.KindMovie, Title: "The Matrix"},
			Episodes: []catalog.Episode{{ID: 900, SeasonNo: 0, EpisodeNo: 0}},
		},
	}}

	res, err := catalogAPI(store).Item(context.Background(), api.Principal{}, &sushiv1.ItemReq{
		TmdbId: 603, Kind: sushiv1.Kind_KIND_MOVIE,
	})
	if err != nil {
		t.Fatalf("Item: %v", err)
	}
	if len(res.GetEpisodes()) == 0 {
		t.Fatal("a movie must still return one episode entry -- the client has nothing to ask Files with otherwise")
	}
}

func TestItemReportsNotFound(t *testing.T) {
	t.Parallel()

	_, err := catalogAPI(&fakeCatalog{}).Item(context.Background(), api.Principal{}, &sushiv1.ItemReq{
		TmdbId: 999999, Kind: sushiv1.Kind_KIND_MOVIE,
	})
	if !errors.Is(err, api.ErrNotFound) {
		t.Errorf("err = %v, want it to wrap api.ErrNotFound (maps to ERR_CODE_NOT_FOUND)", err)
	}
}

func TestItemRejectsAnUnspecifiedKind(t *testing.T) {
	t.Parallel()

	_, err := catalogAPI(&fakeCatalog{}).Item(context.Background(), api.Principal{}, &sushiv1.ItemReq{TmdbId: 603})
	if err == nil {
		t.Fatal("want an error; defaulting the kind would hide a client bug behind an answer that looks right")
	}
}

func TestItemSurfacesStoreFailures(t *testing.T) {
	t.Parallel()

	want := errors.New("database is down")
	_, err := catalogAPI(&fakeCatalog{itemErr: want}).Item(context.Background(), api.Principal{}, &sushiv1.ItemReq{
		TmdbId: 603, Kind: sushiv1.Kind_KIND_MOVIE,
	})
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want it to wrap %v", err, want)
	}
}

func TestFilesReturnsThePickList(t *testing.T) {
	t.Parallel()

	store := &fakeCatalog{files: map[int64][]catalog.File{
		900: {
			{ID: 3, QualityLabel: "1080p", Height: 1080, AudioLangs: "en,fa", SizeBytes: 2_000_000_000, DurationS: 7200, State: catalog.FileReady},
			{ID: 4, QualityLabel: "720p", Height: 720, State: catalog.FilePending},
		},
	}}

	res, err := catalogAPI(store).Files(context.Background(), api.Principal{}, &sushiv1.FilesReq{EpisodeId: 900})
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if len(res.GetFiles()) != 2 {
		t.Fatalf("got %d files, want 2", len(res.GetFiles()))
	}
	if res.GetFiles()[0].GetState() != sushiv1.FileState_FILE_STATE_READY {
		t.Errorf("file 0 state = %v, want READY", res.GetFiles()[0].GetState())
	}
	if res.GetFiles()[1].GetState() != sushiv1.FileState_FILE_STATE_PENDING {
		t.Errorf("file 1 state = %v, want PENDING", res.GetFiles()[1].GetState())
	}
}

// An episode with no files yet (or an id that does not exist) is not an error -- there is no
// meaningful difference between "bad id" and "nothing to offer right now", and the client just
// renders an empty pick-list either way.
func TestFilesReturnsEmptyForAnUnknownOrEmptyEpisode(t *testing.T) {
	t.Parallel()

	res, err := catalogAPI(&fakeCatalog{}).Files(context.Background(), api.Principal{}, &sushiv1.FilesReq{EpisodeId: 12345})
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if len(res.GetFiles()) != 0 {
		t.Errorf("got %d files, want 0", len(res.GetFiles()))
	}
}

func TestFilesCarriesEpisodeProgress(t *testing.T) {
	t.Parallel()

	store := &fakeCatalog{files: map[int64][]catalog.File{
		900: {{ID: 3, QualityLabel: "1080p", Height: 1080, State: catalog.FileReady, DurationS: 7200}},
	}}
	browse := &fakeBrowse{progs: map[int64]catalog.EpisodeProgress{
		900: {PositionS: 1200, Done: false, LastFileID: 3},
	}}
	a := catalogAPI(store).WithBrowse(browse)

	res, err := a.Files(context.Background(), api.Principal{UserID: 7}, &sushiv1.FilesReq{EpisodeId: 900})
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if res.GetPositionS() != 1200 || res.GetDone() || res.GetLastFileId() != 3 {
		t.Errorf("progress position=%d done=%v last=%d", res.GetPositionS(), res.GetDone(), res.GetLastFileId())
	}

	unbound, err := a.Files(context.Background(), api.Principal{}, &sushiv1.FilesReq{EpisodeId: 900})
	if err != nil {
		t.Fatalf("Files unbound: %v", err)
	}
	if unbound.GetPositionS() != 0 || unbound.GetLastFileId() != 0 {
		t.Error("unbound caller must not receive someone else's progress")
	}
}

func TestItemSeriesSendsSeasonIndexAndOnePlayTarget(t *testing.T) {
	t.Parallel()

	store := &fakeCatalog{items: map[itemKey]catalog.Item{
		{37854, catalog.KindSeries}: {
			Row: catalog.Row{TMDBID: 37854, Kind: catalog.KindSeries, Title: "One Piece"},
			Episodes: []catalog.Episode{
				{ID: 1, SeasonNo: 1, EpisodeNo: 1, Title: "Romance Dawn"},
				{ID: 2, SeasonNo: 1, EpisodeNo: 2, Title: "They Call Him Straw Hat Luffy"},
				{ID: 3, SeasonNo: 2, EpisodeNo: 1, Title: "The Coming! Corkscrew Jango"},
			},
		},
	}}

	res, err := catalogAPI(store).Item(context.Background(), api.Principal{}, &sushiv1.ItemReq{
		TmdbId: 37854, Kind: sushiv1.Kind_KIND_SERIES,
	})
	if err != nil {
		t.Fatalf("Item: %v", err)
	}
	if len(res.GetEpisodes()) != 1 || res.GetEpisodes()[0].GetEpisodeId() != 1 {
		t.Errorf("play-target = %+v, want one episode id 1", res.GetEpisodes())
	}
	if len(res.GetSeasons()) != 2 {
		t.Fatalf("seasons = %d, want 2", len(res.GetSeasons()))
	}
	if res.GetSeasons()[0].GetSeasonNo() != 1 || res.GetSeasons()[0].GetEpisodeCount() != 2 {
		t.Errorf("season 0 = %+v, want S1 count 2", res.GetSeasons()[0])
	}
	if res.GetSeasons()[1].GetSeasonNo() != 2 || res.GetSeasons()[1].GetEpisodeCount() != 1 {
		t.Errorf("season 1 = %+v, want S2 count 1", res.GetSeasons()[1])
	}
}

func TestItemPlayTargetIsFirstEpisodeWithReadyFile(t *testing.T) {
	t.Parallel()

	store := &fakeCatalog{
		items: map[itemKey]catalog.Item{
			{37854, catalog.KindSeries}: {
				Row: catalog.Row{TMDBID: 37854, Kind: catalog.KindSeries, Title: "One Piece"},
				Episodes: []catalog.Episode{
					{ID: 1, SeasonNo: 1, EpisodeNo: 1, Title: "Romance Dawn"},
					{ID: 50, SeasonNo: 1, EpisodeNo: 50, Title: "Ready"},
				},
			},
		},
		files: map[int64][]catalog.File{
			50: {{ID: 9, QualityLabel: "1080p", Height: 1080, State: catalog.FileReady}},
		},
	}

	res, err := catalogAPI(store).Item(context.Background(), api.Principal{}, &sushiv1.ItemReq{
		TmdbId: 37854, Kind: sushiv1.Kind_KIND_SERIES,
	})
	if err != nil {
		t.Fatalf("Item: %v", err)
	}
	if len(res.GetEpisodes()) != 1 || res.GetEpisodes()[0].GetEpisodeId() != 50 {
		t.Errorf("play-target = %+v, want episode 50 (first with a ready file)", res.GetEpisodes())
	}
}

func TestItemPlayTargetSkipsSpecials(t *testing.T) {
	t.Parallel()

	store := &fakeCatalog{items: map[itemKey]catalog.Item{
		{1, catalog.KindSeries}: {
			Row: catalog.Row{TMDBID: 1, Kind: catalog.KindSeries, Title: "Show"},
			Episodes: []catalog.Episode{
				{ID: 9, SeasonNo: 0, EpisodeNo: 1, Title: "Special"},
				{ID: 10, SeasonNo: 1, EpisodeNo: 1, Title: "Pilot"},
			},
		},
	}}

	res, err := catalogAPI(store).Item(context.Background(), api.Principal{}, &sushiv1.ItemReq{
		TmdbId: 1, Kind: sushiv1.Kind_KIND_SERIES,
	})
	if err != nil {
		t.Fatalf("Item: %v", err)
	}
	if len(res.GetEpisodes()) != 1 || res.GetEpisodes()[0].GetEpisodeId() != 10 {
		t.Errorf("play-target = %+v, want S1E1 id 10", res.GetEpisodes())
	}
}

func TestEpisodesReturnsOneSeasonPage(t *testing.T) {
	t.Parallel()

	store := &fakeCatalog{items: map[itemKey]catalog.Item{
		{1396, catalog.KindSeries}: {
			Row: catalog.Row{TMDBID: 1396, Kind: catalog.KindSeries, Title: "Breaking Bad"},
			Episodes: []catalog.Episode{
				{ID: 10, SeasonNo: 1, EpisodeNo: 1, Title: "Pilot"},
				{ID: 11, SeasonNo: 1, EpisodeNo: 2, Title: "Cat's in the Bag"},
				{ID: 12, SeasonNo: 2, EpisodeNo: 1, Title: "Seven Thirty-Seven"},
			},
		},
	}}

	res, err := catalogAPI(store).Episodes(context.Background(), api.Principal{}, &sushiv1.EpisodesReq{
		TmdbId: 1396, Kind: sushiv1.Kind_KIND_SERIES, SeasonNo: 1,
	})
	if err != nil {
		t.Fatalf("Episodes: %v", err)
	}
	if res.GetPage() != 0 || res.GetPages() != 1 {
		t.Errorf("page=%d pages=%d, want 0/1", res.GetPage(), res.GetPages())
	}
	if len(res.GetEpisodes()) != 2 || res.GetEpisodes()[0].GetTitle() != "Pilot" {
		t.Errorf("episodes = %+v, want S1's two", res.GetEpisodes())
	}
}

func TestEpisodesEmptyForUnknownTitle(t *testing.T) {
	t.Parallel()

	res, err := catalogAPI(&fakeCatalog{}).Episodes(context.Background(), api.Principal{}, &sushiv1.EpisodesReq{
		TmdbId: 1, Kind: sushiv1.Kind_KIND_SERIES, SeasonNo: 1,
	})
	if err != nil {
		t.Fatalf("Episodes: %v", err)
	}
	if len(res.GetEpisodes()) != 0 || res.GetPages() != 0 {
		t.Errorf("got %d episodes pages=%d, want empty", len(res.GetEpisodes()), res.GetPages())
	}
}

func TestFilesSurfacesStoreFailures(t *testing.T) {
	t.Parallel()

	want := errors.New("database is down")
	_, err := catalogAPI(&fakeCatalog{filesErr: want}).Files(context.Background(), api.Principal{}, &sushiv1.FilesReq{EpisodeId: 900})
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want it to wrap %v", err, want)
	}
}
