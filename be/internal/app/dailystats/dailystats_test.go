package dailystats

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestFormatEnglishAndPersian(t *testing.T) {
	t.Parallel()

	s := Snapshot{Online24h: 12, Registered: 3400, Delivered24h: 88, Indexed: 12345}
	en := Format(s, "en", "2026-09-11")
	if !strings.Contains(en, "Daily stats — 2026-09-11") {
		t.Fatalf("en header: %q", en)
	}
	if !strings.Contains(en, "Online (24h): 12") || !strings.Contains(en, "Registered: 3 400") {
		t.Fatalf("en body: %q", en)
	}
	if !strings.Contains(en, "Videos delivered (24h): 88") || !strings.Contains(en, "Indexed videos: 12 345") {
		t.Fatalf("en metrics: %q", en)
	}

	fa := Format(s, "fa", "2026-09-11")
	if !strings.Contains(fa, "آمار روزانه") || !strings.Contains(fa, "ثبت‌نام‌شده: 3 400") {
		t.Fatalf("fa: %q", fa)
	}
}

func TestNewUserNoticeOmitsEmptyDisplayFields(t *testing.T) {
	t.Parallel()

	got := NewUserNotice(99, "", "")
	if got != "👤 New user\nid: 99" {
		t.Fatalf("got %q", got)
	}
	with := NewUserNotice(1, "Ada", "lovelace")
	if !strings.Contains(with, "name: Ada") || !strings.Contains(with, "@lovelace") {
		t.Fatalf("got %q", with)
	}
}

func TestYMDLocal(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 9, 11, 15, 4, 0, 0, time.Local)
	if YMDLocal(ts) != "2026-09-11" {
		t.Fatalf("ymd %s", YMDLocal(ts))
	}
}

type fakeStore struct {
	snap      Snapshot
	snapErr   error
	claimed   []string
	claimErr  error
	claimOnce map[string]bool
}

func (f *fakeStore) DailyStats(context.Context) (Snapshot, error) {
	return f.snap, f.snapErr
}

func (f *fakeStore) ClaimDailyStatsSend(_ context.Context, ymd string) (bool, error) {
	if f.claimErr != nil {
		return false, f.claimErr
	}
	if f.claimOnce == nil {
		f.claimOnce = map[string]bool{}
	}
	if f.claimOnce[ymd] {
		return false, nil
	}
	f.claimOnce[ymd] = true
	f.claimed = append(f.claimed, ymd)
	return true, nil
}

type fakeNotify struct{ texts []string }

func (f *fakeNotify) NotifyAdmins(_ context.Context, text string) {
	f.texts = append(f.texts, text)
}

func TestDigestSendsOncePerYMD(t *testing.T) {
	t.Parallel()

	store := &fakeStore{snap: Snapshot{Registered: 7}}
	n := &fakeNotify{}
	d := New(store, n, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.now = func() time.Time { return time.Date(2026, 9, 11, 8, 0, 0, 0, time.Local) }

	d.tick(t.Context())
	d.tick(t.Context())

	if len(store.claimed) != 1 || store.claimed[0] != "2026-09-11" {
		t.Fatalf("claimed %v", store.claimed)
	}
	if len(n.texts) != 1 {
		t.Fatalf("sent %d, want 1", len(n.texts))
	}
	if !strings.Contains(n.texts[0], "Registered: 7") {
		t.Fatalf("body %q", n.texts[0])
	}
}

func TestDigestSkipsSendWhenClaimFails(t *testing.T) {
	t.Parallel()

	n := &fakeNotify{}
	d := New(&fakeStore{claimErr: errors.New("db down")}, n, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.tick(t.Context())
	if len(n.texts) != 0 {
		t.Fatal("must not DM when the claim failed")
	}
}

func TestDigestRunNilStoreWaitsForCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	d := New(nil, &fakeNotify{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
