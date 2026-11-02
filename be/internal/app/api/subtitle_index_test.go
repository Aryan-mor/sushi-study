package api

import (
	"context"
	"testing"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
)

func cueAt(end string) string {
	return "1\n00:00:01,000 --> " + end + "\nhi\n"
}

func TestSiblingPackIndexesSeasonPack(t *testing.T) {
	t.Parallel()
	files := []PackedSubtitle{
		{Name: "Friends.S03E01.srt", Ext: ".srt", Text: cueAt("00:22:32,500"), Season: 3, Episode: 1, HasSeason: true, HasEpisode: true},
		{Name: "Friends.S03E12.srt", Ext: ".srt", Text: cueAt("00:22:32,500"), Season: 3, Episode: 12, HasSeason: true, HasEpisode: true},
		{Name: "Friends.S03E12.WEB-DL.srt", Ext: ".srt", Text: cueAt("00:21:10,000"), Season: 3, Episode: 12, HasSeason: true, HasEpisode: true},
		{Name: "Friends.S02E01.srt", Ext: ".srt", Text: cueAt("00:22:00,000"), Season: 2, Episode: 1, HasSeason: true, HasEpisode: true},
		{Name: "notes.srt", Ext: ".srt", Text: cueAt("00:22:00,000")},
		{Name: "Friends.S03E05.sdh.srt", Ext: ".srt", Text: cueAt("00:22:00,000"), Season: 3, Episode: 5, HasSeason: true, HasEpisode: true, SDH: true},
		{Name: "Friends.S03E04.srt", Ext: ".srt", Text: "no cues", Season: 3, Episode: 4, HasSeason: true, HasEpisode: true},
	}
	got := siblingPackIndexes(files, 1, "Friends.S03.Complete.1080p.BluRay", 3, 12)
	want := map[string]string{
		"Friends.S03E01.srt":        "1/bluray",
		"Friends.S03E12.WEB-DL.srt": "12/web",
	}
	if len(got) != len(want) {
		t.Fatalf("indexed %d, want %d: %+v", len(got), len(want), namesOf(got))
	}
	for _, item := range got {
		key := want[item.File.Name]
		gotKey := ""
		switch item.Episode {
		case 1:
			gotKey = "1/bluray"
		case 12:
			gotKey = "12/web"
		}
		if key == "" || gotKey != key || item.Season != 3 {
			t.Fatalf("unexpected %+v", item)
		}
	}
}

func TestSiblingPackIndexesSkipsTypeless(t *testing.T) {
	t.Parallel()
	files := []PackedSubtitle{
		{Name: "Show.S01E02.srt", Ext: ".srt", Text: cueAt("00:40:00,000"), Season: 1, Episode: 2, HasSeason: true, HasEpisode: true},
		{Name: "Show.S01E03.srt", Ext: ".srt", Text: cueAt("00:40:00,000"), Season: 1, Episode: 3, HasSeason: true, HasEpisode: true},
	}
	if got := siblingPackIndexes(files, 0, "1080p", 1, 2); len(got) != 0 {
		t.Fatalf("typeless pack must not be indexed, got %+v", got)
	}
}

func TestSiblingPackIndexesMovie(t *testing.T) {
	t.Parallel()
	files := []PackedSubtitle{
		{Name: "Movie.BluRay.srt", Ext: ".srt", Text: cueAt("02:00:00,000")},
		{Name: "Movie.WEB-DL.srt", Ext: ".srt", Text: cueAt("01:55:00,000")},
		{Name: "Movie.S01E01.srt", Ext: ".srt", Text: cueAt("00:40:00,000"), Season: 1, Episode: 1, HasSeason: true, HasEpisode: true},
	}
	got := siblingPackIndexes(files, 0, "Movie.2020.BluRay", 0, 0)
	if len(got) != 1 || got[0].File.Name != "Movie.WEB-DL.srt" || got[0].Season != 0 || got[0].Episode != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestPickPackedSubtitleSeasonPack(t *testing.T) {
	t.Parallel()
	files := []PackedSubtitle{
		{Name: "Friends.S03E01.srt", Text: "e01", Season: 3, Episode: 1, HasSeason: true, HasEpisode: true},
		{Name: "Friends.S03E12.srt", Text: "e12", Season: 3, Episode: 12, HasSeason: true, HasEpisode: true},
	}
	got, idx, err := pickPackedSubtitle(files, 3, 12)
	if err != nil {
		t.Fatal(err)
	}
	if idx != 1 || got.Text != "e12" {
		t.Fatalf("got %q idx %d", got.Text, idx)
	}
	if _, _, err := pickPackedSubtitle(files, 3, 9); err == nil {
		t.Fatal("missing episode must error")
	}
}

type rateStopUploader struct{ calls int }

func (u *rateStopUploader) Upload(context.Context, string, string, string) (int64, int64, error) {
	u.calls++
	if u.calls >= 2 {
		return 0, 0, &tg.Error{Code: 429, Description: "Too Many Requests", RetryAfter: time.Second}
	}
	return 1, int64(u.calls), nil
}

type nopSubtitleCache struct{}

func (nopSubtitleCache) ListCachedSubtitles(context.Context, uint64, int16, int32, int32, string) ([]CachedSubtitle, error) {
	return nil, nil
}

func (nopSubtitleCache) PutCachedSubtitle(context.Context, uint64, int16, int32, int32, string, string, string, string, int32, int64, int64) error {
	return nil
}

func TestIndexSiblingSubsStopsOnRateLimit(t *testing.T) {
	t.Parallel()

	prev := siblingIndexPace
	siblingIndexPace = 0
	t.Cleanup(func() { siblingIndexPace = prev })

	up := &rateStopUploader{}
	a := &API{subtitleUploader: up, subtitleCache: nopSubtitleCache{}}
	files := []PackedSubtitle{
		{Name: "a.srt", Ext: ".srt", Text: cueAt("00:20:00,000")},
		{Name: "b.srt", Ext: ".srt", Text: cueAt("00:20:00,000")},
		{Name: "c.srt", Ext: ".srt", Text: cueAt("00:20:00,000")},
	}
	siblings := []packIndex{
		{File: files[0], Season: 3, Episode: 1},
		{File: files[1], Season: 3, Episode: 2},
		{File: files[2], Season: 3, Episode: 3},
	}
	a.indexSiblingSubs(context.Background(), subtitleTag{TmdbID: 1, Lang: "fa"}, siblings)
	if up.calls != 2 {
		t.Fatalf("uploads = %d, want 2 (stop after 429, do not fire the rest)", up.calls)
	}
}

func namesOf(items []packIndex) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.File.Name
	}
	return out
}
