package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/dailystats"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
)

const cbMenuStats = "m:st"

var (
	statsBtn    = message{en: "📊 Daily stats", fa: "📊 آمار روزانه"}
	statsFailed = message{en: "😅 Couldn't load the stats. Try again.", fa: "😅 آمار لود نشد. دوباره امتحان کن."}
)

func (b *bot) routeStatsCallback(
	ctx context.Context, q *tg.CallbackQuery, s screen, user identity.User, locale string, log *slog.Logger,
) bool {
	if q.Data != cbMenuStats {
		return false
	}
	if !isSuperadmin(user.Role) {
		b.toast(ctx, q, superToastDenied.pick(locale))
		return true
	}
	b.toast(ctx, q, "")
	b.showDailyStats(ctx, s, locale, log)
	return true
}

func (b *bot) showDailyStats(ctx context.Context, s screen, locale string, log *slog.Logger) {
	nav := actionsNavKeyboard(locale)
	if b.stats == nil {
		b.show(ctx, s, statsFailed.pick(locale), nav, log)
		return
	}
	snap, err := b.stats.DailyStats(ctx)
	if err != nil {
		log.Error("daily stats panel", "err", err)
		b.show(ctx, s, statsFailed.pick(locale), nav, log)
		return
	}
	b.show(ctx, s, dailystats.Format(snap, locale, dailystats.YMDLocal(time.Now())), nav, log)
}

func (b *bot) watchDailyStats(ctx context.Context) error {
	return dailystats.New(b.stats, notifyAdapter{b.notify}, b.log).Run(ctx)
}

// notifyAdapter lets a nil adminNotifier satisfy dailystats.Notifier without a typed nil panic.
type notifyAdapter struct{ n adminNotifier }

func (a notifyAdapter) NotifyAdmins(ctx context.Context, text string) {
	if a.n != nil {
		a.n.NotifyAdmins(ctx, text)
	}
}

func (b *bot) noteNewUser(ctx context.Context, user identity.User) {
	if !user.Created || user.Deleted || isAdmin(user.Role) || b.notify == nil {
		return
	}
	b.log.Info("new user", "tg", user.TGUserID)
	b.notify.NotifyAdmins(ctx, dailystats.NewUserNotice(user.TGUserID, user.Name, user.Username))
}
