package main

import (
	"context"
	"errors"
	"log/slog"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
)

var (
	deleteSummary = message{
		en: "🗑 <b>Delete your Sushi account?</b>\n\n" +
			"Favourites, progress, and a connected bot all go. This chat stays — you can start over later.",
		fa: "🗑 <b>حساب سوشی‌ت حذف بشه؟</b>\n\n" +
			"علاقه‌مندی‌ها، پیشرفت، و بات وصل‌شده پاک می‌شن. این چت می‌مونه — بعداً می‌تونی از صفر شروع کنی.",
	}
	deleteFinal = message{
		en: "Really delete? This can't be undone.",
		fa: "واقعاً حذف بشه؟ برگشتی نداره.",
	}
	deleteDone = message{
		en: "Account deleted. If you want a new one, tap Start over.",
		fa: "حساب حذف شد. اگه خواستی از نو، شروع دوباره رو بزن.",
	}
	deleteCancelled = message{
		en: "Kept. Nothing was deleted.",
		fa: "باشه — چیزی حذف نشد.",
	}
	deleteProtected = message{
		en: "This account can't be deleted from here.",
		fa: "این حساب از اینجا قابل حذف نیست.",
	}
	deleteFailed = message{
		en: "😅 Couldn't delete it. Try again in a moment.",
		fa: "😅 حذف نشد. یه لحظه بعد دوباره بزن.",
	}
	deletedAlready = message{
		en: "This account is already gone. Tap Start over for a new one.",
		fa: "این حساب از قبل رفته. برای حساب جدید، شروع دوباره رو بزن.",
	}
	startOverBtn  = message{en: "✨ Start over", fa: "✨ شروع دوباره"}
	deleteNextBtn = message{en: "Continue", fa: "ادامه"}
	deleteYesBtn  = message{en: "Yes, delete", fa: "بله، حذف کن"}
	deleteNoBtn   = message{en: "Cancel", fa: "لغو"}
)

func deleteSummaryKeyboard(locale string) tg.InlineKeyboard {
	return tg.InlineKeyboard{Rows: [][]tg.InlineButton{
		{{Text: deleteNextBtn.pick(locale), Data: cbDelNext}},
		{{Text: deleteNoBtn.pick(locale), Data: cbDelNo}},
	}}
}

func deleteFinalKeyboard(locale string) tg.InlineKeyboard {
	return tg.InlineKeyboard{Rows: [][]tg.InlineButton{
		{{Text: deleteYesBtn.pick(locale), Data: cbDelYes, Style: tg.StyleDanger}},
		{{Text: deleteNoBtn.pick(locale), Data: cbDelNo}},
	}}
}

func deletedKeyboard(locale string) tg.InlineKeyboard {
	return tg.InlineKeyboard{Rows: [][]tg.InlineButton{
		{{Text: startOverBtn.pick(locale), Data: cbDelRevive, Style: tg.StylePrimary}},
	}}
}

func (b *bot) showDeleteSummary(ctx context.Context, s screen, locale string, log *slog.Logger) {
	b.show(ctx, s, deleteSummary.pick(locale), deleteSummaryKeyboard(locale), log)
}

func (b *bot) showDeleted(ctx context.Context, s screen, locale string, log *slog.Logger) {
	b.show(ctx, s, deletedAlready.pick(locale), deletedKeyboard(locale), log)
}

func (b *bot) confirmDelete(ctx context.Context, s screen, tgUserID int64, locale string, log *slog.Logger) {
	err := b.identity.Tombstone(ctx, tgUserID)
	switch {
	case err == nil:
		b.show(ctx, s, deleteDone.pick(locale), deletedKeyboard(locale), log)
	case errors.Is(err, identity.ErrProtected):
		b.show(ctx, s, deleteProtected.pick(locale), homeKeyboard(locale), log)
	case errors.Is(err, identity.ErrAlreadyDeleted):
		b.show(ctx, s, deletedAlready.pick(locale), deletedKeyboard(locale), log)
	default:
		log.Error("tombstone", "err", err)
		b.show(ctx, s, deleteFailed.pick(locale), homeKeyboard(locale), log)
	}
}

func (b *bot) reviveAccount(ctx context.Context, s screen, from *tg.User, locale string, log *slog.Logger) {
	if err := b.identity.Revive(ctx, from.ID); err != nil {
		log.Error("revive", "err", err)
		b.show(ctx, s, failed.pick(locale), deletedKeyboard(locale), log)
		return
	}
	user, err := b.identity.EnsureUser(ctx, from.ID, from.Name, from.Username)
	if err != nil {
		log.Error("ensure after revive", "err", err)
		b.show(ctx, s, failed.pick(locale), deletedKeyboard(locale), log)
		return
	}
	b.noteNewUser(ctx, user)
	b.showHome(ctx, s, user.Role, locale, log)
}

func (b *bot) routeDeleteCallback(ctx context.Context, q *tg.CallbackQuery, s screen, user identity.User, locale string, log *slog.Logger) bool {
	switch q.Data {
	case cbMenuDelete:
		b.toast(ctx, q, "")
		b.showDeleteSummary(ctx, s, locale, log)
		return true
	case cbDelNext:
		b.toast(ctx, q, "")
		b.show(ctx, s, deleteFinal.pick(locale), deleteFinalKeyboard(locale), log)
		return true
	case cbDelNo:
		b.toast(ctx, q, deleteCancelled.pick(locale))
		if user.Deleted {
			b.showDeleted(ctx, s, locale, log)
			return true
		}
		b.showHome(ctx, s, user.Role, locale, log)
		return true
	case cbDelYes:
		b.toast(ctx, q, "")
		b.confirmDelete(ctx, s, user.TGUserID, locale, log)
		return true
	case cbDelRevive:
		b.toast(ctx, q, "")
		b.reviveAccount(ctx, s, q.From, locale, log)
		return true
	default:
		return false
	}
}
