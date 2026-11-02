package main

import (
	"strings"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/adminreq"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
)

func rowsContain(rows [][]tg.InlineButton, data string) bool {
	for _, row := range rows {
		for _, btn := range row {
			if btn.Data == data {
				return true
			}
		}
	}
	return false
}

func TestHomeKeyboardAddsAdminForStaffOnly(t *testing.T) {
	t.Parallel()

	plain := homeKeyboardFor("fa", identity.RoleUser)
	for _, row := range plain.Rows {
		for _, btn := range row {
			if btn.Data == cbMenuAdmin || btn.Data == cbMenuRequests {
				t.Fatal("non-admin should see no staff panel buttons")
			}
		}
	}
	if len(plain.Rows) != 4 {
		t.Fatalf("non-admin rows = %d, want 4 (Download, then pairs)", len(plain.Rows))
	}

	admin := homeKeyboardFor("en", identity.RoleAdmin)
	if !rowsContain(admin.Rows, cbMenuAdmin) {
		t.Fatal("admin should see the Admin hub button")
	}
	if rowsContain(admin.Rows, cbMenuRequests) {
		t.Fatal("Requests live inside /admin, not on Home")
	}
	if rowsContain(admin.Rows, cbMenuLogin) {
		t.Fatal("login is not a home glass button")
	}
	if len(admin.Rows) != 4 {
		t.Fatalf("admin rows = %d, want 4 (Admin shares the Support row)", len(admin.Rows))
	}

	super := homeKeyboardFor("en", identity.RoleSuperadmin)
	if !rowsContain(super.Rows, cbMenuAdmin) {
		t.Fatal("superadmin should see the Admin hub button")
	}
	if rowsContain(super.Rows, cbMenuRequests) {
		t.Fatal("Requests live inside /admin, not on Home")
	}
	if len(super.Rows) != 4 {
		t.Fatalf("superadmin rows = %d, want 4 (Admin shares the Support row)", len(super.Rows))
	}
}

// Download leads Home on its own row, tinted: it is the one action the screen exists for.
func TestHomeKeyboardLedByATintedDownload(t *testing.T) {
	t.Parallel()

	for _, role := range []string{identity.RoleUser, identity.RoleSuperadmin} {
		first := homeKeyboardFor("en", role).Rows[0]
		if len(first) != 1 || first[0].Data != cbMenuDownload {
			t.Fatalf("role %s: first row = %v, want Download alone", role, first)
		}
		if first[0].Style != tg.StylePrimary {
			t.Errorf("role %s: Download style = %q, want primary", role, first[0].Style)
		}
	}
}

// Home is a grid, not a column: every row after Download carries two buttons, except Support when
// there is no staff hub to pair it with.
func TestHomeKeyboardPairsUpTheRestOfTheRows(t *testing.T) {
	t.Parallel()

	for _, role := range []string{identity.RoleUser, identity.RoleSuperadmin} {
		for i, row := range homeKeyboardFor("en", role).Rows[1:] {
			switch {
			case len(row) == 2:
			case len(row) == 1 && row[0].Data == cbMenuSupport:
			default:
				t.Errorf("role %s: row %d = %v, want a pair (or Support alone)", role, i+1, row)
			}
		}
	}
}

func TestIsAdmin(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{"user": false, "admin": true, "superadmin": true, "": false, "Admin": false}
	for role, want := range cases {
		if got := isAdmin(role); got != want {
			t.Errorf("isAdmin(%q) = %v, want %v", role, got, want)
		}
	}
}

func TestRequesterShort(t *testing.T) {
	t.Parallel()

	// username wins and links, no id
	got := requesterShort(adminreq.Request{RequesterName: "Ali", RequesterUsername: "@ali", RequesterTG: 42})
	if got != `<a href="https://t.me/ali">@ali</a>` {
		t.Errorf("with username = %q", got)
	}
	// no username -> name, still no id
	got = requesterShort(adminreq.Request{RequesterName: "Ali <b>", RequesterTG: 42})
	if got != "Ali &lt;b&gt;" {
		t.Errorf("name only = %q", got)
	}
	// nothing -> id as last resort
	got = requesterShort(adminreq.Request{RequesterTG: 42})
	if got != "<code>42</code>" {
		t.Errorf("id fallback = %q", got)
	}
	// the id never appears when a handle is present
	got = requesterShort(adminreq.Request{RequesterUsername: "ali", RequesterTG: 42})
	if strings.Contains(got, "42") {
		t.Errorf("id leaked into %q", got)
	}
}

func TestRequestAlertKeyboardHasRejectAndRecheck(t *testing.T) {
	t.Parallel()

	kb := requestAlertKeyboard(812, "fa")
	if !rowsContain(kb.Rows, cbReqAlertRejectPrefix+"812") {
		t.Error("alert keyboard missing reject")
	}
	if !rowsContain(kb.Rows, cbReqAlertRecheckPrefix+"812") {
		t.Error("alert keyboard missing re-check")
	}
	for _, row := range kb.Rows {
		for _, btn := range row {
			if n := len(btn.Data); n > 64 {
				t.Errorf("%q callback data is %d bytes, over Telegram's 64", btn.Text, n)
			}
		}
	}
}

func TestParsePageID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in       string
		wantPage int
		wantID   int64
	}{
		{"3:812", 3, 812},
		{"0:1", 0, 1},
		{"7", 0, 0},   // no colon
		{"2:", 2, 0},  // no id
		{"x:5", 0, 5}, // bad page
		{"1:notanid", 1, 0},
		{"-4:9", 0, 9}, // negative page clamps to 0
	}
	for _, tc := range cases {
		p, id := parsePageID(tc.in)
		if p != tc.wantPage || id != tc.wantID {
			t.Errorf("parsePageID(%q) = (%d, %d), want (%d, %d)", tc.in, p, id, tc.wantPage, tc.wantID)
		}
	}
}
