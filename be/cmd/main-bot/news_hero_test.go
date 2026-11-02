package main

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"strings"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/app/newsroom"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

func TestRichWithHeroFallsBackToTMDBSlideshow(t *testing.T) {
	t.Parallel()

	a := newsroom.Announcement{
		TMDBID:   603,
		Kind:     catalog.KindMovie,
		Title:    "The Matrix",
		Poster:   "/m.jpg",
		Backdrop: "/b.jpg",
	}
	html, jpeg := richWithHero(a, nil)
	if len(jpeg) != 0 {
		t.Fatal("empty compose must not attach bytes")
	}
	if strings.Contains(html, "tg://photo") {
		t.Fatal("compose failure must keep the TMDB slideshow")
	}
	if !strings.Contains(html, `src="https://image.tmdb.org/t/p/w500/m.jpg"`) {
		t.Fatalf("missing poster:\n%s", html)
	}
}

func TestRichWithHeroUsesTelegramPhoto(t *testing.T) {
	t.Parallel()

	a := newsroom.Announcement{
		TMDBID:   603,
		Kind:     catalog.KindMovie,
		Title:    "The Matrix",
		Poster:   "/m.jpg",
		Backdrop: "/b.jpg",
	}
	html, jpeg := richWithHero(a, []byte("JPEG"))
	if string(jpeg) != "JPEG" {
		t.Fatalf("attach %q", jpeg)
	}
	if !strings.Contains(html, `src="tg://photo?id=hero"`) {
		t.Fatalf("missing hero:\n%s", html)
	}
	if strings.Contains(html, `/w500/m.jpg`) {
		t.Fatal("hero post must drop the TMDB poster slide")
	}
}

func TestComposeNewsHeroFetch(t *testing.T) {
	t.Parallel()

	poster := image.NewRGBA(image.Rect(0, 0, 200, 300))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, poster, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	get := func(_ context.Context, url string) ([]byte, error) {
		if !strings.Contains(url, "image.tmdb.org") {
			t.Errorf("unexpected url %s", url)
		}
		return buf.Bytes(), nil
	}
	out, err := composeNewsHeroFetch(context.Background(), get, newsroom.Announcement{
		Title:  "Silo",
		Poster: "/m.jpg",
		Year:   2023,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 128 {
		t.Fatal("empty jpeg")
	}
}

func TestComposeNewsHeroFetchFailsOpen(t *testing.T) {
	t.Parallel()

	get := func(context.Context, string) ([]byte, error) {
		return nil, errors.New("tmdb down")
	}
	_, err := composeNewsHeroFetch(context.Background(), get, newsroom.Announcement{Poster: "/m.jpg"})
	if err == nil {
		t.Fatal("want fetch error")
	}
}
