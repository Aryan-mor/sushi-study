package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
	"github.com/Aryan-mor/sushi-study/be/internal/app/newsroom"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

func quietTestLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewsPostKeyboardWatchLaterIsAMainBotDeepLink(t *testing.T) {
	t.Parallel()

	kb := newsPostKeyboard(newsroom.Announcement{TMDBID: 1396, Kind: catalog.KindSeries}, "exampleMainBot", "")
	if len(kb.Rows) != 2 {
		t.Fatalf("keyboard = %d rows, want app then later", len(kb.Rows))
	}
	app := kb.Rows[0][0]
	if app.URL != tg.StartURL("exampleMainBot", "app_t_1396") {
		t.Errorf("app URL = %q", app.URL)
	}
	if app.Style != tg.StylePrimary {
		t.Errorf("app style = %q, want primary", app.Style)
	}
	if app.Text != newsBtnApp {
		t.Errorf("app label = %q", app.Text)
	}
	later := kb.Rows[1][0]
	if later.URL != tg.StartURL("exampleMainBot", "wl_t_1396") {
		t.Errorf("watch-later URL = %q", later.URL)
	}
	if later.Data != "" {
		t.Errorf("URL button must not also carry callback data %q", later.Data)
	}
	if later.Text != newsBtnLater {
		t.Errorf("button label = %q", later.Text)
	}
}

func TestNewsPostKeyboardAddsDownloadWhenCatalogBotIsSet(t *testing.T) {
	t.Parallel()

	kb := newsPostKeyboard(
		newsroom.Announcement{TMDBID: 550, Kind: catalog.KindMovie},
		"exampleMainBot",
		"exampleCatalogBot",
	)
	if len(kb.Rows) != 2 || len(kb.Rows[1]) != 2 {
		t.Fatalf("keyboard = %d rows, want app then download+later", len(kb.Rows))
	}
	if kb.Rows[0][0].URL != tg.StartURL("exampleMainBot", "app_m_550") {
		t.Errorf("app URL = %q", kb.Rows[0][0].URL)
	}
	dl := kb.Rows[1][0]
	if dl.Text != newsBtnDownload {
		t.Errorf("download label = %q", dl.Text)
	}
	if dl.URL != tg.StartURL("exampleCatalogBot", "m_550") {
		t.Errorf("download URL = %q", dl.URL)
	}
	if dl.Style != tg.StyleSuccess {
		t.Errorf("download style = %q, want success", dl.Style)
	}
	if kb.Rows[1][1].URL != tg.StartURL("exampleMainBot", "wl_m_550") {
		t.Errorf("watch-later URL = %q", kb.Rows[1][1].URL)
	}
}

func TestNewsPostKeyboardFallsBackToCallbackWithoutMainUsername(t *testing.T) {
	t.Parallel()

	kb := newsPostKeyboard(newsroom.Announcement{TMDBID: 1396, Kind: catalog.KindSeries}, "", "")
	btn := kb.Rows[0][0]
	if btn.Data != cbNewsLaterPrefix+"1396:2" {
		t.Errorf("callback data = %q", btn.Data)
	}
	if btn.URL != "" {
		t.Errorf("fallback must not carry a URL (%q)", btn.URL)
	}
}

func TestNewsCallbackDataFitsTelegramsLimit(t *testing.T) {
	t.Parallel()

	widest := newsPostKeyboard(newsroom.Announcement{TMDBID: ^uint64(0), Kind: catalog.KindSeries}, "", "")
	if n := len(widest.Rows[0][0].Data); n > 64 {
		t.Errorf("watch-later callback data is %d bytes, over Telegram's 64", n)
	}
	for _, btn := range newsDecisionKeyboard(int64(1)<<62, "fa").Rows[0] {
		if n := len(btn.Data); n > 64 {
			t.Errorf("%q callback data is %d bytes, over Telegram's 64", btn.Text, n)
		}
	}
}

func TestParseNewsTarget(t *testing.T) {
	t.Parallel()

	ok := []struct {
		in     string
		tmdbID uint64
		kind   catalog.Kind
	}{
		{"603:1", 603, catalog.KindMovie},
		{"1396:2", 1396, catalog.KindSeries},
		{" 603 : 1 ", 603, catalog.KindMovie},
	}
	for _, want := range ok {
		in := want.in
		gotID, gotKind, valid := parseNewsTarget(in)
		if !valid || gotID != want.tmdbID || gotKind != want.kind {
			t.Errorf("parseNewsTarget(%q) = %d/%d/%v, want %d/%d/true",
				in, gotID, gotKind, valid, want.tmdbID, want.kind)
		}
	}
	for _, in := range []string{"", "603", "603:", ":1", "603:0", "603:3", "0:1", "abc:1", "603:x", "-1:1"} {
		if _, _, valid := parseNewsTarget(in); valid {
			t.Errorf("parseNewsTarget(%q) accepted a malformed target", in)
		}
	}
}

func TestNewsDecisionKeyboardHasApproveAndReject(t *testing.T) {
	t.Parallel()

	kb := newsDecisionKeyboard(42, "en")
	if !rowsContain(kb.Rows, cbNewsApprovePrefix+"42") {
		t.Error("preview keyboard is missing approve")
	}
	if !rowsContain(kb.Rows, cbNewsRejectPrefix+"42") {
		t.Error("preview keyboard is missing reject")
	}
}

func TestRouteNewsCallbackIgnoresOtherPanels(t *testing.T) {
	t.Parallel()

	b := &bot{}
	for _, data := range []string{
		cbMenuHome, cbMenuSettings, cbMenuRequests, cbReqPagePrefix + "1",
		cbBotTest, "l:fa", "q:1080",
	} {
		q := &tg.CallbackQuery{ID: "1", Data: data, From: &tg.User{ID: 7}}
		if b.routeNewsCallback(context.Background(), q, identity.User{}, "en", quietTestLog()) {
			t.Errorf("routeNewsCallback claimed %q", data)
		}
	}
}

func TestAllowNewsTapEnforcesTheHourlyQuota(t *testing.T) {
	t.Parallel()

	b := &bot{newsTapQuota: 3}
	for i := 1; i <= 3; i++ {
		if !b.allowNewsTap(42) {
			t.Fatalf("tap %d was refused inside the quota", i)
		}
	}
	if b.allowNewsTap(42) {
		t.Error("the fourth tap should be refused")
	}
	if !b.allowNewsTap(43) {
		t.Error("a different user was caught by someone else's quota")
	}
	b.newsTaps[42] = newsTapWindow{taps: 3, resetsAt: time.Now().Add(-time.Minute)}
	if !b.allowNewsTap(42) {
		t.Error("a past window should have reset")
	}
}

func TestAllowNewsTapUnlimitedWhenQuotaIsZero(t *testing.T) {
	t.Parallel()

	b := &bot{newsTapQuota: 0}
	for i := 0; i < 100; i++ {
		if !b.allowNewsTap(42) {
			t.Fatal("a zero quota should mean no limit, not no taps")
		}
	}
}
