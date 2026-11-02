package tg

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCopyMessageParamsProtectsOnlyWhenAsked(t *testing.T) {
	t.Parallel()

	open := copyMessageParams("-1001", 2, 3, "plm_1", "", false)
	if open.Get("protect_content") != "" {
		t.Fatalf("clone/backup copy must not set protect_content, got %q", open.Get("protect_content"))
	}

	locked := copyMessageParams("42", 2, 3, "plm_1", "", true)
	if locked.Get("protect_content") != "true" {
		t.Fatalf("delivery copy protect_content = %q", locked.Get("protect_content"))
	}
	if locked.Get("caption") != "plm_1" {
		t.Fatalf("caption = %q", locked.Get("caption"))
	}
}

func TestCopyMessageParamsParseMode(t *testing.T) {
	t.Parallel()

	// A caption with markup carries its parse mode.
	html := copyMessageParams("-1001", 2, 3, `<a href="x">tmdb</a>`, "HTML", false)
	if html.Get("parse_mode") != "HTML" {
		t.Fatalf("parse_mode = %q, want HTML", html.Get("parse_mode"))
	}

	// No caption (backupd) -> parse_mode is pointless and must be left off so the copy keeps the
	// original caption untouched.
	none := copyMessageParams("-1001", 2, 3, "", "HTML", false)
	if none.Get("parse_mode") != "" {
		t.Fatalf("parse_mode = %q with no caption, want empty", none.Get("parse_mode"))
	}
}

func TestCopyTextButtonOmitsCallbackData(t *testing.T) {
	t.Parallel()

	kb := InlineKeyboard{Rows: [][]InlineButton{{{
		Text:     "کپی",
		CopyText: &CopyText{Text: "s1.abc"},
	}}}}
	raw, err := json.Marshal(kb)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	rows, _ := got["inline_keyboard"].([]any)
	firstRow, _ := rows[0].([]any)
	btn, _ := firstRow[0].(map[string]any)
	if _, ok := btn["callback_data"]; ok {
		t.Fatalf("copy_text button must omit callback_data: %s", raw)
	}
	ct, _ := btn["copy_text"].(map[string]any)
	if ct["text"] != "s1.abc" {
		t.Fatalf("copy_text = %v", btn["copy_text"])
	}
}

// Telegram reads the tint from a field named "style" and accepts only these three words; anything
// else fails the whole sendMessage, taking the screen with it.
func TestButtonStyleMarshalsToTelegramsField(t *testing.T) {
	t.Parallel()

	kb := InlineKeyboard{Rows: [][]InlineButton{{
		{Text: "Yes, delete", Data: "da:y", Style: StyleDanger},
		{Text: "Cancel", Data: "da:x"},
	}}}
	raw, err := json.Marshal(kb)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"style":"danger"`) {
		t.Fatalf("styled button did not marshal a style field: %s", raw)
	}
	// An unstyled button must stay absent rather than send "", which Telegram would reject as not
	// one of the three accepted values.
	if strings.Contains(string(raw), `"style":""`) {
		t.Fatalf("unstyled button sent an empty style: %s", raw)
	}
	for _, s := range []string{StylePrimary, StyleSuccess, StyleDanger} {
		switch s {
		case "primary", "success", "danger":
		default:
			t.Errorf("style %q is not a value Telegram accepts", s)
		}
	}
}
