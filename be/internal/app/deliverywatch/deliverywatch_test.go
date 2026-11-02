package deliverywatch_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/app/deliverywatch"
)

type fakeStore struct {
	n   int
	err error
}

func (f *fakeStore) StaleUnclaimedCount(context.Context, time.Duration) (int, error) {
	return f.n, f.err
}

type fakeNotify struct{ msgs []string }

func (n *fakeNotify) NotifyAdmins(_ context.Context, text string) {
	n.msgs = append(n.msgs, text)
}

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestCheckPagesOncePerOutage(t *testing.T) {
	t.Parallel()

	store := &fakeStore{n: 2}
	notify := &fakeNotify{}
	w := deliverywatch.New(store, notify, quietLog())

	if err := w.Check(context.Background()); err != nil {
		t.Fatalf("first Check: %v", err)
	}
	if len(notify.msgs) != 1 {
		t.Fatalf("notified %d times, want 1 on the rising edge", len(notify.msgs))
	}
	if !strings.Contains(notify.msgs[0], "Runbook:") {
		t.Errorf("alert %q does not name a runbook (R-OPS-1)", notify.msgs[0])
	}

	if err := w.Check(context.Background()); err != nil {
		t.Fatalf("second Check: %v", err)
	}
	if len(notify.msgs) != 1 {
		t.Fatalf("notified %d times, want still 1 while the outage lasts (R-OPS-1)", len(notify.msgs))
	}

	store.n = 0
	if err := w.Check(context.Background()); err != nil {
		t.Fatalf("recovery Check: %v", err)
	}
	store.n = 1
	if err := w.Check(context.Background()); err != nil {
		t.Fatalf("second outage Check: %v", err)
	}
	if len(notify.msgs) != 2 {
		t.Fatalf("notified %d times, want 2 after a second outage", len(notify.msgs))
	}
}

func TestCheckWithNoNotifierStillRuns(t *testing.T) {
	t.Parallel()

	w := deliverywatch.New(&fakeStore{n: 3}, nil, quietLog())
	if err := w.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
}
