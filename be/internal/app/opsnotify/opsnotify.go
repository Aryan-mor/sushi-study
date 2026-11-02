// Package opsnotify DMs the superadmins when something needs a human (docs/02 §7): a bot banned
// or rate-limited. It sends through main-bot -- the one bot people already expect prose from
// (ADR 0004 R-VOICE-1) -- never the bot that actually failed, which may be unreachable.
//
// Composition only (R-GO-3).
package opsnotify

import (
	"context"
	"log/slog"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
)

// Sender is the one Telegram call this package needs, satisfied directly by *tg.Client -- same
// shape as delivery.Sender, declared again here rather than shared, because a health notifier and
// a delivery push are different concerns that happen to need the same one method (R-GO-5).
type Sender interface {
	SendMessage(ctx context.Context, chatID int64, text string) (tg.Message, error)
}

// Notifier DMs every configured superadmin. Best-effort: a failed notification must never break
// the caller's own error handling, so every failure is logged and swallowed, never returned.
type Notifier struct {
	sender   Sender
	adminIDs []int64
	log      *slog.Logger
}

// New builds a Notifier. adminIDs is config.SuperadminIDs() (docs/08 §2.1) -- may be empty, in
// which case NotifyAdmins is a silent no-op (a deployment with nobody to page is a valid, if
// unmonitored, config).
func New(sender Sender, adminIDs []int64, log *slog.Logger) *Notifier {
	return &Notifier{sender: sender, adminIDs: adminIDs, log: log}
}

// NotifyAdmins sends text to every superadmin. One admin's chat being unreachable (never started
// main-bot, blocked it) must not stop the others from hearing about it.
func (n *Notifier) NotifyAdmins(ctx context.Context, text string) {
	for _, id := range n.adminIDs {
		if _, err := n.sender.SendMessage(ctx, id, text); err != nil {
			n.log.Warn("superadmin notify failed", "admin", id, "err", err)
		}
	}
}
