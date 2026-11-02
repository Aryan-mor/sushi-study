package health

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
)

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeStore struct {
	mu              sync.Mutex
	quarantined     map[int32]time.Time
	retired         map[int32]bool
	quarantineFresh bool
	retireFresh     bool
	quarantineCalls int
	retireCalls     int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		quarantined:     map[int32]time.Time{},
		retired:         map[int32]bool{},
		quarantineFresh: true,
		retireFresh:     true,
	}
}

func (f *fakeStore) MarkBotQuarantined(_ context.Context, botID int32, until time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.quarantineCalls++
	f.quarantined[botID] = until
	return f.quarantineFresh, nil
}

func (f *fakeStore) MarkBotRetired(_ context.Context, botID int32) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retireCalls++
	f.retired[botID] = true
	return f.retireFresh, nil
}

type fakeNotifier struct {
	mu    sync.Mutex
	texts []string
}

func (n *fakeNotifier) NotifyAdmins(_ context.Context, text string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.texts = append(n.texts, text)
}

func TestReportIgnoresNonTelegramErrors(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	notifier := &fakeNotifier{}
	r := New(store, notifier, quietLog())

	handled := r.Report(t.Context(), 7, "api", "api1_bot", errors.New("network blip"))
	if handled {
		t.Error("Report handled a plain error; want false")
	}
	if store.retireCalls != 0 || store.quarantineCalls != 0 {
		t.Error("a non-Telegram error must not touch bot state")
	}
	if len(notifier.texts) != 0 {
		t.Error("a non-Telegram error must not notify")
	}
}

func TestReportRetiresOnUnauthorized(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	notifier := &fakeNotifier{}
	r := New(store, notifier, quietLog())

	err := &tg.Error{Code: http.StatusUnauthorized, Description: "Unauthorized"}
	handled := r.Report(t.Context(), 7, "api", "api1_bot", err)
	if !handled {
		t.Fatal("Report did not handle a 401")
	}
	if !store.retired[7] {
		t.Error("bot 7 was not marked retired")
	}
	if len(notifier.texts) != 1 {
		t.Fatalf("notified %d times, want 1", len(notifier.texts))
	}
}

func TestReportDoesNotReNotifyAnAlreadyRetiredBot(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.retireFresh = false // simulates MarkBotRetired's "state != 4" guard finding no rows
	notifier := &fakeNotifier{}
	r := New(store, notifier, quietLog())

	err := &tg.Error{Code: http.StatusUnauthorized, Description: "Unauthorized"}
	r.Report(t.Context(), 7, "api", "api1_bot", err)

	if len(notifier.texts) != 0 {
		t.Errorf("notified %d times for an already-retired bot, want 0", len(notifier.texts))
	}
}

func TestReportQuarantinesOnRateLimitUsingRetryAfter(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	notifier := &fakeNotifier{}
	r := New(store, notifier, quietLog())

	before := time.Now()
	err := &tg.Error{Code: http.StatusTooManyRequests, RetryAfter: 90 * time.Second}
	handled := r.Report(t.Context(), 9, "delivery", "dl1_bot", err)
	if !handled {
		t.Fatal("Report did not handle a 429")
	}

	until, ok := store.quarantined[9]
	if !ok {
		t.Fatal("bot 9 was not quarantined")
	}
	wantEarliest := before.Add(90 * time.Second)
	if until.Before(wantEarliest) {
		t.Errorf("quarantined until %v, want at least %v (retry_after honoured)", until, wantEarliest)
	}
	if len(notifier.texts) != 1 {
		t.Fatalf("notified %d times, want 1", len(notifier.texts))
	}
}

func TestReportFloorsAZeroRetryAfter(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	r := New(store, nil, quietLog())

	before := time.Now()
	err := &tg.Error{Code: http.StatusTooManyRequests}
	r.Report(t.Context(), 9, "delivery", "dl1_bot", err)

	until := store.quarantined[9]
	if until.Before(before.Add(minQuarantine)) {
		t.Errorf("quarantined until %v, want at least the %v floor", until, minQuarantine)
	}
}

func TestReportDoesNotReNotifyAnAlreadyQuarantinedBot(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.quarantineFresh = false
	notifier := &fakeNotifier{}
	r := New(store, notifier, quietLog())

	err := &tg.Error{Code: http.StatusTooManyRequests, RetryAfter: time.Second}
	r.Report(t.Context(), 9, "delivery", "dl1_bot", err)

	if len(notifier.texts) != 0 {
		t.Errorf("notified %d times for an already-quarantined bot, want 0", len(notifier.texts))
	}
}

func TestReportIgnoresOtherTelegramErrors(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	r := New(store, nil, quietLog())

	err := &tg.Error{Code: http.StatusBadRequest, Description: "chat not found"}
	if r.Report(t.Context(), 7, "delivery", "dl1_bot", err) {
		t.Error("Report handled a chat-not-found error; want false, that's a different concern")
	}
	if store.retireCalls != 0 || store.quarantineCalls != 0 {
		t.Error("an unrelated Telegram error must not touch bot state")
	}
}

func TestReportWorksWithoutANotifier(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	r := New(store, nil, quietLog())

	err := &tg.Error{Code: http.StatusUnauthorized}
	if !r.Report(t.Context(), 7, "api", "api1_bot", err) {
		t.Fatal("Report should still handle the error and transition state with no notifier configured")
	}
	if !store.retired[7] {
		t.Error("bot was not retired despite the missing notifier")
	}
}
