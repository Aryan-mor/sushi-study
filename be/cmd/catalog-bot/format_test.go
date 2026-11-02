package main

import (
	"strconv"
	"strings"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/pg"
	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

func TestFileButtonTextPrefersDisplayName(t *testing.T) {
	t.Parallel()
	got := fileButtonText(catalog.File{
		QualityLabel: "1080p WEB-DL SoftSub",
		DisplayName:  "1080 soren soft sub",
		SizeBytes:    2 * 1024 * 1024 * 1024,
	})
	if got != "🎯 1080 soren soft sub · 2.0 GB" {
		t.Errorf("got %q", got)
	}
}

func TestFileButtonTextIncludesSize(t *testing.T) {
	t.Parallel()
	got := fileButtonText(catalog.File{QualityLabel: "1080p", SizeBytes: 2 * 1024 * 1024 * 1024})
	if got != "1080p · 2.0 GB" {
		t.Errorf("got %q", got)
	}
}

func TestFileButtonTextShowsCodecAndSize(t *testing.T) {
	t.Parallel()
	got := fileButtonText(catalog.File{
		QualityLabel: "1080 x265",
		AudioLangs:   "fa",
		SizeBytes:    412 * 1024 * 1024,
	})
	if !strings.Contains(got, "1080p") || !strings.Contains(got, "x265") {
		t.Errorf("want 1080p x265, got %q", got)
	}
	if !strings.Contains(got, "412 MB") {
		t.Errorf("want size, got %q", got)
	}
	if !strings.Contains(got, "dubbed") || !strings.HasPrefix(got, "🎤 ") {
		t.Errorf("want dub, got %q", got)
	}
}

func TestFileButtonTextNeverSaysFile(t *testing.T) {
	t.Parallel()
	got := fileButtonText(catalog.File{AudioLangs: "en", SizeBytes: 400 * 1024 * 1024})
	if strings.Contains(strings.ToLower(got), "file") {
		t.Errorf("generic telegram name leaked: %q", got)
	}
	if !strings.Contains(got, "انگلیسی") {
		t.Errorf("want spoken language, got %q", got)
	}
}

func TestFileButtonTextDubbedFromLabel(t *testing.T) {
	t.Parallel()
	got := fileButtonText(catalog.File{
		QualityLabel: "1080p - dubbed (Persian) - WEB-DL",
		AudioLangs:   "en",
		SizeBytes:    1024 * 1024 * 1024,
	})
	if !strings.Contains(got, "dubbed") || !strings.HasPrefix(got, "🎤 ") {
		t.Errorf("dubbed label not translated: %q", got)
	}
	if strings.Contains(got, "en") {
		t.Errorf("raw ISO leaked: %q", got)
	}
}

func TestFileButtonTextPersianAudioIsDub(t *testing.T) {
	t.Parallel()
	got := fileQualityLabel(catalog.File{Height: 720, AudioLangs: "fa"})
	if got != "720p · dubbed" {
		t.Errorf("got %q", got)
	}
}

func TestFileButtonTextHardSub(t *testing.T) {
	t.Parallel()
	got := fileButtonText(catalog.File{
		QualityLabel: "720p WEB-DL HardSub",
		Height:       720,
		SizeBytes:    400 * 1024 * 1024,
	})
	if !strings.HasPrefix(got, "🎯 ") || !strings.Contains(got, "720p") || !strings.Contains(got, "Hard sub") {
		t.Errorf("want hardsub on button, got %q", got)
	}
	if strings.Contains(got, "softsub") || strings.Contains(got, "دوبله") {
		t.Errorf("hardsub must not also say soft/dub: %q", got)
	}
}

func TestFileButtonTextSoftSub(t *testing.T) {
	t.Parallel()
	fromLabel := fileQualityLabel(catalog.File{QualityLabel: "1080p SoftSub WEB-DL", Height: 1080})
	if !strings.Contains(fromLabel, "softsub") {
		t.Errorf("want softsub from label, got %q", fromLabel)
	}
	fromLangs := fileQualityLabel(catalog.File{Height: 1080, SubLangs: "fa"})
	if !strings.Contains(fromLangs, "softsub") {
		t.Errorf("want softsub from sub_langs, got %q", fromLangs)
	}
}

func TestFileButtonTextHardSubBeatsFaAudio(t *testing.T) {
	t.Parallel()
	got := fileQualityLabel(catalog.File{
		QualityLabel: "720p HardSub",
		Height:       720,
		AudioLangs:   "fa",
	})
	if !strings.Contains(got, "Hard sub") {
		t.Errorf("hardsub token must win over fa audio, got %q", got)
	}
	if strings.Contains(got, "دوبله") {
		t.Errorf("hardsub file must not show as dubbed, got %q", got)
	}
}

func TestFileButtonTextBucketsOddHeight(t *testing.T) {
	t.Parallel()
	got := fileButtonText(catalog.File{
		QualityLabel: "480p WEB-DL",
		Height:       1706,
		SizeBytes:    707 * 1024 * 1024,
	})
	if strings.Contains(got, "1706") || strings.Contains(got, "480p") {
		t.Errorf("raw height must snap to 1080p, got %q", got)
	}
	if !strings.HasPrefix(got, "1080p WEB-DL") {
		t.Errorf("got %q", got)
	}
}

func TestFileButtonTextSoftSubMark(t *testing.T) {
	t.Parallel()
	got := fileButtonText(catalog.File{
		QualityLabel: "720p x265 10bit WEB-DL SoftSub",
		Height:       720,
		SizeBytes:    1100 * 1024 * 1024,
	})
	if !strings.HasPrefix(got, "🎯 720p") {
		t.Errorf("got %q", got)
	}
}

func TestFileButtonTextFourKTier(t *testing.T) {
	t.Parallel()
	got := fileTechLabel(catalog.File{QualityLabel: "4K WEB-DL", Height: 2160})
	if !strings.HasPrefix(got, "2160p") || strings.Contains(got, "4K") {
		t.Errorf("got %q", got)
	}
}

func TestFileButtonTextTracksBeatHardSubLabel(t *testing.T) {
	t.Parallel()
	got := fileQualityLabel(catalog.File{
		QualityLabel: "720p HardSub",
		Height:       720,
		SubLangs:     "fa",
	})
	if !strings.Contains(got, "softsub") {
		t.Errorf("container srt must show softsub, got %q", got)
	}
	if strings.Contains(got, "Hard sub") {
		t.Errorf("srt in file is not hardsub, got %q", got)
	}
}

func TestCatalogCaptionOmitsZeroTMDB(t *testing.T) {
	t.Parallel()
	got := catalogCaption(catalog.Item{Row: catalog.Row{Kind: catalog.KindSeries}}, pickSeason, "exampleCatalogChannel")
	if strings.Contains(got, "TMDB 0") {
		t.Errorf("zero tmdb printed:\n%s", got)
	}
	if !strings.Contains(got, "📺 <b>سریال</b>") {
		t.Errorf("missing kind:\n%s", got)
	}
	if !strings.Contains(got, "🦁 @exampleCatalogChannel") {
		t.Errorf("missing bot handle:\n%s", got)
	}
}

func TestCatalogCaptionDisenchantmentEpisode(t *testing.T) {
	t.Parallel()
	item := catalog.Item{
		Row:      catalog.Row{TMDBID: 73120, Kind: catalog.KindSeries, Title: "Disenchantment", Year: 2018, Rating: 74},
		Detail:   catalog.Detail{Genres: "Animation, Comedy"},
		Episodes: []catalog.Episode{{SeasonNo: 1, EpisodeNo: 1}, {SeasonNo: 1, EpisodeNo: 2}, {SeasonNo: 2, EpisodeNo: 1}},
		IMDBID:   "tt5363918",
	}
	got := catalogCaptionAt(item, episodePickedFooter(1, 4), "exampleCatalogChannel", 1, 4, "A Princess, an Elf, and a Demon Walk Into a Bar")
	for _, want := range []string{
		"imdb.com/title/tt5363918",
		"themoviedb.org/tv/73120",
		">IMDB<",
		">TMDB<",
		"Disenchantment",
		"فصل <b>1</b>",
		"قسمت <b>4</b>",
		"📅 2018",
		"🎭 Animation",
		"📚 2 فصل آماده",
		"🌞 #Disenchantment",
		"🦁 @exampleCatalogChannel",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "TMDB 0") {
		t.Errorf("tmdb 0 leaked:\n%s", got)
	}
}

func TestDeliveryCaptionEpisodeHasSeasonAndPersianLang(t *testing.T) {
	t.Parallel()
	item := catalog.Item{Row: catalog.Row{TMDBID: 73120, Kind: catalog.KindSeries, Title: "Disenchantment", Year: 2018}, IMDBID: "tt5363918"}
	origin := pg.FileOrigin{
		File: catalog.File{
			QualityLabel: "720p - dubbed (Persian)",
			AudioLangs:   "en",
			SubLangs:     "fa",
			Height:       720,
			SizeBytes:    250 * 1024 * 1024,
		},
		TMDBID:       73120,
		Kind:         catalog.KindSeries,
		Title:        "Disenchantment",
		SeasonNo:     1,
		EpisodeNo:    4,
		EpisodeTitle: "Castle Party Massacre",
	}
	got := deliveryCaption(item, origin)
	for _, want := range []string{
		"Disenchantment",
		"imdb.com/title/tt5363918",
		"themoviedb.org/tv/73120",
		"<code>tt5363918</code>",
		"<code>73120</code>",
		"<b>فصل:</b> 1",
		"<b>قسمت:</b> 4",
		"dubbed",
		"فارسی",
		"<b>کیفیت:</b> 720p",
		"<b>حجم:</b> 250 MB",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "TMDB 0") || strings.Contains(got, "\nen") || strings.Contains(got, ">en<") {
		t.Errorf("raw en / tmdb 0 leaked:\n%s", got)
	}
}

func TestDeliveryCaptionLoadsOriginWhenItemEmpty(t *testing.T) {
	t.Parallel()
	origin := pg.FileOrigin{
		File:      catalog.File{QualityLabel: "1080p", AudioLangs: "fa"},
		TMDBID:    550,
		Kind:      catalog.KindMovie,
		Title:     "Fight Club",
		Year:      1999,
		SeasonNo:  0,
		EpisodeNo: 0,
	}
	got := deliveryCaption(catalog.Item{}, origin)
	if strings.Contains(got, "TMDB 0") {
		t.Errorf("empty item printed tmdb 0:\n%s", got)
	}
	if !strings.Contains(got, "Fight Club") || !strings.Contains(got, "550") {
		t.Errorf("origin title/id missing:\n%s", got)
	}
	if strings.Contains(got, "<b>فصل:</b>") {
		t.Errorf("movie should not print season:\n%s", got)
	}
}

func TestTitleHashtag(t *testing.T) {
	t.Parallel()
	if got := titleHashtag("Disenchantment"); got != "#Disenchantment" {
		t.Errorf("got %q", got)
	}
	if titleHashtag("   ") != "" {
		t.Error("blank title invented a hashtag")
	}
}

func TestTitlePageURLPrefersIMDB(t *testing.T) {
	t.Parallel()
	got := titlePageURL(catalog.KindSeries, 73120, "tt5363918")
	if got != "https://www.imdb.com/title/tt5363918/" {
		t.Errorf("got %q", got)
	}
	if titlePageURL(catalog.KindMovie, 550, "nope") != "https://www.themoviedb.org/movie/550" {
		t.Error("invalid imdb should fall back to tmdb")
	}
	links := catalogIDLinks(catalog.KindMovie, 550, "tt0137523", true)
	if !strings.Contains(links, "imdb.com/title/tt0137523") || !strings.Contains(links, "themoviedb.org/movie/550") {
		t.Errorf("both ids missing: %s", links)
	}
}

func TestParseSeasonAndEpisodeArgs(t *testing.T) {
	t.Parallel()
	id, season, page, ok := parseSeasonArg("1399:2:1")
	if !ok || id != 1399 || season != 2 || page != 1 {
		t.Errorf("season = %d/%d/%d/%v", id, season, page, ok)
	}
	id, season, ep, ok := parseEpisodeArg("1399:2:8")
	if !ok || id != 1399 || season != 2 || ep != 8 {
		t.Errorf("episode = %d/%d/%d/%v", id, season, ep, ok)
	}
	if _, _, _, shortOK := parseEpisodeArg("1399:2"); shortOK {
		t.Error("short episode arg accepted")
	}
	tid, sn, idx, ok := parseBulkDeliverArg("73120:1:0")
	if !ok || tid != 73120 || sn != 1 || idx != 0 {
		t.Errorf("bulk = %d/%d/%d/%v", tid, sn, idx, ok)
	}
}

func TestParseShareQuery(t *testing.T) {
	t.Parallel()
	kind, id, ok := parseShareQuery(":share t:73120")
	if !ok || kind != catalog.KindSeries || id != 73120 {
		t.Errorf("got %d/%d/%v", kind, id, ok)
	}
}

func TestParseRequestArg(t *testing.T) {
	t.Parallel()
	id, kind, ok := parseRequestArg("550:1")
	if !ok || id != 550 || kind != catalog.KindMovie {
		t.Errorf("got %d/%d/%v", id, kind, ok)
	}
}

func TestParseTMDBPick(t *testing.T) {
	t.Parallel()
	kind, id, ok := parseTMDBPick("#tmdb:m:550 Fight Club")
	if !ok || kind != catalog.KindMovie || id != 550 {
		t.Errorf("got %d/%d/%v", kind, id, ok)
	}
	kind, id, ok = parseTMDBPick("see #tmdb:t:1399")
	if !ok || kind != catalog.KindSeries || id != 1399 {
		t.Errorf("series pick %d/%d/%v", kind, id, ok)
	}
}

func TestMovieEpisodeIsSeasonZero(t *testing.T) {
	t.Parallel()
	item := catalog.Item{Episodes: []catalog.Episode{
		{ID: 9, SeasonNo: 1, EpisodeNo: 1},
		{ID: 3, SeasonNo: 0, EpisodeNo: 0},
	}}
	ep, ok := movieEpisode(item)
	if !ok || ep.ID != 3 {
		t.Errorf("got %+v %v", ep, ok)
	}
}

func TestAdjacentEpisodes(t *testing.T) {
	t.Parallel()
	prev, next, okPrev, okNext := adjacentEpisodes(2, []int32{1, 2, 3})
	if !okPrev || !okNext || prev != 1 || next != 3 {
		t.Errorf("got %d/%d %v/%v", prev, next, okPrev, okNext)
	}
	_, _, okPrev, okNext = adjacentEpisodes(1, []int32{1, 2})
	if okPrev || !okNext {
		t.Errorf("first episode prev/next = %v/%v", okPrev, okNext)
	}
}

func TestEpisodeQualityKeyboardHasNavAndAll(t *testing.T) {
	t.Parallel()
	files := []catalog.File{{ID: 9, QualityLabel: "720p", Height: 720, AudioLangs: "fa", State: catalog.FileReady}}
	kb := episodeQualityKeyboard(files, 73120, 1, 2, []int32{1, 2, 3}, false)
	joined := keyboardTexts(kb)
	for _, want := range []string{allEpsBtn, prevEpBtn, nextEpBtn, otherEpBtn} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %q", want, joined)
		}
	}
	if strings.Contains(joined, shareBtn) {
		t.Errorf("unexpected %q in %q", shareBtn, joined)
	}
}

func TestCatalogCaptionPrefersPersianOverview(t *testing.T) {
	t.Parallel()
	item := catalog.Item{
		Row:        catalog.Row{TMDBID: 550, Kind: catalog.KindMovie, Title: "Fight Club", Year: 1999},
		Overview:   "An insomniac office worker forms an underground fight club.",
		OverviewFa: "یک کارمند بی‌خواب یک باشگاه مبارزهٔ زیرزمینی راه می‌اندازد.",
	}
	got := catalogCaption(item, pickDownload, "exampleCatalogChannel")
	if !strings.Contains(got, item.OverviewFa) {
		t.Errorf("persian overview missing:\n%s", got)
	}
	if strings.Contains(got, item.Overview) {
		t.Errorf("english overview leaked:\n%s", got)
	}
}

func TestSharePickDescriptionUsesPersianOverview(t *testing.T) {
	t.Parallel()
	fa := "یک کارمند بی‌خواب یک باشگاه مبارزهٔ زیرزمینی راه می‌اندازد."
	got := sharePickDescription(catalog.Item{
		Row:        catalog.Row{Kind: catalog.KindMovie, Year: 1999, Rating: 84},
		Overview:   "An insomniac office worker.",
		OverviewFa: fa,
	})
	if got != fa {
		t.Errorf("got %q", got)
	}
}

func TestSharePickDescriptionFallsBackToPersianMeta(t *testing.T) {
	t.Parallel()
	got := sharePickDescription(catalog.Item{
		Row:      catalog.Row{Kind: catalog.KindMovie, Year: 1999, Rating: 84},
		Overview: "An insomniac office worker.",
	})
	if strings.Contains(got, "insomniac") {
		t.Errorf("english overview leaked into the picker: %q", got)
	}
	if !strings.Contains(got, "فیلم") || !strings.Contains(got, "بزن تا بفرستی") {
		t.Errorf("got %q", got)
	}
}

func TestPersianOriginCollapsesFakeDubEnglishAndHardSub(t *testing.T) {
	t.Parallel()
	files := []catalog.File{
		{ID: 1, QualityLabel: "1080p", Height: 1080, AudioLangs: "fa", SizeBytes: 100, State: catalog.FileReady},
		{ID: 2, QualityLabel: "1080p", Height: 1080, AudioLangs: "en", SizeBytes: 900, State: catalog.FileReady},
		{ID: 3, QualityLabel: "720p HardSub original", Height: 720, AudioLangs: "fa", SizeBytes: 400, State: catalog.FileReady},
		{ID: 4, QualityLabel: "480p", Height: 480, AudioLangs: "fa", SizeBytes: 200, State: catalog.FileReady},
	}
	rows := qualityRowsAt(files, true)
	if len(rows) != 3 {
		t.Fatalf("rows %d, want 3 quality tiers", len(rows))
	}
	if rows[0][0].Data != cbFilePrefix+"2" {
		t.Fatalf("1080p kept %s, want the larger file", rows[0][0].Data)
	}
	joined := keyboardTexts(tg.InlineKeyboard{Rows: rows})
	for _, bad := range []string{"dubbed", "انگلیسی", "Hard sub", "فارسی"} {
		if strings.Contains(joined, bad) {
			t.Errorf("persian original must not offer %q:\n%s", bad, joined)
		}
	}
	foreign := fileQualityLabel(catalog.File{Height: 720, AudioLangs: "fa"})
	if foreign != "720p · dubbed" {
		t.Fatalf("non-persian fa audio still a dub, got %q", foreign)
	}
}

func TestQualityRowsOrderAndColor(t *testing.T) {
	t.Parallel()
	rows := qualityRows([]catalog.File{
		{ID: 1, QualityLabel: "720p WEB-DL SoftSub", Height: 720, State: catalog.FileReady},
		{ID: 2, QualityLabel: "1080p WEB-DL Dub", Height: 1080, State: catalog.FileReady},
		{ID: 3, QualityLabel: "1080p WEB-DL", Height: 1080, State: catalog.FileReady},
		{ID: 4, QualityLabel: "480p WEB-DL", Height: 480, State: catalog.FileReady},
	})
	if len(rows) != 4 {
		t.Fatalf("rows %d", len(rows))
	}
	wantID := []int64{3, 4, 1, 2}
	wantStyle := []string{"", "", tg.StyleSuccess, tg.StylePrimary}
	for i := range wantID {
		btn := rows[i][0]
		if btn.Data != cbFilePrefix+strconv.FormatInt(wantID[i], 10) {
			t.Errorf("row %d data %q", i, btn.Data)
		}
		if btn.Style != wantStyle[i] {
			t.Errorf("row %d style %q, want %q (%s)", i, btn.Style, wantStyle[i], btn.Text)
		}
	}
}

func keyboardTexts(kb tg.InlineKeyboard) string {
	var parts []string
	for _, row := range kb.Rows {
		for _, btn := range row {
			parts = append(parts, btn.Text)
		}
	}
	return strings.Join(parts, " | ")
}
