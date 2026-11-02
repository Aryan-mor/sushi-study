package main

import (
	"strings"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

func TestParseNewsDraftAcceptsIDAndURL(t *testing.T) {
	t.Parallel()

	movie, ok := parseNewsDraft("550")
	if !ok || movie.tmdb != 550 || movie.kind != catalog.KindUnknown {
		t.Fatalf("bare id = %+v ok=%v", movie, ok)
	}
	linked, ok := parseNewsDraft("https://www.themoviedb.org/movie/550-fight-club")
	if !ok || linked.tmdb != 550 || linked.kind != catalog.KindMovie || linked.season != 0 {
		t.Fatalf("movie url = %+v ok=%v", linked, ok)
	}
	ep, ok := parseNewsDraft("https://www.themoviedb.org/tv/1396/season/5/episode/14")
	if !ok || ep.tmdb != 1396 || ep.kind != catalog.KindSeries || ep.season != 5 || ep.episode != 14 {
		t.Fatalf("episode url = %+v ok=%v", ep, ok)
	}
	if _, ok := parseNewsDraft("tt0137523"); ok {
		t.Fatal("imdb id must not draft a news post")
	}
	if _, ok := parseNewsDraft("https://www.imdb.com/title/tt0137523/"); ok {
		t.Fatal("imdb url must not draft a news post")
	}
}

func TestParseNewsKindCallbackRoundTrip(t *testing.T) {
	t.Parallel()

	raw := newsKindData(catalog.KindSeries, 1396, 5, 14)
	if len(raw) > 64 {
		t.Fatalf("callback %q is %d bytes", raw, len(raw))
	}
	got, ok := parseNewsKindCallback(raw)
	if !ok || got.tmdb != 1396 || got.kind != catalog.KindSeries || got.season != 5 || got.episode != 14 {
		t.Fatalf("callback %q = %+v ok=%v", raw, got, ok)
	}
}

func TestRepostWarningSitsAtTheEnd(t *testing.T) {
	t.Parallel()

	if got := appendRepostWarning("post", "fa", false, true); got != "post" {
		t.Fatalf("no repost = %q", got)
	}
	got := appendRepostWarning("post", "fa", true, true)
	if !strings.HasSuffix(got, "<p>"+newsDraftPosted.pick("fa")+"</p>") {
		t.Fatalf("rich warning = %q", got)
	}
	ready := newsDraftReadyText("en", true)
	if !strings.HasSuffix(ready, newsDraftPosted.pick("en")) {
		t.Fatalf("ready text = %q", ready)
	}
}

func TestLiveSeasonEpisodeZeroForAMovie(t *testing.T) {
	t.Parallel()

	item := catalog.Item{Row: catalog.Row{Kind: catalog.KindMovie, TMDBID: 550}}
	sn, ep := liveSeasonEpisode(item, 1, 2)
	if sn != 0 || ep != 0 {
		t.Fatalf("movie season/episode = %d/%d, want 0/0", sn, ep)
	}
	show := catalog.Item{Row: catalog.Row{Kind: catalog.KindSeries, TMDBID: 1396}}
	sn, ep = liveSeasonEpisode(show, 5, 14)
	if sn != 5 || ep != 14 {
		t.Fatalf("series season/episode = %d/%d", sn, ep)
	}
}
