package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

func TestSearchTitlesMergesTypedTVWhenMultiIsFull(t *testing.T) {
	t.Parallel()

	multiHits := make([]map[string]any, 0, searchPerSourceCap)
	for i := 1; i <= searchPerSourceCap; i++ {
		multiHits = append(multiHits, map[string]any{
			"id": i, "media_type": "movie", "title": fmt.Sprintf("Filler %d", i),
			"release_date": "1990-01-01",
		})
	}
	multiBody, err := json.Marshal(map[string]any{"results": multiHits})
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/search/multi"):
			_, _ = w.Write(multiBody)
		case strings.HasSuffix(r.URL.Path, "/search/movie"):
			_, _ = io.WriteString(w, `{"results":[{"id":733976,"title":"Secret Service","release_date":"1970-01-01"}]}`)
		case strings.HasSuffix(r.URL.Path, "/search/tv"):
			if r.URL.Query().Get("first_air_date_year") != "" {
				t.Errorf("no-year query must not set first_air_date_year, got %q", r.URL.Query().Get("first_air_date_year"))
			}
			_, _ = io.WriteString(w, `{"results":[{"id":287450,"name":"Secret Service","first_air_date":"2026-04-27"}]}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	c := New("k")
	c.apiRoot = srv.URL
	rows, err := c.SearchTitles(context.Background(), "Secret Service")
	if err != nil {
		t.Fatalf("SearchTitles: %v", err)
	}
	var sawTV, sawMovie bool
	for _, r := range rows {
		if r.TMDBID == 287450 && r.Kind == catalog.KindSeries {
			sawTV = true
		}
		if r.TMDBID == 733976 && r.Kind == catalog.KindMovie {
			sawMovie = true
		}
	}
	if !sawTV {
		t.Fatalf("typed search/tv series 287450 dropped; %d rows (cap filled by multi?)", len(rows))
	}
	if !sawMovie {
		t.Fatalf("typed search/movie 733976 missing")
	}
}

func TestSearchTitlesKeepsTVWhenTypedMovieFillsCap(t *testing.T) {
	t.Parallel()

	movieHits := make([]map[string]any, 0, searchPerSourceCap)
	for i := 1; i <= searchPerSourceCap; i++ {
		movieHits = append(movieHits, map[string]any{
			"id": 1000 + i, "title": fmt.Sprintf("Secret Service Filler %d", i),
			"release_date": "1970-01-01",
		})
	}
	movieBody, err := json.Marshal(map[string]any{"results": movieHits})
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/search/multi"):
			_, _ = io.WriteString(w, `{"results":[]}`)
		case strings.HasSuffix(r.URL.Path, "/search/movie"):
			_, _ = w.Write(movieBody)
		case strings.HasSuffix(r.URL.Path, "/search/tv"):
			_, _ = io.WriteString(w, `{"results":[{"id":287450,"name":"Secret Service","first_air_date":"2026-04-27"}]}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	c := New("k")
	c.apiRoot = srv.URL
	rows, err := c.SearchTitles(context.Background(), "Secret Service")
	if err != nil {
		t.Fatalf("SearchTitles: %v", err)
	}
	for _, r := range rows {
		if r.TMDBID == 287450 && r.Kind == catalog.KindSeries {
			return
		}
	}
	t.Fatalf("series 287450 dropped under %d typed movies; %d rows", searchPerSourceCap, len(rows))
}
