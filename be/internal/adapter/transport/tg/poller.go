package tg

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
)

// healthReporter is the one thing Poller needs from internal/app/health -- declared here rather
// than imported as a concrete type so this package does not need to know about bot rotation at
// all when nobody wires one in (R-GO-5).
type healthReporter interface {
	Report(ctx context.Context, botID int32, role, username string, err error) bool
}

// Poller drives one bot: long-poll, hand each message to the handler, repeat.
//
// One poller per token, ever. Two of them on the same bot silently steal each other's updates,
// which is the failure mode R-OPS-3 exists to make impossible.
type Poller struct {
	client  *tg.Client
	handler *Handler
	log     *slog.Logger

	// botID/health are both optional (nil-safe): a caller that has not wired up bot rotation
	// (docs/02 §7) yet still gets ordinary getUpdates retry behaviour, just no state transition.
	botID  int32
	health healthReporter

	offset int64
	seen   map[int64]struct{}
}

// PollTimeout is how long a getUpdates call waits before returning empty. Long, because an idle
// poll costs nothing and a short one is just a busy loop against Telegram.
const PollTimeout = 30 * time.Second

// NewPoller builds a poller. botID and health may be zero/nil to skip bot-rotation reporting
// entirely (existing tests, or a caller that does not track bot ids).
func NewPoller(client *tg.Client, handler *Handler, log *slog.Logger, botID int32, health healthReporter) *Poller {
	return &Poller{
		client:  client,
		handler: handler,
		log:     log,
		botID:   botID,
		health:  health,
		seen:    make(map[int64]struct{}),
	}
}

// Run polls until the context is cancelled.
func (p *Poller) Run(ctx context.Context) error {
	p.log.Info("polling", "bot", p.client.Username())

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		updates, err := p.client.GetUpdates(ctx, p.offset, PollTimeout)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err() // shutdown, not a failure
			}
			p.backoff(ctx, err)
			continue
		}

		p.dispatch(ctx, updates)
	}
}

func (p *Poller) dispatch(ctx context.Context, updates []tg.Update) {
	for _, u := range updates {
		if u.ID >= p.offset {
			p.offset = u.ID + 1
		}
		if u.Message == nil {
			continue
		}
		// Telegram can redeliver; the reply cache makes that harmless, but skipping outright is
		// cheaper still (docs/02 §6).
		if _, dup := p.seen[u.ID]; dup {
			continue
		}
		p.remember(u.ID)
		p.handler.Handle(ctx, u.Message)
	}
}

// remember tracks seen update ids, bounded: Telegram's offset already prevents replays across
// calls, so this only guards duplicates within a window.
func (p *Poller) remember(id int64) {
	if len(p.seen) > 8192 {
		p.seen = make(map[int64]struct{}, 1024)
	}
	p.seen[id] = struct{}{}
}

func (p *Poller) backoff(ctx context.Context, err error) {
	delay := 3 * time.Second

	if p.health != nil {
		p.health.Report(ctx, p.botID, "api", p.client.Username(), err)
	}

	var tgErr *tg.Error
	if errors.As(err, &tgErr) {
		if tgErr.RetryAfter > 0 {
			delay = tgErr.RetryAfter
		}
		p.log.Warn("getUpdates failed", "bot", p.client.Username(), "code", tgErr.Code,
			"description", tgErr.Description, "retry_in", delay)
	} else {
		p.log.Warn("getUpdates failed", "bot", p.client.Username(), "err", err, "retry_in", delay)
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}
