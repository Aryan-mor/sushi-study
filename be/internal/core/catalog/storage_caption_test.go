package catalog_test

import (
	"strings"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

const movieCoords = `🔗 <a href="https://www.themoviedb.org/movie/431693">tmdb</a> <code>431693</code> · file <code>3</code> · ox <code>1</code>`

func TestFormatStorageCaptionMovie(t *testing.T) {
	t.Parallel()

	got := catalog.FormatStorageCaption(catalog.StorageCaption{
		FileID: 3,
		TmdbID: 431693,
		Title:  "Spies in Disguise",
		Year:   2019,
		Kind:   1,
		Source: "ox",
		ExtKey: "1",
	})
	want := "🎬 Spies in Disguise (2019)\n" + movieCoords
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestFormatStorageCaptionEpisode(t *testing.T) {
	t.Parallel()

	got := catalog.FormatStorageCaption(catalog.StorageCaption{
		FileID:       9,
		TmdbID:       1396,
		Title:        "Breaking Bad",
		Year:         2008,
		Kind:         2,
		SeasonNo:     2,
		EpisodeNo:    4,
		EpisodeTitle: "Down",
		Quality:      "1080p",
		Source:       "ox",
		ExtKey:       "88",
	})
	if !strings.Contains(got, "\nS02E04 · Down\n") {
		t.Fatalf("missing episode line: %q", got)
	}
	if !strings.Contains(got, "\n🎞 1080p\n") {
		t.Fatalf("missing quality line: %q", got)
	}
	wantCoords := `🔗 <a href="https://www.themoviedb.org/tv/1396">tmdb</a> <code>1396</code> · file <code>9</code> · ox <code>88</code>`
	if !strings.HasSuffix(got, "\n"+wantCoords) {
		t.Fatalf("coords line: %q", got)
	}
}

func TestFormatStorageCaptionFileAndTracks(t *testing.T) {
	t.Parallel()

	got := catalog.FormatStorageCaption(catalog.StorageCaption{
		FileID:     3,
		TmdbID:     431693,
		Title:      "Spies in Disguise",
		Year:       2019,
		Kind:       1,
		Source:     "ox",
		ExtKey:     "1",
		FileName:   "Spies.in.Disguise.2019.1080p.mkv",
		DurationS:  6123,
		AudioLangs: "en,fa",
		SubLangs:   "fa",
	})
	want := "🎬 Spies in Disguise (2019)\n" +
		"⏱ 1:42:03\n" +
		"📄 Spies.in.Disguise.2019.1080p.mkv\n" +
		"🔊 en,fa · 💬 fa\n" +
		movieCoords
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// tgforward files (indexerd) carry a source channel + message id; the coords line gets a Source
// link. fetcherd/the prototype rows never set these, so they get none (TestFormatStorageCaptionMovie).
func TestFormatStorageCaptionSourceLink(t *testing.T) {
	t.Parallel()

	got := catalog.FormatStorageCaption(catalog.StorageCaption{
		FileID:          9,
		TmdbID:          1396,
		Title:           "Breaking Bad",
		Kind:            1,
		Source:          "tgforward",
		ExtKey:          "-1009900000005:555",
		SourceChannelID: -1009900000005,
		SourceMsgID:     555,
	})
	wantCoords := `🔗 <a href="https://www.themoviedb.org/movie/1396">tmdb</a> <code>1396</code> · file <code>9</code> · tgforward <code>-1009900000005:555</code> · <a href="https://t.me/c/9900000005/555">Source</a>`
	if !strings.HasSuffix(got, "\n"+wantCoords) {
		t.Fatalf("coords line: %q", got)
	}
}

// A title with < > & must ride through as text, not markup, and the coords line still renders as
// HTML (bare branch word when there is no TMDB id).
func TestFormatStorageCaptionEscapesMarkup(t *testing.T) {
	t.Parallel()

	got := catalog.FormatStorageCaption(catalog.StorageCaption{
		FileID: 1,
		Title:  "A <B> & C",
		Kind:   1,
	})
	if !strings.Contains(got, "A &lt;B&gt; &amp; C") {
		t.Fatalf("title not escaped: %q", got)
	}
	if strings.Contains(got, "A <B> & C") || strings.Contains(got, "<B>") {
		t.Fatalf("raw markup leaked: %q", got)
	}
	if !strings.HasSuffix(got, "\n🔗 movie · file <code>1</code>") {
		t.Fatalf("coords line: %q", got)
	}
}
