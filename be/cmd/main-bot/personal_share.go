package main

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/pg"
	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
	"github.com/Aryan-mor/sushi-study/be/internal/app/personal"
)

// Share and claim (ADR 0038). apid queues a Share tap; this pass sends the person a warning and the
// video's link on their own Telegram account -- never to a personal reader bot -- and /start pv_…
// gives whoever opens the link the video.

const (
	shareInterval = 3 * time.Second
	shareBatch    = 10
)

var (
	pvShareWarning = message{
		en: "⚠️ <b>Sharing is one-way.</b>\nOnce you share this video, you can't take access back from anyone who gets the link.\n\nVideo: <b>%s</b>",
		fa: "⚠️ <b>اشتراک‌گذاری یک‌طرفه است.</b>\nبعد از اشتراک‌گذاری دیگه نمی‌تونی دسترسی کسی که لینک رو بهش دادی به این ویدیو رو قطع کنی.\n\nنام فیلم: <b>%s</b>",
	}
	pvShareMessage = message{
		en: "🎬 Shared video: <b>%s</b>\n\nOpen this link to add it to your Sushi:\n%s",
		fa: "🎬 اشتراک‌گذاری فایل: <b>%s</b>\n\nاین لینک رو باز کن تا به سوشی‌ات اضافه بشه:\n%s",
	}
	pvClaimAdded = message{
		en: "✅ <b>%s</b> was added to your videos.\n\nOpen Sushi → <b>My videos</b> in the side menu to watch it.",
		fa: "✅ «<b>%s</b>» به ویدیوهات اضافه شد.\n\nتو سوشی از منوی کناری برو به <b>ویدیوهای من</b> تا ببینیش.",
	}
	pvClaimAlready = message{
		en: "<b>%s</b> is already in your <b>My videos</b>.",
		fa: "«<b>%s</b>» از قبل تو <b>ویدیوهای من</b> هست.",
	}
	pvClaimInvalid = message{
		en: "This share link isn't valid.",
		fa: "این لینک اشتراک‌گذاری معتبر نیست.",
	}
)

// shareQueue is the environment's share request queue (ADR 0038).
type shareQueue interface {
	PendingPersonalShares(ctx context.Context, limit int32) ([]pg.PendingPersonalShare, error)
	MarkPersonalShareSent(ctx context.Context, id int64) error
	NotePersonalShareFailure(ctx context.Context, id int64, reason string) error
}

// watchPersonalShares answers Share taps. Every failure is logged and retried on a later pass, never
// fatal (R-GO-10); the queue closes a request after its third failure.
func (b *bot) watchPersonalShares(ctx context.Context) error {
	if b.sharing == nil || b.shareQueue == nil {
		return nil
	}
	t := time.NewTicker(shareInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			b.sendPendingShares(ctx)
		}
	}
}

func (b *bot) sendPendingShares(ctx context.Context) {
	rows, err := b.shareQueue.PendingPersonalShares(ctx, shareBatch)
	if err != nil {
		b.log.Error("personal shares", "err", err)
		return
	}
	for _, r := range rows {
		log := b.log.With("share", r.ID, "video", r.VideoID)
		if err := b.sendShare(ctx, r); err != nil {
			log.Warn("personal share not sent", "err", err)
			if nerr := b.shareQueue.NotePersonalShareFailure(ctx, r.ID, err.Error()); nerr != nil {
				log.Error("personal share failure note", "err", nerr)
			}
			continue
		}
		if err := b.shareQueue.MarkPersonalShareSent(ctx, r.ID); err != nil {
			log.Error("personal share mark sent", "err", err)
			continue
		}
		log.Info("personal share sent")
	}
}

// sendShare sends the warning, then the forwardable link message, to the person's own chat with
// main-bot: chat id = Telegram user id, whatever the app logged in with.
func (b *bot) sendShare(ctx context.Context, r pg.PendingPersonalShare) error {
	payload, title, err := b.sharing.Link(ctx, r.TGUserID, r.VideoID)
	if errors.Is(err, personal.ErrNoAccess) {
		return err // deleted between the tap and now; the third try closes it
	}
	if err != nil {
		return fmt.Errorf("link: %w", err)
	}
	name := html.EscapeString(title)
	if _, err := b.client.SendHTML(ctx, r.TGUserID, fmt.Sprintf(pvShareWarning.pick(r.Locale), name)); err != nil {
		return fmt.Errorf("warning: %w", err)
	}
	link := fmt.Sprintf("https://t.me/%s?start=%s", b.client.Username(), payload)
	if _, err := b.client.SendHTML(ctx, r.TGUserID, fmt.Sprintf(pvShareMessage.pick(r.Locale), name, link)); err != nil {
		return fmt.Errorf("link message: %w", err)
	}
	return nil
}

// claimShare handles /start pv_…: whoever opens a link gets the video, with the link's title.
func (b *bot) claimShare(ctx context.Context, s screen, user identity.User, payload, locale string, log *slog.Logger) {
	if b.sharing == nil {
		b.show(ctx, s, pvClaimInvalid.pick(locale), tg.InlineKeyboard{}, log)
		return
	}
	out, title, err := b.sharing.Claim(ctx, user.TGUserID, payload)
	if err != nil {
		log.Error("personal claim", "err", err)
		b.show(ctx, s, failed.pick(locale), tg.InlineKeyboard{}, log)
		return
	}
	name := html.EscapeString(title)
	switch out {
	case personal.ClaimAdded:
		log.Info("personal share claimed")
		b.show(ctx, s, fmt.Sprintf(pvClaimAdded.pick(locale), name), tg.InlineKeyboard{}, log)
	case personal.ClaimAlreadyHad:
		b.show(ctx, s, fmt.Sprintf(pvClaimAlready.pick(locale), name), tg.InlineKeyboard{}, log)
	default:
		b.show(ctx, s, pvClaimInvalid.pick(locale), tg.InlineKeyboard{}, log)
	}
}
