package catalog_test

import (
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

func TestFormatSubtitleCaptionEpisode(t *testing.T) {
	t.Parallel()

	got := catalog.FormatSubtitleCaption(catalog.SubtitleCaption{
		Title:           "Lanterns",
		Year:            2026,
		Kind:            2,
		SeasonNo:        1,
		EpisodeNo:       5,
		Lang:            "fa",
		FileName:        "lanterns-All-S01E05.zip",
		TmdbID:          654321,
		SourceChannelID: -1009900000006,
		SourceMsgID:     42,
	})
	want := "💬 Lanterns (2026)\nS01E05\n🗣 فارسی\n📄 lanterns-All-S01E05.zip\n" +
		`🔗 <a href="https://www.themoviedb.org/tv/654321">tmdb</a> <code>654321</code> · <a href="https://t.me/c/9900000006/42">Source</a>`
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestFormatSubtitleCaptionNoTMDBOrSource(t *testing.T) {
	t.Parallel()

	got := catalog.FormatSubtitleCaption(catalog.SubtitleCaption{
		Title: "Some Movie", Kind: 1, Lang: "fa", FileName: "some-movie.srt",
	})
	want := "💬 Some Movie\n🗣 فارسی\n📄 some-movie.srt\n🔗 movie"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}
