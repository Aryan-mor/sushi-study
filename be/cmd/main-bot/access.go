package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
)

// inviteShelf is what the authorize panel needs (R-GO-5, ADR 0039).
type inviteShelf interface {
	GrantInvite(ctx context.Context, tgUserID, invitedBy int64) error
	RevokeInvite(ctx context.Context, tgUserID int64) (bool, error)
	PendingInvites(ctx context.Context) ([]int64, error)
}

const (
	cbMenuAccess        = "m:acc"
	cbAccessAdd         = "acc:add"
	cbAccessCancel      = "acc:x"
	cbAccessRevokePfx   = "acc:rm:"
	accessPlaceholder   = "123456789"
	accessMaxTelegramID = int64(1) << 52 // Bot API ids fit a double; anything larger is a typo.
)

var (
	accessBtn   = message{en: "🔑 Authorize users", fa: "🔑 مجوز کاربران"}
	accessTitle = message{
		en: "🔑 <b>Authorize users</b>\n\nAdd a Telegram ID to let that person register. Tap an ID below to withdraw an invite they have not used yet.",
		fa: "🔑 <b>مجوز کاربران</b>\n\nآیدی تلگرام رو اضافه کن تا اون شخص بتونه ثبت‌نام کنه. برای پس گرفتن دعوتی که هنوز استفاده نشده، روی آیدی پایین بزن.",
	}
	accessModeTesting = message{
		en: "Mode: <b>testing</b>. New users need an invite.",
		fa: "حالت: <b>آزمایشی</b>. کاربر جدید دعوت لازم داره.",
	}
	accessModePublic = message{
		en: "Mode: <b>public</b>. Anyone can register, so invites change nothing.",
		fa: "حالت: <b>عمومی</b>. همه می‌تونن ثبت‌نام کنن، پس دعوت تأثیری نداره.",
	}
	accessAddBtn    = message{en: "➕ Add a user", fa: "➕ افزودن کاربر"}
	accessCancelBtn = message{en: "❌ Cancel", fa: "❌ لغو"}
	accessPrompt    = message{
		en: "Reply with the person's numeric Telegram ID. A refused person sees theirs when they tap /start.",
		fa: "آیدی عددی تلگرام اون شخص رو ریپلای کن. کسی که رد شده با زدن /start آیدی خودش رو می‌بینه.",
	}
	accessBad = message{
		en: "That isn't a Telegram ID. Send only the number, and <b>reply</b> here.",
		fa: "این آیدی تلگرام نیست. فقط عدد رو بفرست و اینجا <b>ریپلای</b> کن.",
	}
	accessGranted = message{en: "✅ Authorized <code>%d</code>.", fa: "✅ به <code>%d</code> مجوز داده شد."}
	accessRevoked = message{en: "Invite withdrawn.", fa: "دعوت پس گرفته شد."}
	// accessWelcome goes to the person, whose language we do not know yet: they have no prefs row.
	accessWelcome = "✅ You can now register. Send /start.\n\n✅ حالا می‌تونی ثبت‌نام کنی. /start رو بفرست."
)

func (b *bot) rememberAccessWait(chatID int64) {
	if b.waitingAccess == nil {
		b.waitingAccess = map[int64]bool{}
	}
	b.waitingAccess[chatID] = true
}

func (b *bot) clearAccessWait(chatID int64) {
	if b.waitingAccess != nil {
		delete(b.waitingAccess, chatID)
	}
}

func (b *bot) awaitingAccess(chatID int64) bool {
	return b.waitingAccess != nil && b.waitingAccess[chatID]
}

// parseTelegramID accepts the bare number a refused person is shown, tolerating what a copy adds.
func parseTelegramID(raw string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 || id >= accessMaxTelegramID {
		return 0, false
	}
	return id, true
}

func accessKeyboard(locale string, pending []int64) tg.InlineKeyboard {
	rows := [][]tg.InlineButton{{{Text: accessAddBtn.pick(locale), Data: cbAccessAdd}}}
	for _, id := range pending {
		rows = append(rows, []tg.InlineButton{{
			Text: fmt.Sprintf("❌ %d", id),
			Data: cbAccessRevokePfx + strconv.FormatInt(id, 10),
		}})
	}
	rows = append(rows, []tg.InlineButton{
		{Text: adminBtnBack.pick(locale), Data: cbMenuAdmin},
		{Text: reqBtnHome.pick(locale), Data: cbMenuHome},
	})
	return tg.InlineKeyboard{Rows: rows}
}

func (b *bot) showAccessPanel(ctx context.Context, s screen, locale string, log *slog.Logger) {
	pending, err := b.invites.PendingInvites(ctx)
	if err != nil {
		log.Error("pending invites", "err", err)
		b.show(ctx, s, failed.pick(locale), staffNavKeyboard(locale), log)
		return
	}
	mode := accessModePublic
	if b.accessClosed {
		mode = accessModeTesting
	}
	text := accessTitle.pick(locale) + "\n\n" + mode.pick(locale)
	b.show(ctx, s, text, accessKeyboard(locale, pending), log)
}

// routeAccessCallback owns the authorize panel. Every button re-checks the role: an old keyboard
// keeps working after someone is demoted, and callback data is not a credential.
func (b *bot) routeAccessCallback(
	ctx context.Context, q *tg.CallbackQuery, s screen, user identity.User, locale string, log *slog.Logger,
) bool {
	if q.Data != cbMenuAccess && q.Data != cbAccessAdd && q.Data != cbAccessCancel &&
		!strings.HasPrefix(q.Data, cbAccessRevokePfx) {
		return false
	}
	if !isAdmin(user.Role) {
		b.toast(ctx, q, reqToastDenied.pick(locale))
		return true
	}
	switch {
	case q.Data == cbMenuAccess:
		b.toast(ctx, q, "")
		b.showAccessPanel(ctx, s, locale, log)
	case q.Data == cbAccessAdd:
		b.toast(ctx, q, "")
		b.startAccessAdd(ctx, s, locale, log)
	case q.Data == cbAccessCancel:
		b.toast(ctx, q, "")
		b.clearAccessWait(s.chatID)
		b.dropMessages(ctx, s.chatID)
		b.showAccessPanel(ctx, s, locale, log)
	default:
		b.revokeInvite(ctx, q, s, user, locale, log)
	}
	return true
}

func (b *bot) revokeInvite(
	ctx context.Context, q *tg.CallbackQuery, s screen, user identity.User, locale string, log *slog.Logger,
) {
	id, ok := parseTelegramID(strings.TrimPrefix(q.Data, cbAccessRevokePfx))
	if !ok {
		b.toast(ctx, q, "")
		return
	}
	if _, err := b.invites.RevokeInvite(ctx, id); err != nil {
		log.Error("revoke invite", "err", err)
		b.toast(ctx, q, failed.pick(locale))
		return
	}
	log.Info("invite revoked", "tg_user", id, "by", user.TGUserID)
	b.toast(ctx, q, accessRevoked.pick(locale))
	b.showAccessPanel(ctx, s, locale, log)
}

func (b *bot) startAccessAdd(ctx context.Context, s screen, locale string, log *slog.Logger) {
	b.clearGeminiWait(s.chatID)
	b.clearIDWait(s.chatID)
	b.clearNewsWait(s.chatID)
	cancel := tg.InlineKeyboard{Rows: [][]tg.InlineButton{{{Text: accessCancelBtn.pick(locale), Data: cbAccessCancel}}}}
	b.show(ctx, s, accessTitle.pick(locale), cancel, log)
	b.sendAccessPrompt(ctx, s.chatID, accessPrompt.pick(locale), locale, log)
}

func (b *bot) sendAccessPrompt(ctx context.Context, chatID int64, text, locale string, log *slog.Logger) {
	msg, err := b.client.SendForceReplyHTML(ctx, chatID, text, accessPlaceholder)
	if err != nil {
		log.Error("access prompt", "err", err)
		b.show(ctx, screen{chatID: chatID}, failed.pick(locale), staffNavKeyboard(locale), log)
		return
	}
	b.rememberPrompt(chatID, msg.ID)
	b.rememberAccessWait(chatID)
}

func (b *bot) handlePastedInvite(ctx context.Context, s screen, user identity.User, in inbound, locale string, log *slog.Logger) {
	b.clearAccessWait(s.chatID)
	b.dropMessages(ctx, s.chatID, in.msgID, in.replyToID)
	if !isAdmin(user.Role) {
		b.show(ctx, s, unknown.pick(locale), homeKeyboardFor(locale, user.Role), log)
		return
	}
	id, ok := parseTelegramID(in.rest)
	if !ok {
		b.sendAccessPrompt(ctx, s.chatID, accessBad.pick(locale), locale, log)
		return
	}
	if err := b.invites.GrantInvite(ctx, id, user.TGUserID); err != nil {
		log.Error("grant invite", "err", err)
		b.show(ctx, screen{chatID: s.chatID}, failed.pick(locale), staffNavKeyboard(locale), log)
		return
	}
	log.Info("invite granted", "tg_user", id, "by", user.TGUserID)
	b.notifyInvited(ctx, id, log)
	b.show(ctx, screen{chatID: s.chatID}, fmt.Sprintf(accessGranted.pick(locale), id), accessDoneKeyboard(locale), log)
}

func accessDoneKeyboard(locale string) tg.InlineKeyboard {
	return tg.InlineKeyboard{Rows: [][]tg.InlineButton{
		{{Text: accessBtn.pick(locale), Data: cbMenuAccess}},
		{{Text: reqBtnHome.pick(locale), Data: cbMenuHome}},
	}}
}

// notifyInvited is best-effort: Telegram refuses a first message to someone who never opened this
// bot, and the invite is good either way.
func (b *bot) notifyInvited(ctx context.Context, tgUserID int64, log *slog.Logger) {
	if _, err := b.client.SendHTML(ctx, tgUserID, accessWelcome); err != nil {
		log.Debug("invite notice not delivered", "tg_user", tgUserID, "err", err)
	}
}

// refuseIfClosed answers a person the gate turned away and reports whether err was that refusal.
// A callback from a group gets a toast only, since the staff inbox must not show the notice.
func (b *bot) refuseIfClosed(ctx context.Context, s screen, from *tg.User, err error, log *slog.Logger) bool {
	if !errors.Is(err, identity.ErrNotAuthorized) {
		return false
	}
	log.Info("registration refused", "tg_user", from.ID)
	if directMessage(s.chatID) {
		b.show(ctx, screen{chatID: s.chatID}, identity.NotPublicText(from.Language, from.ID), tg.InlineKeyboard{}, log)
	}
	return true
}
