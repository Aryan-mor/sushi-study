package main

import (
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
)

func TestIsSuperadmin(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		identity.RoleSuperadmin: true,
		identity.RoleAdmin:      false,
		identity.RoleUser:       false,
		"":                      false,
		"Superadmin":            false,
	}
	for role, want := range cases {
		if got := isSuperadmin(role); got != want {
			t.Errorf("isSuperadmin(%q) = %v, want %v", role, got, want)
		}
	}
}

func TestAdminKeyboardShowsRequestsAndIndexByRole(t *testing.T) {
	t.Parallel()

	admin := adminKeyboardFor("en")
	if !rowsContain(admin.Rows, cbMenuRequests) {
		t.Fatal("admin hub must include Requests")
	}
	if !rowsContain(admin.Rows, cbMenuActions) {
		t.Fatal("admin hub must include Actions")
	}
	if !rowsContain(admin.Rows, cbMenuIDs) {
		t.Fatal("admin hub must include TMDB ↔ IMDb")
	}
	if !rowsContain(admin.Rows, cbMenuNews) {
		t.Fatal("admin hub must include News post")
	}
	if !rowsContain(admin.Rows, cbMenuHome) {
		t.Fatal("admin hub must include Home")
	}
	if len(admin.Rows) != 6 {
		t.Fatalf("admin hub rows = %d, want 6 (requests + news + authorize + ids + actions + home)", len(admin.Rows))
	}

	super := adminKeyboardFor("fa")
	if !rowsContain(super.Rows, cbMenuRequests) || !rowsContain(super.Rows, cbMenuActions) || !rowsContain(super.Rows, cbMenuIDs) {
		t.Fatal("superadmin hub must include Requests, TMDB ↔ IMDb, and Actions")
	}
	if !rowsContain(super.Rows, cbMenuHome) {
		t.Fatal("superadmin hub must include Home")
	}
	if !rowsContain(super.Rows, cbMenuNews) {
		t.Fatal("superadmin hub must include News post")
	}
	if len(super.Rows) != 6 {
		t.Fatalf("superadmin hub rows = %d, want 6 (requests + news + authorize + ids + actions + home)", len(super.Rows))
	}
}

func TestActionsKeyboardShowsStatsForSuperadminOnly(t *testing.T) {
	t.Parallel()

	admin := actionsKeyboardFor("en", identity.RoleAdmin)
	if rowsContain(admin.Rows, cbMenuStats) {
		t.Fatal("plain admin must not see Daily stats")
	}
	if !rowsContain(admin.Rows, cbMenuOnline) {
		t.Fatal("admin Actions must include Online")
	}
	if !rowsContain(admin.Rows, cbMenuAdmin) || !rowsContain(admin.Rows, cbMenuHome) {
		t.Fatal("Actions footer must include Admin + Home")
	}
	if len(admin.Rows) != 2 {
		t.Fatalf("admin Actions rows = %d, want 2 (online + footer)", len(admin.Rows))
	}

	super := actionsKeyboardFor("fa", identity.RoleSuperadmin)
	if !rowsContain(super.Rows, cbMenuOnline) {
		t.Fatal("superadmin Actions must include Online")
	}
	if !rowsContain(super.Rows, cbMenuStats) {
		t.Fatal("superadmin Actions must include Daily stats")
	}
	if !rowsContain(super.Rows, cbMenuAdmin) {
		t.Fatal("Actions footer must include back to Admin")
	}
	if len(super.Rows) != 3 {
		t.Fatalf("superadmin Actions rows = %d, want 3 (online + stats + footer)", len(super.Rows))
	}
}

func TestStaffNavKeyboardGoesBackToAdmin(t *testing.T) {
	t.Parallel()

	kb := staffNavKeyboard("en")
	if !rowsContain(kb.Rows, cbMenuAdmin) {
		t.Fatal("staff panel footer must include back to Admin")
	}
	if !rowsContain(kb.Rows, cbMenuHome) {
		t.Fatal("staff panel footer must include Home")
	}
}

func TestActionsNavKeyboardGoesBackToActions(t *testing.T) {
	t.Parallel()

	kb := actionsNavKeyboard("en")
	if !rowsContain(kb.Rows, cbMenuActions) {
		t.Fatal("Actions job footer must include back to Actions")
	}
	if rowsContain(kb.Rows, cbMenuAdmin) {
		t.Fatal("Actions job footer must not skip to Admin")
	}
	if !rowsContain(kb.Rows, cbMenuHome) {
		t.Fatal("Actions job footer must include Home")
	}
}
