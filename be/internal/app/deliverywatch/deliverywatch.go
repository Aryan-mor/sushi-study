// Package deliverywatch notices when API.Play has enqueued copies that nobody is claiming.
//
// apid and deliveryd are separate processes so a stalled copyMessage cannot delay /home
// (docs/01). That split is correct, and it also means apid can sit healthy while the copier is
// simply not running. Pending rows then fill the R-SEC-11 concurrent cap and every later /play
// looks like a rate limit. This loop is the missing health check (R-OPS-2): unclaimed past
// deliveryd's idle cap is a dead worker, not abuse.
package deliverywatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Stale is just past delivery.maxIdleDelay (10s). A pending row still unclaimed after that
// means deliveryd is not running, not that the copy is slow.
const Stale = 15 * time.Second

const (
	defaultEvery = 30 * time.Second
	runbook      = "start deliveryd (local: npm run dev:bots — watching apid always starts it)"
)

// Store is the one count this loop needs. Declared here, by the consumer (R-GO-5).
type Store interface {
	StaleUnclaimedCount(ctx context.Context, olderThan time.Duration) (int, error)
}

// Notifier is the one thing Watcher needs to reach a human. Same shape as health.Notifier;
// declared here so this package does not import health (R-GO-5).
type Notifier interface {
	NotifyAdmins(ctx context.Context, text string)
}

// Watcher polls for stale unclaimed deliveries and pages once per outage, not per row (R-OPS-1).
type Watcher struct {
	store  Store
	notify Notifier
	log    *slog.Logger
	every  time.Duration
	stale  time.Duration
	armed  bool
}

// New builds a Watcher with the production cadence.
func New(store Store, notify Notifier, log *slog.Logger) *Watcher {
	return &Watcher{store: store, notify: notify, log: log, every: defaultEvery, stale: Stale}
}

// Run checks immediately, then every Watcher.every, until ctx is done.
func (w *Watcher) Run(ctx context.Context) error {
	if err := w.Check(ctx); err != nil && !errors.Is(err, context.Canceled) {
		w.log.Error("deliveryd watchdog check failed", "err", err)
	}
	ticker := time.NewTicker(w.every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := w.Check(ctx); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return err
				}
				w.log.Error("deliveryd watchdog check failed", "err", err)
			}
		}
	}
}

// Check looks at the queue once. Exported so tests can drive it without waiting on the ticker.
func (w *Watcher) Check(ctx context.Context) error {
	n, err := w.store.StaleUnclaimedCount(ctx, w.stale)
	if err != nil {
		return err
	}
	if n == 0 {
		if w.armed {
			w.log.Info("deliveryd is claiming again")
			w.armed = false
		}
		return nil
	}
	w.log.Error("deliveryd is not claiming pending copies",
		"unclaimed", n, "older_than", w.stale, "runbook", runbook)
	if !w.armed {
		w.announce(ctx, n)
		w.armed = true
	}
	return nil
}

func (w *Watcher) announce(ctx context.Context, n int) {
	if w.notify == nil {
		return
	}
	w.notify.NotifyAdmins(ctx, fmt.Sprintf(
		"🔴 deliveryd is not claiming pending copies (%d unclaimed older than %s).\nPlay answers ERR_INTERNAL, not ERR_RATE.\nRunbook: %s.",
		n, w.stale, runbook,
	))
}
