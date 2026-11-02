// Package health turns a bot's own errors into the rotation docs/02 §7 describes: a bot that is
// truly banned (401) is retired from its pool, a bot that is only rate-limited (429, past whatever
// the caller's own retry loop already absorbed) sits out its retry_after, and a superadmin hears
// about either transition exactly once, not on every request that lands while it is already down.
//
// Composition only (R-GO-3): callers already have an error from a Telegram call and the bot's row
// id; this package decides what that error means for the bot's rotation state.
package health

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
)

// minQuarantine floors a 429 with no retry_after (docs/02 §6): quarantining for zero time would
// make the very next request retry the same dead bot immediately.
const minQuarantine = 30 * time.Second

// Store is what Reporter needs from storage. Declared here, by the consumer (R-GO-5).
type Store interface {
	// MarkBotQuarantined benches a bot until until. fresh is false when it was already
	// quarantined -- a repeat 429 while sitting out an earlier one is not a new event.
	MarkBotQuarantined(ctx context.Context, botID int32, until time.Time) (fresh bool, err error)
	// MarkBotRetired permanently drops a banned/token-revoked bot from rotation. fresh is false
	// when it was already retired.
	MarkBotRetired(ctx context.Context, botID int32) (fresh bool, err error)
}

// Notifier is the one thing Reporter needs to reach a human. Declared here, not imported from
// opsnotify, so this package does not need to know how the message is actually sent (R-GO-5).
type Notifier interface {
	NotifyAdmins(ctx context.Context, text string)
}

// Reporter watches Telegram errors for the two conditions docs/02 §7 cares about and reacts.
type Reporter struct {
	store  Store
	notify Notifier
	log    *slog.Logger
}

// New builds a Reporter. notify may be nil (tests, or a deployment with no SUPERADMIN_IDS
// configured) -- a health transition still happens, it just goes unannounced.
func New(store Store, notify Notifier, log *slog.Logger) *Reporter {
	return &Reporter{store: store, notify: notify, log: log}
}

// Report inspects err from a call made against botID (role/username are for the log line and the
// admin message only). It returns true when err was a bot-health problem this call handled
// (quarantined or retired) -- false means err was something else entirely and the caller should
// keep its own error handling.
func (r *Reporter) Report(ctx context.Context, botID int32, role, username string, err error) bool {
	var tgErr *tg.Error
	if !errors.As(err, &tgErr) {
		return false
	}

	switch {
	case tgErr.IsUnauthorized():
		r.retire(ctx, botID, role, username, tgErr)
		return true
	case tgErr.IsRateLimit():
		r.quarantine(ctx, botID, role, username, tgErr)
		return true
	default:
		return false
	}
}

func (r *Reporter) retire(ctx context.Context, botID int32, role, username string, tgErr *tg.Error) {
	fresh, err := r.store.MarkBotRetired(ctx, botID)
	if err != nil {
		r.log.Error("mark bot retired failed", "bot", username, "role", role, "err", err)
		return
	}
	if !fresh {
		return
	}
	r.log.Error("bot retired: unauthorized", "bot", username, "role", role, "telegram_err", tgErr.Description)
	r.announce(ctx, fmt.Sprintf(
		"🔴 %s bot @%s is banned or its token was revoked (401 Unauthorized).\nRemoved from rotation — replace it in config, the current pool no longer includes it.",
		role, username,
	))
}

func (r *Reporter) quarantine(ctx context.Context, botID int32, role, username string, tgErr *tg.Error) {
	until := time.Now().Add(max(tgErr.RetryAfter, minQuarantine))
	fresh, err := r.store.MarkBotQuarantined(ctx, botID, until)
	if err != nil {
		r.log.Error("mark bot quarantined failed", "bot", username, "role", role, "err", err)
		return
	}
	if !fresh {
		return
	}
	r.log.Warn("bot quarantined: rate limited", "bot", username, "role", role, "until", until)
	r.announce(ctx, fmt.Sprintf(
		"🟡 %s bot @%s is rate-limited by Telegram.\nBenched until %s, then back in rotation automatically.",
		role, username, until.Format(time.RFC3339),
	))
}

func (r *Reporter) announce(ctx context.Context, text string) {
	if r.notify == nil {
		return
	}
	r.notify.NotifyAdmins(ctx, text)
}
