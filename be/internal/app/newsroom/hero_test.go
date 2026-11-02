package newsroom

import (
	"bytes"
	"image"
	"image/jpeg"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

func fakePosterJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	poster := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			poster.Set(x, y, image.White)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, poster, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestFormatHeroJPEG(t *testing.T) {
	t.Parallel()

	out, err := FormatHeroJPEG(fakePosterJPEG(t, 400, 600), nil, HeroInput{
		TitleEN:        "Silo",
		VoteLabel:      "8.1/10",
		GenresEN:       "Drama, Sci-Fi",
		YearRange:      "2023",
		RuntimeEN:      "49 min",
		OfficialRating: "TV-MA",
	})
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != HeroWidth || img.Bounds().Dy() != HeroHeight {
		t.Fatalf("size %dx%d", img.Bounds().Dx(), img.Bounds().Dy())
	}
	if len(out) > HeroJPEGMaxBytes {
		t.Fatalf("jpeg too large: %d bytes", len(out))
	}
}

func TestFormatHeroJPEGEpisodeSeasonLine(t *testing.T) {
	t.Parallel()

	out, err := FormatHeroJPEG(fakePosterJPEG(t, 300, 450), nil, HeroInput{
		TitleEN:    "Silo",
		SeasonNum:  2,
		EpisodeNum: 4,
		VoteLabel:  "8.1/10",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 128 {
		t.Fatal("empty jpeg")
	}
	if got := heroEpisodeLine(HeroInput{SeasonNum: 2, EpisodeNum: 4}); got != "Season 2 · Episode 4" {
		t.Fatalf("episode line = %q", got)
	}
	if got := heroEpisodeLine(HeroInput{SeasonNum: 1, EpisodeNum: 8}); got != "Season 1 · Episode 8" {
		t.Fatalf("episode line = %q", got)
	}
}

func TestHeroInputFromAnnouncement(t *testing.T) {
	t.Parallel()

	got := HeroInputFrom(Announcement{
		Title:    "Silo",
		Genres:   "Drama, Sci-Fi, Thriller, Horror, Extra",
		Year:     2023,
		Rating:   81,
		RuntimeS: 49 * 60,
		Kind:     catalog.KindSeries,
		Season:   2,
		Episode:  4,
	})
	if got.TitleEN != "Silo" || got.VoteLabel != "8.1/10" || got.YearRange != "2023" || got.RuntimeEN != "49 min" {
		t.Fatalf("meta %+v", got)
	}
	if got.GenresEN != "Drama, Sci-Fi, Thriller, Horror" {
		t.Fatalf("genres %q", got.GenresEN)
	}
	if got.SeasonNum != 2 || got.EpisodeNum != 4 {
		t.Fatalf("episode %d/%d", got.SeasonNum, got.EpisodeNum)
	}
}
