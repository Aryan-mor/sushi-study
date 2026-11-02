package main

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
	"github.com/Aryan-mor/sushi-study/be/internal/app/onboarding"
)

// screen is where a step is rendered: a new message, or an edit of the one whose button was
// tapped. Editing keeps the chat from filling up with dead menus.
type screen struct {
	chatID    int64
	messageID int64 // zero means send a new message
}

func (b *bot) show(ctx context.Context, s screen, text string, kb tg.InlineKeyboard, log *slog.Logger) {
	var err error
	switch {
	case s.messageID != 0:
		err = b.client.EditKeyboard(ctx, s.chatID, s.messageID, text, kb)
	case len(kb.Rows) > 0:
		_, err = b.client.SendKeyboard(ctx, s.chatID, text, kb)
	default:
		_, err = b.client.SendHTML(ctx, s.chatID, text)
	}
	if err != nil {
		log.Error("show failed", "err", err)
	}
}

func (b *bot) showHome(ctx context.Context, s screen, role, locale string, log *slog.Logger) {
	b.show(ctx, s, welcome.pick(locale), homeKeyboardFor(locale, role), log)
}

func (b *bot) showHelp(ctx context.Context, s screen, role, locale string, log *slog.Logger) {
	b.show(ctx, s, help.pick(locale), homeKeyboardFor(locale, role), log)
}

// callbackScreen is the message to edit. Telegram sometimes omits callback_query.message
// (inaccessible / too old); a private DM still has chat id == from.id, so we send a new one.
func callbackScreen(q *tg.CallbackQuery) screen {
	if q == nil {
		return screen{}
	}
	if q.Message != nil && q.Message.Chat.ID != 0 {
		return screen{chatID: q.Message.Chat.ID, messageID: q.Message.ID}
	}
	if q.From != nil {
		return screen{chatID: q.From.ID}
	}
	return screen{}
}

// startLogin asks the next unanswered onboarding question, or confirms they are already done.
func (b *bot) startLogin(ctx context.Context, s screen, user identity.User, locale string, log *slog.Logger) {
	b.clearLoginLanguage(s.chatID)
	prefs, err := b.onboarding.Prefs(ctx, user.ID)
	if err != nil {
		log.Error("prefs lookup", "err", err)
		b.show(ctx, s, failed.pick(locale), homeKeyboardFor(locale, user.Role), log)
		return
	}
	if prefs.Locale != "" {
		locale = prefs.Locale
	}
	b.advance(ctx, s, user, locale, log)
}

// advance asks the next unanswered question, or finishes by binding the chat.
func (b *bot) advance(ctx context.Context, s screen, user identity.User, fallback string, log *slog.Logger) {
	prefs, err := b.onboarding.Prefs(ctx, user.ID)
	if err != nil {
		log.Error("prefs lookup", "err", err)
		b.show(ctx, s, failed.pick(fallback), homeKeyboardFor(fallback, user.Role), log)
		return
	}

	locale := prefs.Locale
	if locale == "" {
		locale = fallback
	}

	switch prefs.Next() {
	case onboarding.StepLanguage:
		b.armLoginLanguage(s.chatID)
		b.show(ctx, s, askLanguage.pick(locale), languageKeyboard(), log)
	case onboarding.StepDone:
		b.finish(ctx, s, user, locale, log)
	}
}

// finish confirms the chat is bound, creating the binding on the first pass through. main-bot
// never sends the assignment itself (ADR 0004): the app fetches it from init-bot over the
// protocol. This only ever says "you're set", never which bot that is, and never a token.
func (b *bot) finish(ctx context.Context, s screen, user identity.User, locale string, log *slog.Logger) {
	if _, err := b.identity.Resolve(ctx, s.chatID); err == nil {
		b.show(ctx, s, alreadySet.pick(locale), homeKeyboardFor(locale, user.Role), log)
		return
	} else if !errors.Is(err, identity.ErrNoBinding) {
		log.Error("resolve failed", "err", err)
		b.show(ctx, s, failed.pick(locale), homeKeyboardFor(locale, user.Role), log)
		return
	}

	binding, err := b.identity.Bind(ctx, user.TGUserID, s.chatID, "")
	if err != nil {
		log.Error("bind failed", "err", err)
		b.show(ctx, s, failed.pick(locale), homeKeyboardFor(locale, user.Role), log)
		return
	}

	log.Info("bound", "user", binding.UserID, "epoch", binding.Epoch)
	b.show(ctx, s, assigned.pick(locale), homeKeyboardFor(locale, user.Role), log)
}

func (b *bot) handleCallback(ctx context.Context, q *tg.CallbackQuery) {
	if q.From == nil {
		b.toast(ctx, q, "")
		return
	}
	s := callbackScreen(q)
	if s.chatID == 0 {
		b.toast(ctx, q, "")
		return
	}

	log := b.log.With("chat", s.chatID)
	user, err := b.identity.EnsureUser(ctx, q.From.ID, q.From.Name, q.From.Username)
	if b.refuseIfClosed(ctx, s, q.From, err, log) {
		b.toast(ctx, q, "")
		return
	}
	if err != nil {
		log.Error("ensure user", "err", err)
		hint := localeHint(q.From)
		b.toast(ctx, q, failed.pick(hint))
		b.show(ctx, s, failed.pick(hint), homeKeyboard(hint), log)
		return
	}

	locale := b.callbackLocale(ctx, user, q.From)

	if user.Deleted {
		if b.routeDeleteCallback(ctx, q, s, user, locale, log) {
			return
		}
		b.toast(ctx, q, "")
		b.showDeleted(ctx, s, locale, log)
		return
	}
	b.noteNewUser(ctx, user)
	b.dispatchLiveCallback(ctx, q, s, user, locale, log)
}

func (b *bot) callbackLocale(ctx context.Context, user identity.User, from *tg.User) string {
	locale := localeHint(from)
	if user.Deleted {
		return locale
	}
	if prefs, err := b.onboarding.Prefs(ctx, user.ID); err == nil && prefs.Locale != "" {
		return prefs.Locale
	}
	return locale
}

func (b *bot) dispatchLiveCallback(ctx context.Context, q *tg.CallbackQuery, s screen, user identity.User, locale string, log *slog.Logger) {
	b.clearIDWait(s.chatID)
	b.clearNewsWait(s.chatID)
	b.clearAccessWait(s.chatID)
	if b.routeHubCallback(ctx, q, s, user, locale, log) {
		return
	}
	if b.routeRequestCallback(ctx, q, s, user, locale, log) {
		return
	}
	if b.routeNewsCallback(ctx, q, user, locale, log) {
		return
	}
	if b.routeBotCallback(ctx, q, s, user, locale, log) {
		return
	}
	if b.routeAiKeyCallback(ctx, q, s, user, locale, log) {
		return
	}
	if b.routeSettingsCallback(ctx, q, s, user, locale, log) {
		return
	}
	if b.routeSupportCallback(ctx, q, s, locale, log) {
		return
	}
	if b.routeDeleteCallback(ctx, q, s, user, locale, log) {
		return
	}
	if b.routeDownloadCallback(ctx, q, s, user, locale, log) {
		return
	}
	if b.routePersonalCallback(ctx, q, s, user, locale, log) {
		return
	}
	b.toast(ctx, q, "")

	switch q.Data {
	case cbMenuHome:
		b.showHome(ctx, s, user.Role, locale, log)
	case cbMenuHelp:
		b.showHelp(ctx, s, user.Role, locale, log)
	case cbMenuLanguage:
		b.show(ctx, s, askLanguage.pick(locale), withSettingsBack(languageKeyboard(), locale), log)
	case cbMenuLogin:
		b.startLogin(ctx, s, user, locale, log)
	case cbMenuApp:
		b.startAppCode(ctx, s, user, locale, log)
	default:
		b.recordAnswer(ctx, s, user, q.Data, locale)
	}
}

func (b *bot) recordAnswer(ctx context.Context, s screen, user identity.User, data, locale string) {
	log := b.log.With("chat", s.chatID)
	prefs, prefErr := b.onboarding.Prefs(ctx, user.ID)
	if prefErr != nil {
		log.Error("prefs lookup", "err", prefErr)
		b.show(ctx, s, failed.pick(locale), homeKeyboardFor(locale, user.Role), log)
		return
	}
	wasDone := prefs.Complete()

	var err error
	nextLocale := locale
	switch {
	case strings.HasPrefix(data, cbLocalePrefix):
		nextLocale = strings.TrimPrefix(data, cbLocalePrefix)
		err = b.onboarding.SetLocale(ctx, user.ID, nextLocale)
	case strings.HasPrefix(data, "q:"), strings.HasPrefix(data, "a:"):
		// Legacy quality/audio keyboards from older bot versions — taste wizard owns these now.
		log.Info("ignored legacy quality/audio callback", "data", data)
		b.afterPrefSaved(ctx, s, user, nextLocale, wasDone, log)
		return
	default:
		log.Warn("unknown callback")
		return
	}
	if err != nil {
		log.Error("recording the answer failed", "err", err)
		return
	}
	b.afterPrefSaved(ctx, s, user, nextLocale, wasDone, log)
}

func localeHint(from *tg.User) string {
	if from != nil && strings.HasPrefix(from.Language, "fa") {
		return "fa"
	}
	return "en"
}
