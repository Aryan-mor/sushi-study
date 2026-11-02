package main

import (
	"fmt"
	"html"
	"strings"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
)

func TestSupportKeyboardOpensTheSupportBot(t *testing.T) {
	t.Parallel()

	kb := supportKeyboard("en", "exampleSupportBot")
	if len(kb.Rows) < 1 || kb.Rows[0][0].URL != "https://t.me/exampleSupportBot" {
		t.Fatalf("support URL = %+v", kb.Rows)
	}
	if kb.Rows[0][0].Data != "" {
		t.Fatal("support open must be a URL button, not a callback")
	}
	if strings.Contains(kb.Rows[0][0].Text, "exampleSupportBot") || strings.Contains(kb.Rows[0][0].Text, "@") {
		t.Fatalf("label = %q, must not show the handle", kb.Rows[0][0].Text)
	}
	if kb.Rows[0][0].Text != supportOpenBtn.en {
		t.Fatalf("label = %q, want %q", kb.Rows[0][0].Text, supportOpenBtn.en)
	}
}

func TestSupportCopyPointsAtTheHandleNotThisChat(t *testing.T) {
	t.Parallel()

	blob := supportBody.en + supportBody.fa
	for _, word := range []string{"reply", "Reply", "ریپلای", "write here", "this chat"} {
		if strings.Contains(blob, word) {
			t.Fatalf("support copy must not ask them to write here: %q", word)
		}
	}
	if !strings.Contains(blob, "@%s") {
		t.Fatal("support copy must name the support bot handle")
	}
}

func TestSupportCopyHasNoArchitectureNouns(t *testing.T) {
	t.Parallel()

	blob := supportBody.en + supportBody.fa
	for _, word := range []string{"binding", "assignment", "API bot", "pool", "init-bot"} {
		if strings.Contains(blob, word) {
			t.Fatalf("support copy must not say %q", word)
		}
	}
}

func TestBotStatusKeyboardHasTestReplaceRemove(t *testing.T) {
	t.Parallel()

	kb := botStatusKeyboard("en")
	want := []string{cbBotTest, cbBotReplace, cbBotRemove, cbMenuHome}
	if len(kb.Rows) != len(want) {
		t.Fatalf("rows = %d, want %d", len(kb.Rows), len(want))
	}
	for i, data := range want {
		if kb.Rows[i][0].Data != data {
			t.Fatalf("row %d data = %q, want %q", i, kb.Rows[i][0].Data, data)
		}
	}
}

func TestBotStatusCopyDoesNotShowAToken(t *testing.T) {
	t.Parallel()

	blob := botStatusTitle.en + botStatusTitle.fa
	if strings.Contains(blob, "%s%s") || strings.Contains(strings.ToLower(blob), "token:") {
		t.Fatal("status copy must not render the raw token")
	}
}

func TestSettingsKeyboardHasLanguage(t *testing.T) {
	t.Parallel()

	kb := settingsKeyboard("en")
	if !rowsContain(kb.Rows, cbMenuLanguage) {
		t.Fatal("settings must include language")
	}
	if rowsContain(kb.Rows, cbSetQualityOpen) || rowsContain(kb.Rows, cbSetAudioOpen) {
		t.Fatal("quality/audio moved to the app taste wizard")
	}
	if rowsContain(kb.Rows, cbSetAiKey) || rowsContain(kb.Rows, cbAiKeyRemove) {
		t.Fatal("gemini key lives on its own panel, not in settings")
	}
	if rowsContain(kb.Rows, cbMenuLogin) {
		t.Fatal("settings must not include login")
	}
}

func TestAiKeyPanelKeyboards(t *testing.T) {
	t.Parallel()

	empty := aiKeyEmptyKeyboard("en")
	if !rowsContain(empty.Rows, cbAiKeyReplace) || !rowsContain(empty.Rows, cbMenuHome) {
		t.Fatal("empty gemini panel must offer add + home")
	}
	if rowsContain(empty.Rows, cbAiKeyRemove) {
		t.Fatal("empty gemini panel must not offer remove")
	}

	set := aiKeyStatusKeyboard("en")
	want := []string{cbAiKeyReplace, cbAiKeyRemove, cbMenuHome}
	if len(set.Rows) != len(want) {
		t.Fatalf("set panel rows = %d, want %d", len(set.Rows), len(want))
	}
	for i, data := range want {
		if set.Rows[i][0].Data != data {
			t.Fatalf("set panel row %d data = %q, want %q", i, set.Rows[i][0].Data, data)
		}
	}
}

func TestAiKeyStatusCopyDoesNotShowAKey(t *testing.T) {
	t.Parallel()

	blob := aiKeyStatusSet.en + aiKeyStatusSet.fa + aiKeyStatusNone.en + aiKeyStatusNone.fa
	for _, needle := range []string{"AIza", "AQ.", "api key:", "API key:"} {
		if strings.Contains(blob, needle) {
			t.Fatalf("status copy must not show a key fragment %q", needle)
		}
	}
}

// The handle lands in a parse_mode=HTML body. Telegram rejects a message whose entities will not
// parse, so an unescaped '&' in a mistyped SUPPORT_BOT_USERNAME took the whole Support screen down
// rather than showing a wrong name (R-SEC-10).
func TestSupportBodyEscapesTheHandleForHTML(t *testing.T) {
	t.Parallel()

	body := fmt.Sprintf(supportBody.en, html.EscapeString("Rick&Morty<Bot"))
	if strings.Contains(body, "&Morty") || strings.Contains(body, "<Bot") {
		t.Fatalf("handle reached the HTML body unescaped: %q", body)
	}
	if !strings.Contains(body, "&amp;Morty&lt;Bot") {
		t.Fatalf("handle should be escaped: %q", body)
	}
}

// Styles are how a screen says what a button is. Destructive actions must be red, so nobody taps
// "delete my account" thinking it is the way back.
func TestDestructiveButtonsAreRedAndTheWayOutIsNot(t *testing.T) {
	t.Parallel()

	kb := supportKeyboard("en", "exampleSupportBot")
	for _, row := range kb.Rows {
		for _, btn := range row {
			switch btn.Data {
			case cbMenuDelete:
				if btn.Style != tg.StyleDanger {
					t.Errorf("delete account style = %q, want danger", btn.Style)
				}
			case cbMenuHome:
				if btn.Style != "" {
					t.Errorf("Home style = %q, want the default tint", btn.Style)
				}
			}
		}
	}
	if got := deleteFinalKeyboard("en").Rows[0][0].Style; got != tg.StyleDanger {
		t.Errorf("final delete confirmation style = %q, want danger", got)
	}
	if got := botRemoveConfirmKeyboard("en").Rows[0][0].Style; got != tg.StyleDanger {
		t.Errorf("bot removal confirmation style = %q, want danger", got)
	}
	if got := aiKeyRemoveConfirmKeyboard("en").Rows[0][0].Style; got != tg.StyleDanger {
		t.Errorf("gemini key removal confirmation style = %q, want danger", got)
	}
}

func TestSupportKeyboardHasDeleteAccount(t *testing.T) {
	t.Parallel()

	kb := supportKeyboard("fa", "exampleSupportBot")
	found := false
	for _, row := range kb.Rows {
		for _, btn := range row {
			if btn.Data == cbMenuDelete {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("support screen must still offer delete account")
	}
}
