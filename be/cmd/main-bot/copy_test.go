package main

import (
	"strings"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
	"github.com/Aryan-mor/sushi-study/be/internal/app/onboarding"
)

func TestBotSetupIntroIsNumberedHTML(t *testing.T) {
	t.Parallel()

	if !strings.Contains(botSetupIntro.en, "<b>1.</b>") || !strings.Contains(botSetupIntro.en, "Create the bot") {
		t.Fatal("english walkthrough must create the bot inside the Mini App, not via /newbot")
	}
	if !strings.Contains(botSetupIntro.fa, "<b>1.</b>") || !strings.Contains(botSetupIntro.fa, "بات رو بساز") {
		t.Fatal("persian walkthrough must create the bot inside the Mini App, not via /newbot")
	}
	if strings.Contains(botSetupIntro.en, "/newbot") || strings.Contains(botSetupIntro.fa, "/newbot") {
		t.Fatal("creating the bot and Bot to Bot must share the Mini App path")
	}
	if !strings.Contains(botSetupIntro.en, "reply") && !strings.Contains(botSetupIntro.en, "Reply") {
		t.Fatal("english must ask for a reply, not a glass tap")
	}
	if !strings.Contains(botSetupIntro.fa, "ریپلای") {
		t.Fatal("persian must ask for a reply")
	}
	if !strings.Contains(botSetupIntro.en, "Mini App") || !strings.Contains(botSetupIntro.fa, "مینی‌اپ") {
		t.Fatal("walkthrough must send Bot to Bot setup through BotFather's Mini App")
	}
	if !strings.Contains(botSetupIntro.en, "Open") || !strings.Contains(botSetupIntro.fa, "Open") {
		t.Fatal("walkthrough must mention the Open button at the bottom of BotFather")
	}
}

func TestEnrollNeedBotToBotNamesTheSettingAndAsksForAReply(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ lang, text, reply string }{
		{"en", enrollNeedBotToBot.en, "reply"},
		{"fa", enrollNeedBotToBot.fa, "ریپلای"},
	} {
		if !strings.Contains(tc.text, "Bot to Bot Communication Mode") {
			t.Fatalf("%s copy must name the exact BotFather setting to turn on", tc.lang)
		}
		if !strings.Contains(tc.text, "Mini App") && !strings.Contains(tc.text, "مینی‌اپ") {
			t.Fatalf("%s copy must send them through BotFather's Mini App", tc.lang)
		}
		if !strings.Contains(strings.ToLower(tc.text), strings.ToLower(tc.reply)) {
			t.Fatalf("%s copy must ask for a reply with the same token", tc.lang)
		}
	}
}

func TestAppCodeLeadIsCelebration(t *testing.T) {
	t.Parallel()
	if !strings.Contains(appCodeLead.fa, "هورا") || !strings.Contains(appCodeLead.en, "Hooray") {
		t.Fatal("app-code lead must celebrate success, not repeat the walkthrough")
	}
	if strings.Contains(strings.ToLower(appCodeLead.en), "paste") || strings.Contains(appCodeLead.fa, "بچسبون") {
		t.Fatal("success copy must send them back to the app, not paste a code")
	}
}

func TestReconnectIntroAsksForAReply(t *testing.T) {
	t.Parallel()
	if !strings.Contains(strings.ToLower(reconnectIntro.en), "reply") {
		t.Fatal("english reconnect must ask for a reply")
	}
	if !strings.Contains(reconnectIntro.fa, "ریپلای") {
		t.Fatal("persian reconnect must ask for a reply")
	}
}

func TestHomeKeyboardHasNoAppCodeGlass(t *testing.T) {
	t.Parallel()

	kb := homeKeyboard("fa")
	for _, row := range kb.Rows {
		for _, btn := range row {
			if btn.Data == cbMenuApp || btn.Data == cbMenuLogin {
				t.Fatal("app-code and login are not home glass buttons")
			}
		}
	}
	if len(kb.Rows) != 4 {
		t.Fatalf("rows = %d, want 4 (Download, then two pairs, then Support)", len(kb.Rows))
	}
	want := [][]string{
		{cbMenuDownload},
		{cbMenuSettings, cbMenuHelp},
		{cbSetAiKey, cbMenuBot},
		{cbMenuSupport},
	}
	for i, row := range want {
		if len(kb.Rows[i]) != len(row) {
			t.Fatalf("row %d holds %d buttons, want %d", i, len(kb.Rows[i]), len(row))
		}
		for j, data := range row {
			if kb.Rows[i][j].Data != data {
				t.Fatalf("row %d col %d data = %q, want %q", i, j, kb.Rows[i][j].Data, data)
			}
		}
	}
}

func TestMenuCommandsForAddsStaffEntries(t *testing.T) {
	t.Parallel()

	plain := menuCommandsFor("en", identity.RoleUser)
	for _, c := range plain {
		if c.Command == "admin" || c.Command == "index" {
			t.Fatal("non-admin command drawer must not list staff commands")
		}
	}
	if len(plain) != 6 {
		t.Fatalf("user commands = %d, want 6", len(plain))
	}
	if !commandNamed(plain, "download") {
		t.Fatal("user drawer must include /download")
	}

	admin := menuCommandsFor("en", identity.RoleAdmin)
	if !commandNamed(admin, "admin") {
		t.Fatal("admin drawer must include /admin")
	}
	if commandNamed(admin, "index") {
		t.Fatal("command drawer must not list /index — Index lives inside /admin")
	}
	for _, c := range admin {
		if c.Command == "admin" && c.Description != "Admin" {
			t.Fatalf("/admin description = %q, want Admin", c.Description)
		}
	}

	super := menuCommandsFor("fa", identity.RoleSuperadmin)
	if !commandNamed(super, "admin") {
		t.Fatal("superadmin drawer must include /admin")
	}
	if commandNamed(super, "index") {
		t.Fatal("command drawer must not list /index — Index lives inside /admin")
	}
}

func TestLanguageKeyboardMatchesBotLocales(t *testing.T) {
	t.Parallel()

	kb := languageKeyboard()
	var codes []string
	for i, row := range kb.Rows {
		if len(row) == 0 || len(row) > 2 || (i < len(kb.Rows)-1 && len(row) != 2) {
			t.Fatalf("language row %d len = %d", i, len(row))
		}
		for _, btn := range row {
			if !strings.HasPrefix(btn.Data, cbLocalePrefix) {
				t.Fatalf("data %q", btn.Data)
			}
			if !strings.Contains(btn.Text, " ") && !strings.Contains(btn.Text, "فارسی") {
				t.Fatalf("button %q should carry a flag and a native name", btn.Text)
			}
			codes = append(codes, strings.TrimPrefix(btn.Data, cbLocalePrefix))
		}
	}
	if strings.Join(codes, ",") != strings.Join(onboarding.BotLocales, ",") {
		t.Fatalf("codes %v, want %v", codes, onboarding.BotLocales)
	}
	if !strings.Contains(askLanguage.pick(""), "only for chatting with this bot") {
		t.Fatal("language prompt must say the choice is only for this bot")
	}
	if welcome.pick("de") == welcome.en || welcome.pick("tr") == welcome.en || welcome.pick("ar") == "" {
		t.Fatal("welcome must exist in the new bot languages")
	}
	if got := (message{en: "hello", fa: "سلام"}).pick("fr"); got != "hello" {
		t.Fatalf("missing translation = %q, want english", got)
	}
}

func TestBareStartNeedsLanguage(t *testing.T) {
	t.Parallel()
	if skipsLanguageGate(inbound{cmd: "start"}) {
		t.Fatal("bare /start must ask for a bot language")
	}
	if !skipsLanguageGate(inbound{cmd: "start", rest: "login"}) {
		t.Fatal("/start login still runs the bind path")
	}
	if !skipsLanguageGate(inbound{cmd: "login"}) {
		t.Fatal("/login must stay reachable before a language exists")
	}
	if skipsLanguageGate(inbound{cmd: "help"}) {
		t.Fatal("/help before a language must show the picker")
	}
}

func commandNamed(cmds []tg.BotCommand, name string) bool {
	for _, c := range cmds {
		if c.Command == name {
			return true
		}
	}
	return false
}
