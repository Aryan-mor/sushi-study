package tmdb

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

func TestParseFindPrefersMovieThenTV(t *testing.T) {
	t.Parallel()

	movie := parseFind([]byte(`{
		"movie_results":[{"id":550,"title":"Fight Club","release_date":"1999-10-15","poster_path":"/fc.jpg","vote_average":8.4}],
		"tv_results":[{"id":1396,"name":"Breaking Bad","first_air_date":"2008-01-20"}]
	}`), "tt0137523")
	if movie.TMDBID != 550 || movie.Kind != catalog.KindMovie || movie.Title != "Fight Club" || movie.Year != 1999 {
		t.Fatalf("movie hit %+v", movie.Row)
	}
	if movie.IMDBID != "tt0137523" {
		t.Fatalf("IMDBID = %q", movie.IMDBID)
	}

	tv := parseFind([]byte(`{
		"movie_results":[],
		"tv_results":[{"id":1396,"name":"Breaking Bad","first_air_date":"2008-01-20","poster_path":"/bb.jpg"}]
	}`), "tt0903747")
	if tv.TMDBID != 1396 || tv.Kind != catalog.KindSeries || tv.Title != "Breaking Bad" || tv.Year != 2008 {
		t.Fatalf("tv hit %+v", tv.Row)
	}

	none := parseFind([]byte(`{"movie_results":[],"tv_results":[],"person_results":[{"id":1}]}`), "tt0000001")
	if none.TMDBID != 0 {
		t.Fatalf("person-only find must be empty, got %+v", none)
	}
}

func TestFindByIMDBHitsFindEndpoint(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/find/tt0137523" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("external_source"); got != "imdb_id" {
			t.Errorf("external_source = %q", got)
		}
		if r.URL.Query().Get("api_key") == "" {
			t.Error("missing api_key")
		}
		_, _ = io.WriteString(w, `{"movie_results":[{"id":550,"title":"Fight Club","release_date":"1999-10-15"}]}`)
	}))
	t.Cleanup(srv.Close)

	c := New("k")
	c.apiRoot = srv.URL
	item, err := c.FindByIMDB(context.Background(), "TT0137523")
	if err != nil {
		t.Fatalf("FindByIMDB: %v", err)
	}
	if item.TMDBID != 550 || item.IMDBID != "tt0137523" {
		t.Fatalf("item %+v", item)
	}
}

func TestFindByIMDBEmptyIDOrKey(t *testing.T) {
	t.Parallel()
	c := New("")
	item, err := c.FindByIMDB(context.Background(), "tt0137523")
	if err != nil || item.TMDBID != 0 {
		t.Fatalf("empty key: %+v %v", item, err)
	}
	c = New("k")
	item, err = c.FindByIMDB(context.Background(), "not-an-id")
	if err != nil || item.TMDBID != 0 {
		t.Fatalf("bad id: %+v %v", item, err)
	}
}

func TestCanonicalIMDB(t *testing.T) {
	t.Parallel()
	if got := canonicalIMDB(" TT0137523 "); got != "tt0137523" {
		t.Fatalf("got %q", got)
	}
	if got := canonicalIMDB("tt12"); got != "" {
		t.Fatalf("too short: %q", got)
	}
	if !strings.HasPrefix(canonicalIMDB("tt0903747"), "tt") {
		t.Fatal("series id")
	}
}
