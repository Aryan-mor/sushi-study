package main

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

func TestParseIDQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in     string
		imdb   string
		tmdb   uint64
		forced catalog.Kind
		ok     bool
	}{
		{in: "tt0137523", imdb: "tt0137523", ok: true},
		{in: "TT0137523", imdb: "tt0137523", ok: true},
		{in: "https://www.imdb.com/title/tt0137523/?ref_=fn", imdb: "tt0137523", ok: true},
		{in: "550", tmdb: 550, ok: true},
		{in: "https://www.themoviedb.org/movie/550-fight-club", tmdb: 550, forced: catalog.KindMovie, ok: true},
		{in: "https://www.themoviedb.org/tv/1396", tmdb: 1396, forced: catalog.KindSeries, ok: true},
		{in: "tmdb: 603", tmdb: 603, ok: true},
		{in: "themoviedb 4607", tmdb: 4607, ok: true},
		{in: "", ok: false},
		{in: "hello", ok: false},
		{in: "tt12", ok: false},
	}
	for _, tc := range tests {
		q, ok := parseIDQuery(tc.in)
		if ok != tc.ok {
			t.Fatalf("%q ok=%v, want %v", tc.in, ok, tc.ok)
		}
		if !ok {
			continue
		}
		if q.imdb != tc.imdb || q.tmdb != tc.tmdb || q.forced != tc.forced {
			t.Fatalf("%q → %+v, want imdb=%q tmdb=%d forced=%v", tc.in, q, tc.imdb, tc.tmdb, tc.forced)
		}
	}
}

func TestLookupIDsIMDBAndTMDB(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	api := &fakeIDs{
		pages: map[string]catalog.Item{
			"movie:550": {
				Row:    catalog.Row{TMDBID: 550, Kind: catalog.KindMovie, Title: "Fight Club", Year: 1999},
				IMDBID: "tt0137523",
			},
			"series:1396": {
				Row:    catalog.Row{TMDBID: 1396, Kind: catalog.KindSeries, Title: "Breaking Bad", Year: 2008},
				IMDBID: "tt0903747",
			},
		},
		imdb: map[string]catalog.Item{
			"tt0137523": {Row: catalog.Row{TMDBID: 550, Kind: catalog.KindMovie, Title: "Fight Club", Year: 1999}, IMDBID: "tt0137523"},
		},
	}

	got, err := lookupIDs(ctx, api, idQuery{imdb: "tt0137523"})
	if err != nil || len(got) != 1 || got[0].TMDBID != 550 {
		t.Fatalf("imdb lookup: %+v %v", got, err)
	}

	got, err = lookupIDs(ctx, api, idQuery{tmdb: 550, forced: catalog.KindMovie})
	if err != nil || len(got) != 1 || got[0].IMDBID != "tt0137523" {
		t.Fatalf("tmdb movie: %+v %v", got, err)
	}

	got, err = lookupIDs(ctx, api, idQuery{tmdb: 1396})
	if err != nil || len(got) != 1 || got[0].Kind != catalog.KindSeries {
		t.Fatalf("bare tmdb tries both, series hit: %+v %v", got, err)
	}

	got, err = lookupIDs(ctx, api, idQuery{imdb: "tt0000001"})
	if err != nil || len(got) != 0 {
		t.Fatalf("missing imdb: %+v %v", got, err)
	}
}

func TestLookupIDsBothKindsAndErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	both := &fakeIDs{pages: map[string]catalog.Item{
		"movie:99":  {Row: catalog.Row{TMDBID: 99, Title: "Film"}},
		"series:99": {Row: catalog.Row{TMDBID: 99, Title: "Show"}},
	}}
	got, err := lookupIDs(ctx, both, idQuery{tmdb: 99})
	if err != nil || len(got) != 2 {
		t.Fatalf("both kinds: %+v %v", got, err)
	}

	boom := &fakeIDs{err: errors.New("upstream")}
	if _, err := lookupIDs(ctx, boom, idQuery{imdb: "tt0137523"}); err == nil {
		t.Fatal("want upstream error")
	}
	if _, err := lookupIDs(ctx, nil, idQuery{tmdb: 1}); err == nil {
		t.Fatal("want missing client error")
	}
}

func TestFormatIDResult(t *testing.T) {
	t.Parallel()
	item := catalog.Item{
		Row:    catalog.Row{TMDBID: 550, Kind: catalog.KindMovie, Title: "Fight Club", Year: 1999},
		IMDBID: "tt0137523",
	}
	got := formatIDResult("en", []catalog.Item{item})
	for _, need := range []string{"Fight Club", "1999", "movie", "550", "tt0137523", "themoviedb.org/movie/550", "imdb.com/title/tt0137523"} {
		if !strings.Contains(got, need) {
			t.Fatalf("missing %q in %s", need, got)
		}
	}

	none := catalog.Item{Row: catalog.Row{TMDBID: 1, Kind: catalog.KindSeries, Title: "X", Year: 2024}}
	got = formatIDItem("fa", none)
	if !strings.Contains(got, idsNoIMDB.fa) || !strings.Contains(got, "سریال") {
		t.Fatalf("no-imdb fa: %s", got)
	}
}

func TestIDsResultKeyboard(t *testing.T) {
	t.Parallel()
	kb := idsResultKeyboard("en")
	if !rowsContain(kb.Rows, cbMenuIDs) || !rowsContain(kb.Rows, cbMenuAdmin) || !rowsContain(kb.Rows, cbMenuHome) {
		t.Fatal("result keyboard needs convert-again, Admin, Home")
	}
}

type fakeIDs struct {
	pages map[string]catalog.Item
	imdb  map[string]catalog.Item
	err   error
}

func (f *fakeIDs) TitlePage(_ context.Context, tmdbID uint64, kind catalog.Kind) (catalog.Item, error) {
	if f.err != nil {
		return catalog.Item{}, f.err
	}
	return f.pages[kind.String()+":"+strconv.FormatUint(tmdbID, 10)], nil
}

func (f *fakeIDs) FindByIMDB(_ context.Context, imdbID string) (catalog.Item, error) {
	if f.err != nil {
		return catalog.Item{}, f.err
	}
	return f.imdb[imdbID], nil
}
