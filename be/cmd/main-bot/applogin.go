package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/botlogin"
	"github.com/Aryan-mor/sushi-study/be/internal/app/enroll"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
	"github.com/Aryan-mor/sushi-study/be/internal/app/loginpost"
)

type tokenShelf interface {
	PutReaderBotToken(ctx context.Context, userID int64, token, username string) error
	ReaderBotToken(ctx context.Context, userID int64) (token, username string, ok bool, err error)
	DeleteReaderBotToken(ctx context.Context, userID int64) error
	PutLoginNonce(ctx context.Context, userID int64, nonce []byte) error
	LoginNonce(ctx context.Context, userID int64) (nonce []byte, ok bool, err error)
	DeleteLoginNonce(ctx context.Context, userID int64) error
	EnqueueLoginPost(ctx context.Context, blob string, deleteAfter time.Time) error
}

type tokenVerifier struct{}

func (tokenVerifier) VerifyToken(ctx context.Context, token string) (int64, string, error) {
	me, err := tg.New(token).Me(ctx)
	if err != nil {
		return 0, "", err
	}
	return me.ID, me.Username, nil
}

func (b *bot) rememberStartNonce(ctx context.Context, user identity.User, rest string, log *slog.Logger) {
	nonce, err := botlogin.ParseStartNonce(rest)
	if err != nil {
		return
	}
	if err := b.tokens.PutLoginNonce(ctx, user.ID, nonce); err != nil {
		log.Error("store login nonce", "err", err)
	}
}

func (b *bot) startAppCode(ctx context.Context, s screen, user identity.User, locale string, log *slog.Logger) {
	token, _, ok, err := b.tokens.ReaderBotToken(ctx, user.ID)
	if err != nil {
		log.Error("reader bot token lookup", "err", err)
		b.show(ctx, s, failed.pick(locale), homeKeyboard(locale), log)
		return
	}
	if !ok {
		b.promptBotSetup(ctx, s, locale, log)
		return
	}
	b.finishLogin(ctx, s, user, token, locale, log)
}

func (b *bot) promptBotSetup(ctx context.Context, s screen, locale string, log *slog.Logger) {
	b.promptConnectBot(ctx, s, botSetupIntro.pick(locale), locale, log)
}

func (b *bot) promptConnectBot(ctx context.Context, s screen, text, locale string, log *slog.Logger) {
	b.clearMenu(ctx, s, log)
	msg, err := b.client.SendForceReplyHTML(ctx, s.chatID, text, botTokenPlaceholder)
	if err != nil {
		log.Error("bot setup prompt", "err", err)
		b.show(ctx, screen{chatID: s.chatID}, failed.pick(locale), homeKeyboard(locale), log)
		return
	}
	b.rememberPrompt(s.chatID, msg.ID)
}

// clearMenu removes the inline-keyboard message a callback came from, so a following ForceReply
// prompt is not stacked under a stale menu. No-op when the screen is a fresh message (messageID 0).
func (b *bot) clearMenu(ctx context.Context, s screen, log *slog.Logger) {
	if s.messageID == 0 {
		return
	}
	if err := b.client.DeleteMessage(ctx, s.chatID, s.messageID); err != nil {
		log.Warn("could not clear the menu before ForceReply", "err", err)
	}
}

func (b *bot) promptTokenReply(ctx context.Context, chatID int64, text string, log *slog.Logger) {
	msg, err := b.client.SendForceReplyHTML(ctx, chatID, text, botTokenPlaceholder)
	if err != nil {
		log.Error("token reply prompt", "err", err)
		return
	}
	b.rememberPrompt(chatID, msg.ID)
}

func (b *bot) handlePastedToken(ctx context.Context, s screen, user identity.User, in inbound, locale string) {
	log := b.log.With("chat", s.chatID)
	b.dropMessages(ctx, s.chatID, in.msgID, in.replyToID)

	token := in.rest
	binding, username, err := b.enroll.Enroll(ctx, user.TGUserID, token)
	if err != nil {
		if errors.Is(err, enroll.ErrInvalidToken) {
			// Enroll wraps *every* getMe failure as ErrInvalidToken -- a genuinely bad token,
			// but also a network/DNS failure reaching api.telegram.org. Log the cause so the two
			// are distinguishable; the user still just sees "that token doesn't work".
			log.Warn("enroll rejected the token", "err", err)
			b.promptTokenReply(ctx, s.chatID, enrollBadToken.pick(locale), log)
			return
		}
		log.Error("enrollment failed", "err", err)
		b.promptTokenReply(ctx, s.chatID, enrollFailed.pick(locale), log)
		return
	}
	log.Debug("enrolled reader bot", "reader_chat_id", binding.ReaderChatID, "bot_username", username)
	if err := b.tokens.PutReaderBotToken(ctx, user.ID, token, username); err != nil {
		log.Error("store reader bot token", "err", err)
		b.promptTokenReply(ctx, s.chatID, enrollFailed.pick(locale), log)
		return
	}
	if b.replaceMsg != nil {
		delete(b.replaceMsg, s.chatID)
	}
	b.finishLogin(ctx, s, user, token, locale, log)
}

func (b *bot) finishLogin(ctx context.Context, s screen, user identity.User, token, locale string, log *slog.Logger) {
	log.Debug("bot-to-bot probe: sending", "init_bot_username", b.initUsername)
	if err := probeBotToBot(ctx, token, b.initUsername); err != nil {
		b.finishLoginProbeFail(ctx, s, err, locale, log)
		return
	}
	log.Debug("bot-to-bot probe: ok")

	nonce, ok, err := b.tokens.LoginNonce(ctx, user.ID)
	if err != nil {
		log.Error("login nonce lookup", "err", err)
		b.show(ctx, s, failed.pick(locale), homeKeyboard(locale), log)
		return
	}
	if !ok {
		b.dropMessages(ctx, s.chatID)
		if _, sendErr := b.client.SendHTMLDismissReply(ctx, s.chatID, appCodeNeedApp.pick(locale)); sendErr != nil {
			log.Error("need-app notice", "err", sendErr)
		}
		return
	}

	blob, err := botlogin.Seal(token, nonce)
	if err != nil {
		log.Error("seal login blob", "err", err)
		b.show(ctx, s, failed.pick(locale), homeKeyboard(locale), log)
		return
	}
	tag, err := botlogin.SearchTag(nonce)
	if err != nil {
		log.Error("derive search tag", "err", err)
		b.show(ctx, s, failed.pick(locale), homeKeyboard(locale), log)
		return
	}
	// The app derives the same tag from its own nonce and finds this post with
	// t.me/s/<channel>?q=%23<tag>, instead of relying on the blob still being in the visible tail.
	post := blob + "\n\n#" + tag
	if err := b.tokens.EnqueueLoginPost(ctx, post, time.Now().Add(loginpost.TTL)); err != nil {
		log.Error("enqueue login post", "err", err)
		b.show(ctx, s, failed.pick(locale), homeKeyboard(locale), log)
		return
	}
	if err := b.tokens.DeleteLoginNonce(ctx, user.ID); err != nil {
		log.Debug("delete login nonce", "err", err)
	}

	b.dropMessages(ctx, s.chatID)
	if _, err := b.client.SendHTMLDismissReply(ctx, s.chatID, appCodeLead.pick(locale)); err != nil {
		log.Error("login success notice", "err", err)
	}
}

func (b *bot) finishLoginProbeFail(ctx context.Context, s screen, err error, locale string, log *slog.Logger) {
	var te *tg.Error
	isTG := errors.As(err, &te)
	log.Warn("bot-to-bot probe failed",
		"is_tg_error", isTG,
		"code", tgCode(te, isTG),
		"desc", tgDesc(te, isTG),
		"err", err)
	msg := enrollFailed.pick(locale)
	switch {
	case isTG && te.IsBotToBotDenied():
		log.Warn("bot-to-bot probe denied: user's bot lacks Bot-to-Bot mode", "err", err)
		msg = enrollNeedBotToBot.pick(locale)
	case isTG && te.IsForbidden():
		log.Warn("bot-to-bot probe: unclassified 403, treating as Bot-to-Bot denied", "desc", te.Description)
		msg = enrollNeedBotToBot.pick(locale)
	case isTG && te.IsUnauthorized():
		log.Warn("bot-to-bot probe unauthorized: token revoked or bot gone", "err", err)
		msg = enrollBadToken.pick(locale)
	default:
		log.Error("bot-to-bot probe", "err", err)
	}
	b.clearMenu(ctx, s, log)
	b.promptTokenReply(ctx, s.chatID, msg, log)
}

// probeBotToBot is the ADR 0013 check: can the person's bot message another bot at all? The
// send to init-bot's @username is the whole test. Deleting the ping afterwards is best-effort --
// a stray ping in init-bot's chat is harmless, and failing enrolment because cleanup 400'd would
// lock out a user whose Bot-to-Bot mode is actually fine.
func probeBotToBot(ctx context.Context, token, initUsername string) error {
	c := tg.New(token)
	msg, err := c.SendMessageToUsername(ctx, initUsername, botlogin.PingText)
	if err != nil {
		return err
	}
	if delErr := c.DeleteMessage(ctx, msg.Chat.ID, msg.ID); delErr != nil {
		slog.Debug("probeBotToBot cleanup", "err", delErr)
	}
	return nil
}

func tgCode(e *tg.Error, ok bool) int {
	if !ok {
		return 0
	}
	return e.Code
}

func tgDesc(e *tg.Error, ok bool) string {
	if !ok {
		return ""
	}
	return e.Description
}

func (b *bot) rememberPrompt(chatID, messageID int64) {
	if b.promptID == nil {
		b.promptID = map[int64]int64{}
	}
	b.promptID[chatID] = messageID
}

func (b *bot) dropMessages(ctx context.Context, chatID int64, extra ...int64) {
	ids := extra
	if b.promptID != nil {
		if id := b.promptID[chatID]; id != 0 {
			ids = append(ids, id)
			delete(b.promptID, chatID)
		}
	}
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if err := b.client.DeleteMessage(ctx, chatID, id); err != nil {
			b.log.Debug("delete setup message", "err", err)
		}
	}
}
