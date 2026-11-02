package api_test

import (
	"context"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
)

type stubSubProvider struct{}

func (stubSubProvider) Search(context.Context, uint64, bool, int, int, string) ([]api.SubtitlePack, error) {
	return []api.SubtitlePack{{
		Tag: "prov-1", ReleaseName: "lanterns-All-S01E05.srt", Lang: "fa",
	}}, nil
}

func (stubSubProvider) FetchPack(context.Context, string) ([]api.PackedSubtitle, error) {
	return []api.PackedSubtitle{{
		Name: "lanterns-All-S01E05.srt", Ext: ".srt",
		Text:   "1\n00:00:01,000 --> 00:00:02,000\nhi\n",
		Season: 1, Episode: 5, HasSeason: true, HasEpisode: true,
	}}, nil
}

type memSubtitleCache struct{}

func (memSubtitleCache) ListCachedSubtitles(context.Context, uint64, int16, int32, int32, string) ([]api.CachedSubtitle, error) {
	return nil, nil
}

func (memSubtitleCache) PutCachedSubtitle(context.Context, uint64, int16, int32, int32, string, string, string, string, int32, int64, int64) error {
	return nil
}

type captionUploader struct {
	fileName string
	caption  string
}

func (u *captionUploader) Upload(_ context.Context, fileName, text, caption string) (int64, int64, error) {
	u.fileName = fileName
	u.caption = caption
	if text == "" {
		return 0, 0, errEmptySubtitle
	}
	return -100, 99, nil
}

type stubSubDeliver struct{}

func (stubSubDeliver) CopyToReader(context.Context, int64, int64, int64, string) (int64, int64, error) {
	return 7, 8, nil
}

var errEmptySubtitle = stubErr("empty subtitle")

type stubErr string

func (e stubErr) Error() string { return string(e) }

func TestSubtitleFileStampsIndexerCaption(t *testing.T) {
	t.Parallel()

	const tmdbID = uint64(654321)
	browse := &fakeBrowse{items: map[itemKey]catalog.Item{
		{tmdbID, catalog.KindSeries}: {
			Row: catalog.Row{TMDBID: tmdbID, Kind: catalog.KindSeries, Title: "Lanterns", Year: 2026},
		},
	}}
	up := &captionUploader{}
	a := api.New(&fakeRails{}, &fakeDeliveries{}, &fakeCatalog{}, &fakeSearch{}, api.RateLimits{}).
		WithBrowse(browse).
		WithSubtitles(stubSubProvider{}, memSubtitleCache{}, up, stubSubDeliver{})

	list, err := a.Subtitle(context.Background(), api.Principal{}, &sushiv1.SubtitleReq{
		TmdbId: tmdbID, Kind: sushiv1.Kind_KIND_SERIES, SeasonNo: 1, EpisodeNo: 5, Lang: "fa",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.GetPacks()) != 1 {
		t.Fatalf("packs = %d", len(list.GetPacks()))
	}

	if _, err := a.SubtitleFile(context.Background(), api.Principal{ReaderChatID: 1}, &sushiv1.SubtitleFileReq{
		Tag: list.GetPacks()[0].GetTag(),
	}); err != nil {
		t.Fatal(err)
	}

	want := catalog.FormatSubtitleCaption(catalog.SubtitleCaption{
		Title: "Lanterns", Year: 2026, Kind: int16(catalog.KindSeries),
		SeasonNo: 1, EpisodeNo: 5, Lang: "fa",
		FileName: "lanterns-All-S01E05.srt", TmdbID: int64(tmdbID),
	})
	if up.caption != want {
		t.Fatalf("caption\n got %q\nwant %q", up.caption, want)
	}
}

type bluray23Provider struct{}

func (bluray23Provider) Search(context.Context, uint64, bool, int, int, string) ([]api.SubtitlePack, error) {
	return []api.SubtitlePack{{Tag: "prov", ReleaseName: "Show.BluRay", Lang: "fa"}}, nil
}

func (bluray23Provider) FetchPack(context.Context, string) ([]api.PackedSubtitle, error) {
	return []api.PackedSubtitle{{
		Name: "Show.S01E05.BluRay.srt", Ext: ".srt",
		Text:   "1\n00:00:01,000 --> 00:23:00,000\nhi\n",
		Season: 1, Episode: 5, HasSeason: true, HasEpisode: true,
	}}, nil
}

type hitSubtitleCache struct{}

func (hitSubtitleCache) ListCachedSubtitles(context.Context, uint64, int16, int32, int32, string) ([]api.CachedSubtitle, error) {
	return []api.CachedSubtitle{{
		ChannelID: -100, MessageID: 55, Ext: ".srt", Provider: "subdl", SubType: "bluray", DurationMin: 23,
	}}, nil
}

func (hitSubtitleCache) PutCachedSubtitle(context.Context, uint64, int16, int32, int32, string, string, string, string, int32, int64, int64) error {
	return nil
}

type recordingDeliver struct {
	channelID int64
	messageID int64
}

func (d *recordingDeliver) CopyToReader(_ context.Context, _, channelID, messageID int64, _ string) (int64, int64, error) {
	d.channelID = channelID
	d.messageID = messageID
	return 7, 8, nil
}

func TestSubtitleFileReusesArchivedCut(t *testing.T) {
	t.Parallel()

	const tmdbID = uint64(1081003)
	browse := &fakeBrowse{items: map[itemKey]catalog.Item{
		{tmdbID, catalog.KindMovie}: {
			Row: catalog.Row{TMDBID: tmdbID, Kind: catalog.KindMovie, Title: "Toy Story", Year: 2026},
		},
	}}
	up := &captionUploader{}
	deliver := &recordingDeliver{}
	a := api.New(&fakeRails{}, &fakeDeliveries{}, &fakeCatalog{}, &fakeSearch{}, api.RateLimits{}).
		WithBrowse(browse).
		WithSubtitles(bluray23Provider{}, hitSubtitleCache{}, up, deliver)

	// No duration on the list call, so the list itself does not short-circuit. The file
	// step still must not post a second copy of a cut the channel already has.
	list, err := a.Subtitle(context.Background(), api.Principal{}, &sushiv1.SubtitleReq{
		TmdbId: tmdbID, Kind: sushiv1.Kind_KIND_MOVIE, Lang: "TR",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SubtitleFile(context.Background(), api.Principal{ReaderChatID: 1}, &sushiv1.SubtitleFileReq{
		Tag: list.GetPacks()[0].GetTag(),
	}); err != nil {
		t.Fatal(err)
	}
	if up.fileName != "" || up.caption != "" {
		t.Fatalf("reused cut was uploaded: file=%q caption=%q", up.fileName, up.caption)
	}
	if deliver.channelID != -100 || deliver.messageID != 55 {
		t.Fatalf("delivered channel=%d message=%d, want -100/55", deliver.channelID, deliver.messageID)
	}
}
