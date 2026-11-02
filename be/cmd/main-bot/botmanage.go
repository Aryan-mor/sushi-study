package main

import (
	"context"
	"fmt"
	"html"
	"log/slog"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
)

var (
	botStatusTitle = message{
		en: "🤖 <b>Connected bot:</b> @%s\n\nKeep the token private. Test it, replace it, or remove it.",
		fa: "🤖 <b>بات متصل:</b> @%s\n\nتوکن رو پیش خودت نگه دار. تست کن، عوض کن، یا حذف کن.",
	}
	botStatusNone = message{
		en: "🤖 No bot connected yet.",
		fa: "🤖 هنوز بات وصل نیست.",
	}
	botBtnTest      = message{en: "🔎 Test", fa: "🔎 تست"}
	botBtnReplace   = message{en: "🔄 Replace", fa: "🔄 تعویض"}
	botBtnRemove    = message{en: "🗑 Remove", fa: "🗑 حذف"}
	botBtnAdd       = message{en: "➕ Add bot", fa: "➕ وصل کردن بات"}
	botReplaceIntro = message{
		en: "🔄 <b>Updating your bot</b>\n\nReply to the next message with the new token, or cancel below. The current bot stays connected until a new one succeeds.",
		fa: "🔄 <b>در حال تعویض بات</b>\n\nتوکن جدید رو به پیام بعدی ریپلای کن، یا لغو کن. تا وقتی توکن جدید قبول نشه، بات قبلی وصل می‌مونه.",
	}
	botBtnCancel        = message{en: "❌ Cancel", fa: "❌ لغو"}
	botReplaceCancelled = message{
		en: "❌ Replace cancelled — your previous bot is still connected.",
		fa: "❌ تعویض لغو شد — بات قبلی هنوز وصله.",
	}
	botTestOK = message{
		en: "✅ @%s is working.",
		fa: "✅ @%s کار می‌کنه.",
	}
	botTestFail = message{
		en: "😅 This bot isn't answering. Replace the token, or in @BotFather open the Mini App (or tap Open at the bottom), then Bot Settings, and turn on Bot to Bot Communication Mode.",
		fa: "😅 این بات جواب نمی‌ده. توکن رو عوض کن، یا تو @BotFather مینی‌اپ رو باز کن (یا پایین صفحه Open رو بزن)، برو Bot Settings و Bot to Bot Communication Mode رو روشن کن.",
	}
	botRemoveAsk = message{
		en: "Remove the connected bot? You can add another afterwards.",
		fa: "بات وصل‌شده حذف بشه؟ بعداً می‌تونی یکی دیگه وصل کنی.",
	}
	botBtnRemoveYes = message{en: "🗑 Yes, remove", fa: "🗑 بله، حذف کن"}
	botRemoved      = message{
		en: "🧹 Bot disconnected. Tap Add bot when you have a new token.",
		fa: "🧹 بات قطع شد. وقتی توکن جدید داشتی، وصل کردن بات رو بزن.",
	}
)

func botStatusKeyboard(locale string) tg.InlineKeyboard {
	return tg.InlineKeyboard{Rows: [][]tg.InlineButton{
		{{Text: botBtnTest.pick(locale), Data: cbBotTest}},
		{{Text: botBtnReplace.pick(locale), Data: cbBotReplace}},
		{{Text: botBtnRemove.pick(locale), Data: cbBotRemove}},
		{{Text: reqBtnHome.pick(locale), Data: cbMenuHome}},
	}}
}

func botEmptyKeyboard(locale string) tg.InlineKeyboard {
	return tg.InlineKeyboard{Rows: [][]tg.InlineButton{
		{{Text: botBtnAdd.pick(locale), Data: cbBotReplace, Style: tg.StylePrimary}},
		{{Text: reqBtnHome.pick(locale), Data: cbMenuHome}},
	}}
}

func botReplaceCancelKeyboard(locale string) tg.InlineKeyboard {
	return tg.InlineKeyboard{Rows: [][]tg.InlineButton{
		{{Text: botBtnCancel.pick(locale), Data: cbBotCancel}},
	}}
}

func botRemoveConfirmKeyboard(locale string) tg.InlineKeyboard {
	return tg.InlineKeyboard{Rows: [][]tg.InlineButton{
		{{Text: botBtnRemoveYes.pick(locale), Data: cbBotRemoveYes, Style: tg.StyleDanger}},
		{{Text: botBtnCancel.pick(locale), Data: cbMenuBot}},
	}}
}

func (b *bot) startConnectBot(ctx context.Context, s screen, user identity.User, locale string, log *slog.Logger) {
	b.showBotStatus(ctx, s, user, locale, log)
}

func (b *bot) showBotStatus(ctx context.Context, s screen, user identity.User, locale string, log *slog.Logger) {
	_, username, ok, err := b.tokens.ReaderBotToken(ctx, user.ID)
	if err != nil {
		log.Error("reader bot token lookup", "err", err)
		b.show(ctx, s, failed.pick(locale), homeKeyboardFor(locale, user.Role), log)
		return
	}
	if !ok {
		b.show(ctx, s, botStatusNone.pick(locale), botEmptyKeyboard(locale), log)
		return
	}
	text := fmt.Sprintf(botStatusTitle.pick(locale), html.EscapeString(username))
	b.show(ctx, s, text, botStatusKeyboard(locale), log)
}

func (b *bot) startReplaceBot(ctx context.Context, s screen, user identity.User, locale string, log *slog.Logger) {
	_, _, ok, err := b.tokens.ReaderBotToken(ctx, user.ID)
	if err != nil {
		log.Error("reader bot token lookup", "err", err)
		b.show(ctx, s, failed.pick(locale), homeKeyboardFor(locale, user.Role), log)
		return
	}
	if ok {
		b.show(ctx, s, botReplaceIntro.pick(locale), botReplaceCancelKeyboard(locale), log)
		if b.replaceMsg == nil {
			b.replaceMsg = map[int64]int64{}
		}
		if s.messageID != 0 {
			b.replaceMsg[s.chatID] = s.messageID
		}
		b.promptTokenReply(ctx, s.chatID, reconnectIntro.pick(locale), log)
		return
	}
	b.promptBotSetup(ctx, s, locale, log)
}

func (b *bot) cancelReplaceBot(ctx context.Context, s screen, user identity.User, locale string, log *slog.Logger) {
	b.dropMessages(ctx, s.chatID)
	statusID := s.messageID
	if b.replaceMsg != nil {
		if id := b.replaceMsg[s.chatID]; id != 0 {
			statusID = id
			delete(b.replaceMsg, s.chatID)
		}
	}
	_, _, ok, err := b.tokens.ReaderBotToken(ctx, user.ID)
	if err != nil {
		log.Error("reader bot token lookup", "err", err)
	}
	kb := botEmptyKeyboard(locale)
	if err == nil && ok {
		kb = botStatusKeyboard(locale)
	}
	b.show(ctx, screen{chatID: s.chatID, messageID: statusID}, botReplaceCancelled.pick(locale), kb, log)
}

func (b *bot) testCurrentBot(ctx context.Context, q *tg.CallbackQuery, s screen, user identity.User, locale string, log *slog.Logger) {
	token, username, ok, err := b.tokens.ReaderBotToken(ctx, user.ID)
	if err != nil {
		log.Error("reader bot token lookup", "err", err)
		b.toast(ctx, q, failed.pick(locale))
		return
	}
	if !ok {
		b.toast(ctx, q, botStatusNone.pick(locale))
		b.showBotStatus(ctx, s, user, locale, log)
		return
	}
	if err := probeBotToBot(ctx, token, b.initUsername); err != nil {
		log.Warn("bot token test failed", "err", err)
		b.toast(ctx, q, botTestFail.pick(locale))
		return
	}
	b.toast(ctx, q, fmt.Sprintf(botTestOK.pick(locale), username))
}

func (b *bot) removeCurrentBot(ctx context.Context, s screen, user identity.User, locale string, log *slog.Logger) {
	if err := b.tokens.DeleteReaderBotToken(ctx, user.ID); err != nil {
		log.Error("delete reader bot token", "err", err)
		b.show(ctx, s, failed.pick(locale), homeKeyboardFor(locale, user.Role), log)
		return
	}
	b.show(ctx, s, botRemoved.pick(locale), botEmptyKeyboard(locale), log)
}

func (b *bot) routeBotCallback(
	ctx context.Context, q *tg.CallbackQuery, s screen, user identity.User, locale string, log *slog.Logger,
) bool {
	switch q.Data {
	case cbMenuBot:
		b.toast(ctx, q, "")
		b.showBotStatus(ctx, s, user, locale, log)
		return true
	case cbBotTest:
		b.testCurrentBot(ctx, q, s, user, locale, log)
		return true
	case cbBotReplace:
		b.toast(ctx, q, "")
		b.startReplaceBot(ctx, s, user, locale, log)
		return true
	case cbBotCancel:
		b.toast(ctx, q, "")
		b.cancelReplaceBot(ctx, s, user, locale, log)
		return true
	case cbBotRemove:
		b.toast(ctx, q, "")
		b.show(ctx, s, botRemoveAsk.pick(locale), botRemoveConfirmKeyboard(locale), log)
		return true
	case cbBotRemoveYes:
		b.toast(ctx, q, "")
		b.removeCurrentBot(ctx, s, user, locale, log)
		return true
	default:
		return false
	}
}
