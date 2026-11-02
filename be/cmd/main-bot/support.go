package main

import (
	"context"
	"fmt"
	"html"
	"log/slog"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
)

var (
	supportBody = message{
		en: "💬 <b>Need a person?</b>\n\nSend a message to @%s — they read it and answer there.",
		fa: "💬 <b>با آدم حرف می‌زنی؟</b>\n\nبه @%s پیام بده — همون‌جا می‌خونن و جواب می‌دن.",
	}
	supportOpenBtn = message{en: "💬 Support", fa: "💬 پشتیبانی"}
	supportDelBtn  = message{en: "🗑 Delete account", fa: "🗑 حذف حساب"}
)

func supportKeyboard(locale, handle string) tg.InlineKeyboard {
	url := "https://t.me/" + handle
	return tg.InlineKeyboard{Rows: [][]tg.InlineButton{
		{{Text: supportOpenBtn.pick(locale), URL: url, Style: tg.StylePrimary}},
		{{Text: supportDelBtn.pick(locale), Data: cbMenuDelete, Style: tg.StyleDanger}},
		{{Text: reqBtnHome.pick(locale), Data: cbMenuHome}},
	}}
}

func (b *bot) showSupport(ctx context.Context, s screen, locale string, log *slog.Logger) {
	handle := b.supportUser
	// The handle reaches us from config, so it is not a person's input — but it lands in a
	// parse_mode=HTML body, and one stray '&' or '<' in a mistyped env var would make Telegram
	// reject the whole message rather than show a broken name (R-SEC-10). Escaping costs nothing
	// and turns a dead Support screen into a cosmetic typo. The keyboard's URL takes the raw
	// handle: that is a URL field, not markup.
	body := fmt.Sprintf(supportBody.pick(locale), html.EscapeString(handle))
	b.show(ctx, s, body, supportKeyboard(locale, handle), log)
}

func (b *bot) routeSupportCallback(ctx context.Context, q *tg.CallbackQuery, s screen, locale string, log *slog.Logger) bool {
	if q.Data != cbMenuSupport {
		return false
	}
	b.toast(ctx, q, "")
	b.showSupport(ctx, s, locale, log)
	return true
}
