package channels_test

import (
	"strings"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/platform/channels"
)

const raw = "clone|-1009900000009,backup1|-1009900000007,backup2|-1009900000008"

func TestParse(t *testing.T) {
	t.Parallel()

	set, err := channels.Parse("CHANNELS", raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := set.Clone().ID; got != -1009900000009 {
		t.Errorf("clone = %d", got)
	}
	if n := len(set.Backups()); n != 2 {
		t.Errorf("backups = %d, want 2", n)
	}
	// The clone is written first: the copy delivery reads from should exist before the ones that
	// only matter if it is lost.
	if all := set.All(); all[0].Name != channels.RoleClone {
		t.Errorf("All()[0] = %q, want the clone first", all[0].Name)
	}
	// No news role in this CHANNELS, which is not an error: main-bot just announces nothing
	// (ADR 0017 §2).
	if _, ok := set.News(); ok {
		t.Error("News() reported a channel for a CHANNELS that names none")
	}
	if _, ok := set.Requests(); ok {
		t.Error("Requests() reported a channel for a CHANNELS that names none")
	}
}

func TestParseReadsTheNewsChannel(t *testing.T) {
	t.Parallel()

	set, err := channels.Parse("CHANNELS", raw+",news|-1009900000010")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	ch, ok := set.News()
	if !ok {
		t.Fatal("News() = not found, want the channel CHANNELS names")
	}
	if ch.ID != -1009900000010 {
		t.Errorf("news id = %d", ch.ID)
	}
	if ch.Name != channels.RoleNews {
		t.Errorf("news role = %q, want %q", ch.Name, channels.RoleNews)
	}
	// The news channel is not a file destination. If it leaked into All() or Backups(), backupd
	// would bind a bot to it and fetcherd would copy video into the announce feed.
	if n := len(set.Backups()); n != 2 {
		t.Errorf("backups = %d, want the news channel left out", n)
	}
	for _, c := range set.All() {
		if c.Name == channels.RoleNews {
			t.Error("All() includes the news channel; nothing is ever copied into it")
		}
	}
}

func TestParseReadsTheRequestsChannel(t *testing.T) {
	t.Parallel()

	set, err := channels.Parse("CHANNELS", raw+",requests|-1009900000002")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	ch, ok := set.Requests()
	if !ok {
		t.Fatal("Requests() = not found, want the channel CHANNELS names")
	}
	if ch.ID != -1009900000002 {
		t.Errorf("requests id = %d", ch.ID)
	}
	if ch.Name != channels.RoleRequests {
		t.Errorf("requests role = %q, want %q", ch.Name, channels.RoleRequests)
	}
	for _, c := range set.All() {
		if c.Name == channels.RoleRequests {
			t.Error("All() includes the requests channel; nothing is ever copied into it")
		}
	}
}

func TestParseRejects(t *testing.T) {
	t.Parallel()

	cases := map[string]struct{ raw, want string }{
		"no clone":     {"backup1|-100123", "no clone channel"},
		"two clones":   {"clone|-100123,clone|-100456", "appears twice"},
		"positive id":  {"clone|100123", "channel ids are negative"},
		"not a number": {"clone|not-an-id", "is not a channel id"},
		"unknown role": {"clone|-100123,archive|-100456", "unknown role"},
		"two news":     {"clone|-100123,news|-100456,news|-100789", "appears twice"},
		"two requests": {"clone|-100123,requests|-100456,requests|-100789", "appears twice"},
		"empty":        {"", "no entries"},
		"missing pipe": {"clone", "no \"|\" separator"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := channels.Parse("CHANNELS", tc.raw)
			if err == nil {
				t.Fatalf("Parse(%q) succeeded, want an error", tc.raw)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestParseReadsThePersonalChannels(t *testing.T) {
	t.Parallel()

	set, err := channels.Parse("CHANNELS", raw+",personal_clone|-1009900000004,personal_backup|-1009900000011")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pc, ok := set.PersonalClone()
	if !ok || pc.ID != -1009900000004 || pc.Name != channels.RolePersonalClone {
		t.Errorf("PersonalClone() = %+v, %v", pc, ok)
	}
	pb, ok := set.PersonalBackup()
	if !ok || pb.ID != -1009900000011 || pb.Name != channels.RolePersonalBackup {
		t.Errorf("PersonalBackup() = %+v, %v", pb, ok)
	}
	// Every catalog writer walks All() and Backups(). A personal channel in either would have
	// backupd copy the whole catalog into it (ADR 0037 §2).
	if n := len(set.Backups()); n != 2 {
		t.Errorf("backups = %d, want the personal channels left out", n)
	}
	for _, c := range set.All() {
		if c.Name == channels.RolePersonalClone || c.Name == channels.RolePersonalBackup {
			t.Errorf("All() carries %q", c.Name)
		}
	}
}

func TestParseWithoutPersonalChannels(t *testing.T) {
	t.Parallel()

	set, err := channels.Parse("CHANNELS", raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, ok := set.PersonalClone(); ok {
		t.Error("PersonalClone() reported a channel for a CHANNELS that names none")
	}
	if _, ok := set.PersonalBackup(); ok {
		t.Error("PersonalBackup() reported a channel for a CHANNELS that names none")
	}
}

func TestParseRejectsTwoPersonalClones(t *testing.T) {
	t.Parallel()

	_, err := channels.Parse("CHANNELS", raw+",personal_clone|-1009900000004,personal_clone|-1009900000012")
	if err == nil || !strings.Contains(err.Error(), "personal_clone") {
		t.Fatalf("err = %v, want a duplicate personal_clone error", err)
	}
}
