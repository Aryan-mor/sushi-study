// Package channels parses Sushi's channel set: where files live, and in what order they are
// written.
//
//	CHANNELS=clone|-100…,backup1|-100…,backup2|-100…,backup3|-100…
//
// One packed secret rather than four (docs/08 §2.1), for the same reason the bot pools are one:
// adding a fourth backup should be one edit, not a new key plus code that knows to look for it.
// Fields are joined with '|', same as every other packed secret: a colon cannot be the separator
// once bot tokens (which contain a colon) share the format.
package channels

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Aryan-mor/sushi-study/be/internal/platform/pairs"
)

// Role is what a channel is for.
type Role string

const (
	// RoleClone is the channel delivery reads from. Exactly one.
	RoleClone Role = "clone"
	// RoleBackup is a copy that exists so a lost clone is recoverable. Any number, including none.
	RoleBackup Role = "backup"
	// RoleNews is where main-bot announces newly playable titles (ADR 0017). At most one, and
	// optional: a deployment without it queues nothing and posts nothing.
	RoleNews Role = "news"
	// RoleRequests is the staff content-request inbox (ADR 0023). At most one, optional: without
	// it, new-request alerts still DM each admin.
	RoleRequests Role = "requests"
	// RolePersonalClone holds user-uploaded personal videos; main-bot copies an upload in, delivery
	// copies it out (ADR 0037). At most one, optional: without it main-bot takes no uploads.
	RolePersonalClone Role = "personal_clone"
	// RolePersonalBackup is the personal clone's backup; backupd fills it with the backup1 bot
	// (ADR 0037). At most one, optional.
	RolePersonalBackup Role = "personal_backup"
)

// Channel is one destination.
type Channel struct {
	// Name is the key from the secret: "clone", "backup1", … It is what file_locations records, so
	// renaming one in the secret renames it in the database too — pick names once.
	Name Role
	ID   int64
}

// Set is the parsed channel configuration.
type Set struct {
	clone    Channel
	backups  []Channel
	news     Channel
	requests Channel

	personalClone  Channel
	personalBackup Channel
}

// Parse reads the packed channel secret.
//
// Exactly one clone is required: it is where delivery copies from, so a set without one is a
// configuration that cannot serve video, and saying so at boot beats finding out at play time.
func Parse(secretName, raw string) (*Set, error) {
	entries, err := pairs.Parser{FieldNames: []string{"role", "channel id"}}.Parse(secretName, raw)
	if err != nil {
		return nil, err
	}

	set := &Set{}
	for i, e := range entries {
		id, err := strconv.ParseInt(e[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s: entry %d (%s) is not a channel id: %w", secretName, i+1, e.Key(), err)
		}
		// Telegram channel ids are negative and start with -100. A positive id is a user or a
		// group, and posting media there would be a different mistake entirely.
		if id >= 0 {
			return nil, fmt.Errorf("%s: entry %d (%s) has id %d; channel ids are negative", secretName, i+1, e.Key(), id)
		}

		name := strings.ToLower(e.Key())
		ch := Channel{Name: Role(name), ID: id}
		if err := set.add(secretName, i+1, ch, name); err != nil {
			return nil, err
		}
	}

	if set.clone.ID == 0 {
		return nil, fmt.Errorf("%s: no clone channel; delivery would have nothing to copy from", secretName)
	}
	return set, nil
}

func (set *Set) add(secretName string, index int, ch Channel, name string) error {
	switch {
	case name == string(RoleClone):
		if set.clone.ID != 0 {
			return fmt.Errorf("%s: more than one clone channel", secretName)
		}
		set.clone = ch
	case name == string(RoleNews):
		if set.news.ID != 0 {
			return fmt.Errorf("%s: more than one news channel", secretName)
		}
		set.news = ch
	case name == string(RoleRequests):
		if set.requests.ID != 0 {
			return fmt.Errorf("%s: more than one requests channel", secretName)
		}
		set.requests = ch
	case name == string(RolePersonalClone):
		if set.personalClone.ID != 0 {
			return fmt.Errorf("%s: more than one personal_clone channel", secretName)
		}
		set.personalClone = ch
	case name == string(RolePersonalBackup):
		if set.personalBackup.ID != 0 {
			return fmt.Errorf("%s: more than one personal_backup channel", secretName)
		}
		set.personalBackup = ch
	case strings.HasPrefix(name, string(RoleBackup)):
		set.backups = append(set.backups, ch)
	default:
		return fmt.Errorf("%s: entry %d has unknown role %q; expected clone, backupN, news, requests, personal_clone or personal_backup", secretName, index, name)
	}
	return nil
}

// Clone is the channel delivery reads from.
func (s *Set) Clone() Channel { return s.clone }

// Backups are the extra copies, in the order they appear in the secret.
func (s *Set) Backups() []Channel {
	out := make([]Channel, len(s.backups))
	copy(out, s.backups)
	return out
}

// News is the announce channel, and whether CHANNELS named one at all (ADR 0017). Unlike the
// clone, its absence is not a configuration error: main-bot simply announces nothing.
func (s *Set) News() (Channel, bool) {
	return s.news, s.news.ID != 0
}

// Requests is the staff request inbox, and whether CHANNELS named one (ADR 0023). Same optional
// shape as News: absence is not a boot error.
func (s *Set) Requests() (Channel, bool) {
	return s.requests, s.requests.ID != 0
}

// PersonalClone is where personal videos live, and whether CHANNELS named one (ADR 0037).
func (s *Set) PersonalClone() (Channel, bool) {
	return s.personalClone, s.personalClone.ID != 0
}

// PersonalBackup is the personal clone's backup, and whether CHANNELS named one (ADR 0037).
func (s *Set) PersonalBackup() (Channel, bool) {
	return s.personalBackup, s.personalBackup.ID != 0
}

// All returns the clone first, then the backups: the order a file is written in, so the copy that
// matters is in place before the ones that only matter later. News, requests and the personal
// channels are deliberately not here — no catalog file is ever copied into them, and every catalog
// writer (indexerd, the reader, the BACKUP_BOTS binding) walks this list.
func (s *Set) All() []Channel {
	return append([]Channel{s.clone}, s.backups...)
}

// String renders the set for logs.
func (s *Set) String() string {
	names := make([]string, 0, len(s.backups)+1)
	for _, c := range s.All() {
		names = append(names, string(c.Name))
	}
	return strings.Join(names, " ")
}
