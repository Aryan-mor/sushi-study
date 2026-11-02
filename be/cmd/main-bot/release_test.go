package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/platform/config"
)

type fakeNotices struct {
	fresh   bool
	err     error
	service string
	tag     string
	calls   int
}

func (f *fakeNotices) ClaimDeployNotice(_ context.Context, service, tag string) (bool, error) {
	f.calls++
	f.service, f.tag = service, tag
	return f.fresh, f.err
}

type fakeAdminNotifier struct{ sent []string }

func (f *fakeAdminNotifier) NotifyAdmins(_ context.Context, text string) {
	f.sent = append(f.sent, text)
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestAnnounceDeployTellsSuperadminsOnce(t *testing.T) {
	t.Parallel()
	notices := &fakeNotices{fresh: true}
	notify := &fakeAdminNotifier{}

	announceDeploy(context.Background(), config.EnvProd, "abc123def456", notices, notify, quietLogger())

	if notices.service != deployNoticeService || notices.tag != "abc123def456" {
		t.Fatalf("claimed %q/%q, want %q/abc123def456", notices.service, notices.tag, deployNoticeService)
	}
	if len(notify.sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(notify.sent))
	}
	if !strings.Contains(notify.sent[0], "abc123def456") {
		t.Errorf("message does not name the release: %q", notify.sent[0])
	}
	if !strings.Contains(notify.sent[0], string(config.EnvProd)) {
		t.Errorf("message does not name the env: %q", notify.sent[0])
	}
}

// A restart on the same image is not a release. The claim says so, and nobody is DMed.
func TestAnnounceDeployStaysQuietOnRestart(t *testing.T) {
	t.Parallel()
	notices := &fakeNotices{fresh: false}
	notify := &fakeAdminNotifier{}

	announceDeploy(context.Background(), config.EnvProd, "abc123def456", notices, notify, quietLogger())

	if len(notify.sent) != 0 {
		t.Fatalf("sent %v, want nothing on a repeat tag", notify.sent)
	}
}

// A dev checkout has no SUSHI_IMAGE_TAG: there is no deploy to announce, and the DB is never touched.
func TestAnnounceDeployIgnoresEmptyTag(t *testing.T) {
	t.Parallel()
	notices := &fakeNotices{fresh: true}
	notify := &fakeAdminNotifier{}

	announceDeploy(context.Background(), config.EnvDev, "", notices, notify, quietLogger())

	if notices.calls != 0 {
		t.Errorf("claimed a notice for an empty tag (%d calls)", notices.calls)
	}
	if len(notify.sent) != 0 {
		t.Errorf("sent %v, want nothing without a tag", notify.sent)
	}
}

// The notice is best-effort: a broken claim is logged, not announced, and never returned.
func TestAnnounceDeploySwallowsClaimFailure(t *testing.T) {
	t.Parallel()
	notices := &fakeNotices{err: errors.New("no such table")}
	notify := &fakeAdminNotifier{}

	announceDeploy(context.Background(), config.EnvProd, "abc123def456", notices, notify, quietLogger())

	if len(notify.sent) != 0 {
		t.Fatalf("sent %v, want nothing when the claim failed", notify.sent)
	}
}
