package main

import (
	"context"
	"log/slog"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
)

type geminiKeyShelf interface {
	PutGeminiAPIKey(ctx context.Context, userID int64, key string) error
	HasGeminiAPIKey(ctx context.Context, userID int64) (bool, error)
	DeleteGeminiAPIKey(ctx context.Context, userID int64) error
}

var (
	aiKeyStatusNone = message{
		en: "🔑 No Gemini key yet.\n\nAdd a free key from Google AI Studio to translate subtitles to Persian.",
		fa: "🔑 هنوز کلید Gemini نیست.\n\nبرای ترجمهٔ زیرنویس به فارسی، یک کلید رایگان از Google AI Studio اضافه کن.",
	}
	aiKeyStatusSet = message{
		en: "🔑 <b>Gemini key</b> is saved.\n\nUsed only to translate subtitles to Persian. Replace or remove it. The key itself is never shown here.",
		fa: "🔑 <b>کلید Gemini</b> ذخیره شده.\n\nفقط برای ترجمهٔ زیرنویس به فارسی. عوضش کن یا حذف کن. خود کلید اینجا نشون داده نمی‌شه.",
	}
	aiKeyBtnAdd       = message{en: "➕ Add key", fa: "➕ افزودن کلید"}
	aiKeyBtnReplace   = message{en: "🔄 Replace", fa: "🔄 تعویض"}
	aiKeyBtnCancel    = message{en: "❌ Cancel", fa: "❌ لغو"}
	aiKeyReplaceIntro = message{
		en: "🔄 <b>Updating your Gemini key</b>\n\nReply to the next message with the new key, or cancel below. The current key stays until a new one is accepted.",
		fa: "🔄 <b>در حال تعویض کلید Gemini</b>\n\nکلید جدید رو به پیام بعدی ریپلای کن، یا لغو کن. تا وقتی کلید جدید قبول نشه، قبلی می‌مونه.",
	}
	aiKeyReplaceCancelled = message{
		en: "❌ Replace cancelled — your previous key is still saved.",
		fa: "❌ تعویض لغو شد — کلید قبلی هنوز ذخیره است.",
	}
)

func (b *bot) rememberGeminiWait(chatID int64) {
	if b.waitingGemini == nil {
		b.waitingGemini = map[int64]bool{}
	}
	b.waitingGemini[chatID] = true
}

func (b *bot) clearGeminiWait(chatID int64) {
	if b.waitingGemini != nil {
		delete(b.waitingGemini, chatID)
	}
}

func (b *bot) awaitingGemini(chatID int64) bool {
	return b.waitingGemini != nil && b.waitingGemini[chatID]
}

func aiKeyEmptyKeyboard(locale string) tg.InlineKeyboard {
	return tg.InlineKeyboard{Rows: [][]tg.InlineButton{
		{{Text: aiKeyBtnAdd.pick(locale), Data: cbAiKeyReplace, Style: tg.StylePrimary}},
		{{Text: reqBtnHome.pick(locale), Data: cbMenuHome}},
	}}
}

func aiKeyStatusKeyboard(locale string) tg.InlineKeyboard {
	return tg.InlineKeyboard{Rows: [][]tg.InlineButton{
		{{Text: aiKeyBtnReplace.pick(locale), Data: cbAiKeyReplace}},
		{{Text: aiKeyBtnRemove.pick(locale), Data: cbAiKeyRemove}},
		{{Text: reqBtnHome.pick(locale), Data: cbMenuHome}},
	}}
}

func aiKeyReplaceCancelKeyboard(locale string) tg.InlineKeyboard {
	return tg.InlineKeyboard{Rows: [][]tg.InlineButton{
		{{Text: aiKeyBtnCancel.pick(locale), Data: cbAiKeyCancel}},
	}}
}

func aiKeyRemoveConfirmKeyboard(locale string) tg.InlineKeyboard {
	return tg.InlineKeyboard{Rows: [][]tg.InlineButton{
		{{Text: aiKeyBtnRemoveYes.pick(locale), Data: cbAiKeyRemoveYes, Style: tg.StyleDanger}},
		{{Text: aiKeyBtnCancel.pick(locale), Data: cbSetAiKey}},
	}}
}

func (b *bot) showAiKeyStatus(ctx context.Context, s screen, user identity.User, locale string, log *slog.Logger) {
	if b.hasGeminiKey(ctx, user.ID, log) {
		b.show(ctx, s, aiKeyStatusSet.pick(locale), aiKeyStatusKeyboard(locale), log)
		return
	}
	b.show(ctx, s, aiKeyStatusNone.pick(locale), aiKeyEmptyKeyboard(locale), log)
}

func (b *bot) startSetGeminiKey(ctx context.Context, s screen, user identity.User, locale string, log *slog.Logger) {
	if b.hasGeminiKey(ctx, user.ID, log) {
		b.show(ctx, s, aiKeyReplaceIntro.pick(locale), aiKeyReplaceCancelKeyboard(locale), log)
		if s.messageID != 0 {
			if b.geminiReplaceMsg == nil {
				b.geminiReplaceMsg = map[int64]int64{}
			}
			b.geminiReplaceMsg[s.chatID] = s.messageID
		}
		b.sendGeminiForceReply(ctx, s.chatID, aiKeyIntro.pick(locale), locale, log)
		return
	}
	b.promptGeminiKey(ctx, s, locale, log)
}

func (b *bot) cancelSetGeminiKey(ctx context.Context, s screen, user identity.User, locale string, log *slog.Logger) {
	b.clearGeminiWait(s.chatID)
	b.dropMessages(ctx, s.chatID)
	if b.geminiReplaceMsg != nil {
		delete(b.geminiReplaceMsg, s.chatID)
	}
	if b.hasGeminiKey(ctx, user.ID, log) {
		b.show(ctx, s, aiKeyReplaceCancelled.pick(locale), aiKeyStatusKeyboard(locale), log)
		return
	}
	b.showAiKeyStatus(ctx, s, user, locale, log)
}

func (b *bot) promptGeminiKey(ctx context.Context, s screen, locale string, log *slog.Logger) {
	b.clearMenu(ctx, s, log)
	b.sendGeminiForceReply(ctx, s.chatID, aiKeyIntro.pick(locale), locale, log)
}

func (b *bot) sendGeminiForceReply(ctx context.Context, chatID int64, text, locale string, log *slog.Logger) {
	msg, err := b.client.SendForceReplyHTML(ctx, chatID, text, geminiKeyPlaceholder)
	if err != nil {
		log.Error("gemini key prompt", "err", err)
		b.show(ctx, screen{chatID: chatID}, failed.pick(locale), homeKeyboard(locale), log)
		return
	}
	b.rememberPrompt(chatID, msg.ID)
	b.rememberGeminiWait(chatID)
}

func (b *bot) handlePastedGeminiKey(ctx context.Context, s screen, user identity.User, in inbound, locale string) {
	log := b.log.With("chat", s.chatID)
	b.clearGeminiWait(s.chatID)
	b.dropMessages(ctx, s.chatID, in.msgID, in.replyToID)

	if b.gemini == nil {
		b.show(ctx, s, failed.pick(locale), homeKeyboard(locale), log)
		return
	}
	if err := b.gemini.PutGeminiAPIKey(ctx, user.ID, in.rest); err != nil {
		log.Error("store gemini key", "err", err)
		b.show(ctx, s, aiKeyStoreFailed.pick(locale), aiKeyEmptyKeyboard(locale), log)
		return
	}
	b.dropGeminiReplaceMsg(ctx, s.chatID, log)
	b.show(ctx, s, aiKeySaved.pick(locale), aiKeyStatusKeyboard(locale), log)
}

// dropGeminiReplaceMsg removes the "updating your Gemini key" message once the new key has been
// accepted -- it only carries a Cancel button that no longer applies past this point.
func (b *bot) dropGeminiReplaceMsg(ctx context.Context, chatID int64, log *slog.Logger) {
	if b.geminiReplaceMsg == nil {
		return
	}
	id, ok := b.geminiReplaceMsg[chatID]
	if !ok {
		return
	}
	delete(b.geminiReplaceMsg, chatID)
	if id == 0 {
		return
	}
	if err := b.client.DeleteMessage(ctx, chatID, id); err != nil {
		log.Debug("delete gemini replace-intro message", "err", err)
	}
}

func (b *bot) handleBadGeminiPaste(ctx context.Context, s screen, locale string, log *slog.Logger) {
	msg, err := b.client.SendForceReplyHTML(ctx, s.chatID, aiKeyBad.pick(locale), geminiKeyPlaceholder)
	if err != nil {
		log.Error("gemini bad-key prompt", "err", err)
		return
	}
	b.rememberPrompt(s.chatID, msg.ID)
	b.rememberGeminiWait(s.chatID)
}

func (b *bot) removeGeminiKey(ctx context.Context, s screen, user identity.User, locale string, log *slog.Logger) {
	if b.gemini == nil {
		b.show(ctx, s, failed.pick(locale), homeKeyboard(locale), log)
		return
	}
	if err := b.gemini.DeleteGeminiAPIKey(ctx, user.ID); err != nil {
		log.Error("delete gemini key", "err", err)
		b.show(ctx, s, failed.pick(locale), homeKeyboard(locale), log)
		return
	}
	b.show(ctx, s, aiKeyRemoved.pick(locale), aiKeyEmptyKeyboard(locale), log)
}

func (b *bot) hasGeminiKey(ctx context.Context, userID int64, log *slog.Logger) bool {
	if b.gemini == nil || userID == 0 {
		return false
	}
	ok, err := b.gemini.HasGeminiAPIKey(ctx, userID)
	if err != nil {
		log.Error("gemini key lookup", "err", err)
		return false
	}
	return ok
}

func (b *bot) routeAiKeyCallback(
	ctx context.Context, q *tg.CallbackQuery, s screen, user identity.User, locale string, log *slog.Logger,
) bool {
	switch q.Data {
	case cbSetAiKey:
		b.toast(ctx, q, "")
		b.showAiKeyStatus(ctx, s, user, locale, log)
		return true
	case cbAiKeyReplace:
		b.toast(ctx, q, "")
		b.startSetGeminiKey(ctx, s, user, locale, log)
		return true
	case cbAiKeyCancel:
		b.toast(ctx, q, "")
		b.cancelSetGeminiKey(ctx, s, user, locale, log)
		return true
	case cbAiKeyRemove:
		b.toast(ctx, q, "")
		b.show(ctx, s, aiKeyRemoveAsk.pick(locale), aiKeyRemoveConfirmKeyboard(locale), log)
		return true
	case cbAiKeyRemoveYes:
		b.toast(ctx, q, "")
		b.removeGeminiKey(ctx, s, user, locale, log)
		return true
	default:
		return false
	}
}
