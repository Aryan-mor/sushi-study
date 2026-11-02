package opsnotify

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
)

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeSender struct {
	sentTo []int64
	failOn map[int64]bool
}

func (f *fakeSender) SendMessage(_ context.Context, chatID int64, _ string) (tg.Message, error) {
	if f.failOn[chatID] {
		return tg.Message{}, errors.New("telegram: 400 chat not found")
	}
	f.sentTo = append(f.sentTo, chatID)
	return tg.Message{}, nil
}

func TestNotifyAdminsSendsToEveryConfiguredID(t *testing.T) {
	t.Parallel()

	sender := &fakeSender{}
	n := New(sender, []int64{111, 222, 333}, quietLog())

	n.NotifyAdmins(t.Context(), "bot down")

	if len(sender.sentTo) != 3 {
		t.Fatalf("sent to %d admins, want 3", len(sender.sentTo))
	}
}

func TestNotifyAdminsKeepsGoingWhenOneFails(t *testing.T) {
	t.Parallel()

	sender := &fakeSender{failOn: map[int64]bool{222: true}}
	n := New(sender, []int64{111, 222, 333}, quietLog())

	n.NotifyAdmins(t.Context(), "bot down")

	if len(sender.sentTo) != 2 {
		t.Fatalf("sent to %d admins, want 2 (one dropped, two still reached)", len(sender.sentTo))
	}
}

func TestNotifyAdminsWithNoIDsIsANoOp(t *testing.T) {
	t.Parallel()

	sender := &fakeSender{}
	n := New(sender, nil, quietLog())

	n.NotifyAdmins(t.Context(), "bot down")

	if len(sender.sentTo) != 0 {
		t.Error("no admins configured, but a message was sent somewhere")
	}
}
