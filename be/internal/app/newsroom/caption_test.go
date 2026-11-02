package newsroom

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

func TestFormatPostMovieRich(t *testing.T) {
	t.Parallel()

	got := FormatPost(Announcement{
		TMDBID:   603,
		Kind:     catalog.KindMovie,
		Title:    "The Matrix",
		Poster:   "/m.jpg",
		Backdrop: "/b.jpg",
		Genres:   "Action, Science Fiction",
		Overview: "A hacker learns the truth.",
		RuntimeS: 8160,
		Year:     1999,
		Rating:   82,
		Country:  "US",
		Handle:   "exampleMainBot",
	})

	for _, want := range []string{
		`<h3><a href="https://www.themoviedb.org/movie/603">The Matrix (1999)</a></h3>`,
		"<tg-slideshow>",
		`src="https://image.tmdb.org/t/p/w500/m.jpg"`,
		`src="https://image.tmdb.org/t/p/w780/b.jpg"`,
		"<details>",
		"عنوان",
		"سال انتشار",
		"امتیاز",
		"مدت زمان",
		"ژانر",
		"سینما / VOD",
		"کشور تولید",
		"🇺🇸 آمریکا",
		"<pullquote>A hacker learns the truth.</pullquote>",
		censorNotice,
		"#TheMatrix",
		"@exampleMainBot",
		postClosing,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("post is missing %q\n--- got ---\n%s", want, got)
		}
	}
	if strings.Contains(got, "youtube.com") || strings.Contains(got, "تریلر") {
		t.Error("post without a trailer key must not mention the trailer")
	}
	if strings.Contains(got, "t.me") || strings.Contains(got, "oxnews") || strings.Contains(got, "#Sushi") {
		t.Error("post must not link t.me, copy the prototype's @exampleNews, or use #Sushi")
	}
	lionAt := strings.Index(got, "🦁")
	sunAt := strings.Index(got, "🌞")
	if lionAt < 0 || sunAt < 0 || lionAt > sunAt {
		t.Errorf("lion @handle must precede the title hashtag:\n%s", got)
	}
	if strings.Contains(got, "tg-emoji") {
		t.Error("the prototype custom-emoji ids are not ours")
	}
	if strings.Contains(got, ">شبکه</td>") {
		t.Error("movie without a network must omit the شبکه row")
	}
}

func TestFormatPostNetworkRow(t *testing.T) {
	t.Parallel()

	got := FormatPost(Announcement{
		TMDBID:  1396,
		Kind:    catalog.KindSeries,
		Title:   "Stranger Things",
		Network: "Netflix, Disney+",
	})
	if !strings.Contains(got, ">شبکه</td>") || !strings.Contains(got, "Netflix، Disney+") {
		t.Errorf("network row missing:\n%s", got)
	}
	absent := FormatPost(Announcement{
		TMDBID: 1, Kind: catalog.KindSeries, Title: "Silo", Network: catalog.NetworkNone,
	})
	if strings.Contains(absent, ">شبکه</td>") {
		t.Errorf("NetworkNone must omit the row:\n%s", absent)
	}
}

func TestFormatPostTrailerLink(t *testing.T) {
	t.Parallel()

	with := FormatPost(Announcement{
		TMDBID:     603,
		Kind:       catalog.KindMovie,
		Title:      "The Matrix",
		Country:    "US",
		TrailerKey: "dQw4w9WgXcQ",
	})
	const want = `<a href="https://www.youtube.com/watch?v=dQw4w9WgXcQ">تریلر</a>`
	linkAt := strings.Index(with, want)
	tableEnd := strings.Index(with, "</table>")
	countryAt := strings.Index(with, "کشور تولید")
	if linkAt < 0 || tableEnd < 0 || countryAt < 0 || countryAt > linkAt || linkAt > tableEnd {
		t.Errorf("trailer must be the last table row:\n%s", with)
	}
	if strings.Count(with, "youtube.com") != 1 {
		t.Errorf("trailer link must appear once:\n%s", with)
	}

	for _, key := range []string{"", "-", "not a key", `"><script>`} {
		got := FormatPost(Announcement{TMDBID: 1, Kind: catalog.KindMovie, Title: "Silo", TrailerKey: key})
		if strings.Contains(got, "youtube.com") || strings.Contains(got, "تریلر") {
			t.Errorf("key %q must not emit a trailer link:\n%s", key, got)
		}
	}
}

func TestFormatPostWithHeroUsesTelegramPhoto(t *testing.T) {
	t.Parallel()

	a := Announcement{
		TMDBID:   603,
		Kind:     catalog.KindMovie,
		Title:    "The Matrix",
		Poster:   "/m.jpg",
		Backdrop: "/b.jpg",
	}
	got := FormatPostWithHero(a)
	if !strings.Contains(got, `src="tg://photo?id=hero"`) {
		t.Errorf("hero post missing tg://photo:\n%s", got)
	}
	if strings.Contains(got, `src="https://image.tmdb.org/t/p/w500/m.jpg"`) {
		t.Error("hero post must not also ship the TMDB poster; it is inside the card")
	}
	if !strings.Contains(got, `src="https://image.tmdb.org/t/p/w780/b.jpg"`) {
		t.Errorf("hero post must keep the backdrop:\n%s", got)
	}
	plain := FormatPost(a)
	if !strings.Contains(plain, `src="https://image.tmdb.org/t/p/w500/m.jpg"`) {
		t.Error("FormatPost without hero must keep the TMDB poster")
	}
	if strings.Contains(plain, "tg://photo") {
		t.Error("FormatPost without hero must not mention tg://photo")
	}
}

func TestFormatPostSlideshowCapsAtFive(t *testing.T) {
	t.Parallel()

	got := FormatPostWithHero(Announcement{
		TMDBID:    603,
		Kind:      catalog.KindMovie,
		Title:     "The Matrix",
		Poster:    "/m.jpg",
		Backdrop:  "/b.jpg",
		Backdrops: []string{"one", "two", "three", "four", "five"},
	})
	if strings.Count(got, "<img ") != 5 {
		t.Errorf("slideshow want 5 frames, got %d\n%s", strings.Count(got, "<img "), got)
	}
	for _, stem := range []string{"one", "two", "three"} {
		if !strings.Contains(got, "/w780/"+stem+".jpg") {
			t.Errorf("missing gallery stem %s\n%s", stem, got)
		}
	}
	if strings.Contains(got, "/w780/four.jpg") || strings.Contains(got, "/w500/m.jpg") {
		t.Errorf("sixth frame or poster leaked:\n%s", got)
	}
}

func TestFormatPostEpisodeRich(t *testing.T) {
	t.Parallel()

	got := FormatPost(Announcement{
		TMDBID:   1396,
		Kind:     catalog.KindSeries,
		Title:    "Breaking Bad",
		Subtitle: "Ozymandias",
		Season:   5,
		Episode:  14,
		Year:     2008,
	})

	if !strings.Contains(got, "قسمت جدید · فصل پنجم · قسمت 14 از «Breaking Bad» آماده شد!") {
		t.Errorf("episode announcement missing:\n%s", got)
	}
	if !strings.Contains(got, "فصل پنجم · قسمت 14 · «Ozymandias»") {
		t.Errorf("episode meta cell missing:\n%s", got)
	}
	if !strings.Contains(got, "/tv/1396") {
		t.Errorf("series should link to /tv:\n%s", got)
	}
	if strings.Contains(got, "سینما / VOD") {
		t.Error("episode post should not claim cinema/VOD")
	}
	if strings.Contains(got, ">شبکه</td>") {
		t.Error("empty network must omit the شبکه row")
	}
	if strings.Contains(got, "کشور تولید") {
		t.Error("empty country must omit the row")
	}
}

func TestFormatPostDropsGenericEpisodeTitle(t *testing.T) {
	t.Parallel()

	got := FormatPost(Announcement{
		TMDBID: 1, Kind: catalog.KindSeries, Title: "Severance", Subtitle: "severance",
		Season: 1, Episode: 2,
	})
	if strings.Contains(got, "· «severance»") || strings.Contains(got, "· «Severance»") {
		t.Errorf("meta cell kept a title identical to the series:\n%s", got)
	}

	generic := FormatPost(Announcement{
		TMDBID: 1, Kind: catalog.KindSeries, Title: "Silo", Subtitle: "قسمت ۲",
		Season: 1, Episode: 2,
	})
	if strings.Contains(generic, "«قسمت ۲»") {
		t.Errorf("kept a generic episode title:\n%s", generic)
	}
}

func TestOverviewTruncationNeverSplitsARune(t *testing.T) {
	t.Parallel()

	persian := strings.Repeat("سلام دنیا ", 400)
	got := FormatPost(Announcement{
		TMDBID: 1, Kind: catalog.KindMovie, Title: "تست", Overview: persian,
	})

	if !utf8.ValidString(got) {
		t.Fatal("post is not valid UTF-8: a multi-byte character was cut in half")
	}
	if strings.ContainsRune(got, '\uFFFD') {
		t.Error("post contains a replacement character")
	}
}

func TestFormatPostFitsRichBudget(t *testing.T) {
	t.Parallel()

	got := FormatPost(Announcement{
		TMDBID:   999999,
		Kind:     catalog.KindSeries,
		Title:    strings.Repeat("عنوان بسیار طولانی ", 12),
		Subtitle: strings.Repeat("نام قسمت ", 12),
		Genres:   "Action, Drama, Comedy, Thriller, Horror, Romance",
		Overview: strings.Repeat("خلاصه داستان طولانی ", 300),
		Season:   12,
		Episode:  345,
		Year:     2026,
		Rating:   99,
		RuntimeS: 7200,
	})

	if n := utf8.RuneCountInString(got); n > richMax {
		t.Errorf("rich post is %d runes, over %d", n, richMax)
	}
}

func TestFormatCaptionStillFitsAPhoto(t *testing.T) {
	t.Parallel()

	got := FormatCaption(Announcement{
		TMDBID: 603, Kind: catalog.KindMovie, Title: "The Matrix",
		Genres: "Action, Science Fiction", Overview: "A hacker learns the truth.",
		RuntimeS: 8160, Year: 1999, Rating: 82, Handle: "exampleMainBot",
	})
	if !strings.Contains(got, "🎬 <b>فیلم جدید اضافه شد</b>") {
		t.Errorf("caption fallback lost the old heading:\n%s", got)
	}
	if !strings.Contains(got, "@exampleMainBot") || !strings.Contains(got, "#TheMatrix") {
		t.Errorf("caption fallback lost footer:\n%s", got)
	}
	if n := utf8.RuneCountInString(got); n > captionMax {
		t.Errorf("caption is %d runes, over Telegram's %d", n, captionMax)
	}
}

func TestGenreLineCapsAtFour(t *testing.T) {
	t.Parallel()

	got := genreLine("A, B, C, D, E, F")
	if strings.Count(got, "،") != 3 {
		t.Errorf("genre line = %q, want four names (three separators)", got)
	}
	if strings.Contains(got, "E") {
		t.Errorf("genre line = %q, want the fifth genre dropped", got)
	}
	if genreLine("") != "" {
		t.Error("no genres should render nothing, not an empty bullet")
	}
}

func TestUserContentIsEscaped(t *testing.T) {
	t.Parallel()

	got := FormatPost(Announcement{
		TMDBID: 7, Kind: catalog.KindMovie,
		Title:    `Dogs & <b>Cats</b>`,
		Overview: `5 < 6 & "quoted"`,
		Genres:   "Sci-Fi & Fantasy",
	})

	if strings.Contains(got, "<b>Cats</b>") {
		t.Error("a title carrying HTML was not escaped")
	}
	if !strings.Contains(got, "Dogs &amp; ") {
		t.Errorf("ampersand not escaped:\n%s", got)
	}
	if !strings.Contains(got, "Sci-Fi &amp; Fantasy") {
		t.Errorf("genre ampersand not escaped:\n%s", got)
	}
}

func TestPosterURL(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":          "",
		"   ":       "",
		"/abc.jpg":  "https://image.tmdb.org/t/p/w500/abc.jpg",
		"abc.jpg":   "https://image.tmdb.org/t/p/w500/abc.jpg",
		"abc":       "https://image.tmdb.org/t/p/w500/abc.jpg",
		" /x  ":     "https://image.tmdb.org/t/p/w500/x.jpg",
		" /x.jpg  ": "https://image.tmdb.org/t/p/w500/x.jpg",
	}
	for in, want := range cases {
		if got := PosterURL(in); got != want {
			t.Errorf("PosterURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPersianDigits(t *testing.T) {
	t.Parallel()

	if got := persianDigits(2026); got != "2026" {
		t.Errorf("persianDigits(2026) = %q", got)
	}
	if got := persianDecimal(78); got != "7.8" {
		t.Errorf("persianDecimal(78) = %q", got)
	}
}

func TestMediaHashtag(t *testing.T) {
	t.Parallel()

	if got := mediaHashtag("The Matrix"); got != "#TheMatrix" {
		t.Errorf("mediaHashtag = %q", got)
	}
	if mediaHashtag("   ") != "" {
		t.Error("blank title should not invent a hashtag")
	}
}

func TestLionLineIsMainBotHandle(t *testing.T) {
	t.Parallel()

	if got := brandMention(" @exampleMainBot "); got != "@exampleMainBot" {
		t.Errorf("brandMention = %q", got)
	}
	if brandMention("") != "" || brandMention("  @  ") != "" {
		t.Error("empty handle must not invent a mention")
	}

	got := FormatPost(Announcement{TMDBID: 1, Kind: catalog.KindMovie, Title: "Silo"})
	if strings.Contains(got, "🦁") || strings.Contains(got, "#Sushi") {
		t.Errorf("no handle should omit the lion line:\n%s", got)
	}
	if !strings.Contains(got, "🌞") {
		t.Errorf("title hashtag should still render:\n%s", got)
	}
}

func TestFormatPreviewWrapsTheRealPost(t *testing.T) {
	t.Parallel()

	a := Announcement{ID: 42, TMDBID: 603, Kind: catalog.KindMovie, Title: "The Matrix", Year: 1999}
	post := FormatPost(a)

	for locale, header := range map[string]string{"en": previewHeaderEN, "fa": previewHeaderFA} {
		got := FormatPreview(a, locale)
		if !strings.Contains(got, header) {
			t.Errorf("%s preview is missing its header", locale)
		}
		if !strings.Contains(got, post) {
			t.Errorf("%s preview does not contain the post verbatim:\n%s", locale, got)
		}
		if !strings.Contains(got, "#42") || !strings.Contains(got, "603") {
			t.Errorf("%s preview should carry the ids staff review by:\n%s", locale, got)
		}
	}
}
