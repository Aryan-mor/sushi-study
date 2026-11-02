package main

import (
	"strings"
	"testing"
)

func TestParseTelegramID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		raw  string
		want int64
		ok   bool
	}{
		{raw: "123456789", want: 123456789, ok: true},
		{raw: "  123456789\n", want: 123456789, ok: true},
		{raw: "", ok: false},
		{raw: "0", ok: false},
		{raw: "-100123", ok: false},
		{raw: "@someone", ok: false},
		{raw: "12 34", ok: false},
		{raw: "99999999999999999999", ok: false},
	}
	for _, tc := range tests {
		got, ok := parseTelegramID(tc.raw)
		if ok != tc.ok || got != tc.want {
			t.Errorf("parseTelegramID(%q) = %d, %v; want %d, %v", tc.raw, got, ok, tc.want, tc.ok)
		}
	}
}

func TestAccessKeyboardOffersARevokePerPendingInvite(t *testing.T) {
	t.Parallel()
	kb := accessKeyboard("en", []int64{111, 222})

	var revokes []string
	for _, row := range kb.Rows {
		for _, btn := range row {
			if strings.HasPrefix(btn.Data, cbAccessRevokePfx) {
				revokes = append(revokes, strings.TrimPrefix(btn.Data, cbAccessRevokePfx))
			}
		}
	}
	if len(revokes) != 2 || revokes[0] != "111" || revokes[1] != "222" {
		t.Errorf("revoke buttons = %v", revokes)
	}
	if kb.Rows[0][0].Data != cbAccessAdd {
		t.Errorf("first row = %q, want the add button", kb.Rows[0][0].Data)
	}
}

func TestAccessCallbacksFitTelegramsLimit(t *testing.T) {
	t.Parallel()
	const telegramCallbackLimit = 64
	longest := cbAccessRevokePfx + "9007199254740991"
	if len(longest) > telegramCallbackLimit {
		t.Errorf("%q is %d bytes", longest, len(longest))
	}
}

func TestAdminHubOffersAuthorizeToEveryAdmin(t *testing.T) {
	t.Parallel()
	for _, role := range []string{"admin", "superadmin"} {
		found := false
		for _, row := range adminKeyboardFor("en").Rows {
			for _, btn := range row {
				found = found || btn.Data == cbMenuAccess
			}
		}
		if !found {
			t.Errorf("%s hub has no authorize button", role)
		}
	}
}
