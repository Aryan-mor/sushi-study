package main

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
)

type recordingNotify struct{ texts []string }

func (r *recordingNotify) NotifyAdmins(_ context.Context, text string) {
	r.texts = append(r.texts, text)
}

func TestRouteStatsCallbackIgnoresOtherData(t *testing.T) {
	t.Parallel()

	b := &bot{}
	q := &tg.CallbackQuery{Data: cbMenuAdmin}
	if b.routeStatsCallback(context.Background(), q, screen{}, identity.User{Role: identity.RoleSuperadmin}, "en", slog.Default()) {
		t.Fatal("must not own the admin hub")
	}
}

func TestNoteNewUserSkipsStaffAndRepeats(t *testing.T) {
	t.Parallel()

	n := &recordingNotify{}
	b := &bot{notify: n, log: slog.Default()}

	b.noteNewUser(context.Background(), identity.User{Created: true, Role: identity.RoleSuperadmin, TGUserID: 1})
	b.noteNewUser(context.Background(), identity.User{Created: false, Role: identity.RoleUser, TGUserID: 2, Name: "x"})
	b.noteNewUser(context.Background(), identity.User{Created: true, Deleted: true, Role: identity.RoleUser, TGUserID: 3})
	if len(n.texts) != 0 {
		t.Fatalf("sent %v, want none", n.texts)
	}

	b.noteNewUser(context.Background(), identity.User{Created: true, Role: identity.RoleUser, TGUserID: 9, Name: "Ada", Username: "lovelace"})
	if len(n.texts) != 1 || !strings.Contains(n.texts[0], "id: 9") {
		t.Fatalf("got %v", n.texts)
	}
}
