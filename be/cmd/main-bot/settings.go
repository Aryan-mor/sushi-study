package main

import (
	"context"
	"log/slog"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
)

var (
	settingsTitle = message{
		en: "⚙️ <b>Settings</b>", fa: "⚙️ <b>تنظیمات</b>", ar: "⚙️ <b>الإعدادات</b>", de: "⚙️ <b>Einstellungen</b>",
		ru: "⚙️ <b>Настройки</b>", uk: "⚙️ <b>Налаштування</b>", it: "⚙️ <b>Impostazioni</b>", fr: "⚙️ <b>Réglages</b>", tr: "⚙️ <b>Ayarlar</b>",
	}
	settingsBack = message{
		en: "◀️ Settings", fa: "◀️ تنظیمات", ar: "◀️ الإعدادات", de: "◀️ Einstellungen",
		ru: "◀️ Настройки", uk: "◀️ Налаштування", it: "◀️ Impostazioni", fr: "◀️ Réglages", tr: "◀️ Ayarlar",
	}
	settingsLang = message{
		en: "🌐 Language", fa: "🌐 زبان", ar: "🌐 اللغة", de: "🌐 Sprache",
		ru: "🌐 Язык", uk: "🌐 Мова", it: "🌐 Lingua", fr: "🌐 Langue", tr: "🌐 Dil",
	}
)

func settingsKeyboard(locale string) tg.InlineKeyboard {
	return tg.InlineKeyboard{Rows: [][]tg.InlineButton{
		{{Text: settingsLang.pick(locale), Data: cbMenuLanguage}},
		{{Text: reqBtnHome.pick(locale), Data: cbMenuHome}},
	}}
}

func withSettingsBack(kb tg.InlineKeyboard, locale string) tg.InlineKeyboard {
	kb.Rows = append(kb.Rows, []tg.InlineButton{{Text: settingsBack.pick(locale), Data: cbMenuSettings}})
	return kb
}

func (b *bot) showSettings(ctx context.Context, s screen, _ identity.User, locale string, log *slog.Logger) {
	b.show(ctx, s, settingsTitle.pick(locale), settingsKeyboard(locale), log)
}

func (b *bot) routeSettingsCallback(
	ctx context.Context, q *tg.CallbackQuery, s screen, user identity.User, locale string, log *slog.Logger,
) bool {
	switch q.Data {
	case cbMenuSettings:
		b.toast(ctx, q, "")
		b.showSettings(ctx, s, user, locale, log)
		return true
	default:
		return false
	}
}

func (b *bot) afterPrefSaved(ctx context.Context, s screen, user identity.User, locale string, wasDone bool, log *slog.Logger) {
	b.tuneChat(ctx, s.chatID, locale, user.Role, log)
	if wasDone {
		b.clearLoginLanguage(s.chatID)
		b.showSettings(ctx, s, user, locale, log)
		return
	}
	// /start login is still the path that binds. A first bare /start only picks how this bot talks.
	if b.consumeLoginLanguage(s.chatID) {
		b.advance(ctx, s, user, locale, log)
		return
	}
	b.showHome(ctx, s, user.Role, locale, log)
}

func (b *bot) savedLocale(ctx context.Context, userID int64) (string, bool) {
	if userID == 0 || b.onboarding == nil {
		return "", false
	}
	prefs, err := b.onboarding.Prefs(ctx, userID)
	if err != nil {
		return "", false
	}
	return prefs.Locale, true
}

// skipsLanguageGate lets a deep link or /login through before a bot language exists.
// A bare /start, /help, or any other chat must pick a language first.
func skipsLanguageGate(in inbound) bool {
	if in.cmd == "login" {
		return true
	}
	return in.cmd == "start" && in.rest != ""
}

func (b *bot) armLoginLanguage(chatID int64) {
	if b.loginLanguage == nil {
		b.loginLanguage = map[int64]bool{}
	}
	b.loginLanguage[chatID] = true
}

func (b *bot) clearLoginLanguage(chatID int64) {
	if b.loginLanguage != nil {
		delete(b.loginLanguage, chatID)
	}
}

func (b *bot) consumeLoginLanguage(chatID int64) bool {
	if b.loginLanguage == nil || !b.loginLanguage[chatID] {
		return false
	}
	delete(b.loginLanguage, chatID)
	return true
}
