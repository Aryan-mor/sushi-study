package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTVHasEpisode(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tv/322499/season/1/episode/13":
			_, _ = w.Write([]byte(`{"id":1,"episode_number":13,"season_number":1}`))
		case "/tv/322499/season/0/episode/13", "/tv/322499/season/1/episode/25":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	c := New("k")
	c.apiRoot = srv.URL
	ctx := context.Background()

	ok, err := c.TVHasEpisode(ctx, 322499, 1, 13)
	if err != nil || !ok {
		t.Fatalf("S01E13: ok=%v err=%v, want true", ok, err)
	}
	ok, err = c.TVHasEpisode(ctx, 322499, 0, 13)
	if err != nil || ok {
		t.Fatalf("S00E13: ok=%v err=%v, want false", ok, err)
	}
	ok, err = c.TVHasEpisode(ctx, 322499, 1, 25)
	if err != nil || ok {
		t.Fatalf("S01E25: ok=%v err=%v, want false", ok, err)
	}
}
