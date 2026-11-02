package contentnotify

import (
	"context"
	"strings"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

// The bug this locks down: what the database holds is the poster's file stem -- no leading slash,
// no ".jpg" (catalog.PosterPath). Building a URL without putting the extension back gives a 404,
// which surfaces only as a failed sendPhoto, and dmRich then downgrades to a text DM. Every
// content-request notification lost its artwork that way, silently.
func TestNotificationPosterURLIsBuiltFromTheStoredStem(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		alerts: []AdminAlert{{
			ID: 7, TMDBID: 42, Kind: catalog.KindMovie, Title: "Dune",
			Poster: "mk8UH7JRmK8adcqJJpB1ygP7B1C",
		}},
		admins: []Admin{{TelegramID: 100}},
	}
	msg := &fakeMessenger{}
	newReconciler(store, msg).tick(context.Background())

	want := "https://image.tmdb.org/t/p/w500/mk8UH7JRmK8adcqJJpB1ygP7B1C.jpg"
	if len(msg.photos) != 1 || msg.photos[0] != want {
		t.Errorf("photos = %v, want [%s]", msg.photos, want)
	}
}

// A follow DM used to be text-only, so a new episode arrived without the series' artwork even
// though title_cards had a poster for it.
func TestFollowNoticeCarriesThePoster(t *testing.T) {
	t.Parallel()

	store := &fakeStore{followNotices: []FollowNotice{{
		UserID: 8, RequesterTG: 500, TMDBID: 5, MaxSeason: 2, MaxEp: 20,
		Title: "The Show", Poster: "abc123", NewCount: 1,
	}}}
	msg := &fakeMessenger{}
	newReconciler(store, msg).tick(context.Background())

	if len(msg.photos) != 1 || msg.photos[0] != "https://image.tmdb.org/t/p/w500/abc123.jpg" {
		t.Errorf("photos = %v, want the series poster", msg.photos)
	}
}

// These DMs used to carry Persian and English in one body. Each recipient's locale now travels
// with the row, so each person reads one language.
func TestRequesterNoticeIsWrittenInTheRequestersLocaleOnly(t *testing.T) {
	t.Parallel()

	base := RequesterNotice{TMDBID: 42, Kind: catalog.KindMovie, Title: "Dune"}

	fa := base
	fa.Locale = "fa"
	got := formatRequesterNotice(fa)
	if !strings.Contains(got, "آماده") {
		t.Errorf("fa notice is not in Persian:\n%s", got)
	}
	if strings.Contains(got, "Ready to watch") || strings.Contains(got, "press play") {
		t.Errorf("fa notice still carries the English copy:\n%s", got)
	}

	got = formatRequesterNotice(base) // no locale -> English
	if !strings.Contains(got, "Ready to watch") {
		t.Errorf("default notice is not in English:\n%s", got)
	}
	if strings.Contains(got, "آماده") {
		t.Errorf("English notice still carries the Persian copy:\n%s", got)
	}
}

// Two admins reading different languages get one alert each, in their own.
func TestAdminAlertIsRenderedPerRecipientLocale(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		alerts: []AdminAlert{{ID: 7, TMDBID: 42, Kind: catalog.KindMovie, Title: "Dune"}},
		admins: []Admin{{TelegramID: 100, Locale: "en"}, {TelegramID: 200, Locale: "fa"}},
	}
	msg := &fakeMessenger{}
	newReconciler(store, msg).tick(context.Background())

	if len(msg.texts) != 2 {
		t.Fatalf("sent %d alerts, want one per admin", len(msg.texts))
	}
	if !strings.Contains(msg.texts[0], "New request") || strings.Contains(msg.texts[0], "درخواست") {
		t.Errorf("admin 100 should read English only:\n%s", msg.texts[0])
	}
	if !strings.Contains(msg.texts[1], "درخواست") || strings.Contains(msg.texts[1], "New request") {
		t.Errorf("admin 200 should read Persian only:\n%s", msg.texts[1])
	}
}

func TestAdminAlertNamesSubtitleLanguageOrADash(t *testing.T) {
	t.Parallel()

	with := formatAdminAlert(AdminAlert{
		TMDBID: 42, Kind: catalog.KindMovie, Title: "Dune", SubtitleLang: "en",
	}, "fa")
	if !strings.Contains(with, "زیرنویس: <code>en</code>") {
		t.Errorf("caption missing chosen subtitle lang:\n%s", with)
	}

	none := formatAdminAlert(AdminAlert{
		TMDBID: 42, Kind: catalog.KindMovie, Title: "Dune",
	}, "fa")
	if !strings.Contains(none, "زیرنویس: <code>-</code>") {
		t.Errorf("caption missing the empty-lang dash:\n%s", none)
	}
}

// Year and rating sat unused in title_cards while the DM showed the title alone. rating is stored
// scaled to 0-100, so 82 has to read as 8.2 and not as 82.
func TestCardShowsYearAndRatingOutOfTen(t *testing.T) {
	t.Parallel()

	got := formatRequesterNotice(RequesterNotice{
		TMDBID: 42, Kind: catalog.KindMovie, Title: "Dune", Year: 2021, Rating: 82,
	})
	for _, want := range []string{"📅 2021", "⭐ 8.2"} {
		if !strings.Contains(got, want) {
			t.Errorf("card missing %q:\n%s", want, got)
		}
	}
}

// About a quarter of the catalog has neither year nor rating (doc 12 §7.1). A card for one of
// those must lose the line, not print zeroes into it.
func TestCardDropsTheMetaLineWhenThereIsNothingToPutOnIt(t *testing.T) {
	t.Parallel()

	got := formatRequesterNotice(RequesterNotice{TMDBID: 42, Kind: catalog.KindMovie, Title: "Dune"})
	for _, unwanted := range []string{"📅", "⭐", "\n\n\n"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("card should not contain %q with no year or rating:\n%s", unwanted, got)
		}
	}
}

// Telegram measures a caption in characters and rejects the send over 1024. The title is the one
// unbounded field, so it is clamped on the way in -- clamping the rendered HTML instead could cut
// a tag in half and fail the whole message.
func TestALongTitleIsClampedSoTheCaptionStaysSendable(t *testing.T) {
	t.Parallel()

	got := formatAdminAlert(AdminAlert{
		TMDBID: 42, Kind: catalog.KindMovie,
		Title:             strings.Repeat("ط", 600),
		IMDBID:            "tt1160419",
		RequesterTG:       99,
		RequesterName:     strings.Repeat("ن", 300),
		RequesterUsername: "ada_l",
	}, "fa")

	if n := len([]rune(got)); n > 1024 {
		t.Errorf("caption is %d characters, over Telegram's 1024 limit:\n%s", n, got)
	}
	if !strings.Contains(got, "…") {
		t.Errorf("a clamped title should say it was cut:\n%s", got)
	}
}

// A title with markup characters in it must not be able to break the parse -- Telegram rejects the
// whole message on a bad entity, so an unescaped '<' would lose the notification (R-SEC-10).
func TestTitleMarkupIsEscaped(t *testing.T) {
	t.Parallel()

	got := formatRequesterNotice(RequesterNotice{TMDBID: 42, Title: `<i>& "Dune"`})
	if strings.Contains(got, `<i>`) {
		t.Errorf("title markup reached the caption unescaped:\n%s", got)
	}
	if !strings.Contains(got, "&lt;i&gt;&amp;") {
		t.Errorf("title should be HTML-escaped:\n%s", got)
	}
}

// A title we have never hydrated still has to name itself.
func TestCardFallsBackToTheTMDBIDWithNoTitle(t *testing.T) {
	t.Parallel()

	got := formatRequesterNotice(RequesterNotice{TMDBID: 603, Kind: catalog.KindMovie})
	if !strings.Contains(got, "TMDB 603") {
		t.Errorf("card should fall back to the id:\n%s", got)
	}
}
