package main

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"strconv"
	"strings"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/pglib"
	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
	"github.com/Aryan-mor/sushi-study/be/internal/app/personal"
	"github.com/Aryan-mor/sushi-study/be/internal/platform/config"
)

// Personal videos (ADR 0037): a person sends a video here, names it, and watches it in the app
// under My videos. Staff first choose between that and the index probe (ADR 0026).

const (
	cbPersonalSuggestPrefix = "pv:s:" // use the suggested title for this video id
	personalHoldCap         = 100
	// suggestionButtonRunes keeps the one-tap button readable on a phone.
	suggestionButtonRunes = 40
	startUpload           = "upload"
)

var (
	pvChoose = message{
		en: "What should I do with this video?",
		fa: "با این ویدیو چیکار کنم؟",
	}
	pvBtnMine  = message{en: "🎬 Add to My videos", fa: "🎬 اضافه به ویدیوهای من"}
	pvAskTitle = message{
		en: "🎬 What should this video be called?\n\n<b>Reply</b> to this message with a title.",
		fa: "🎬 اسم این ویدیو چی باشه؟\n\nبه همین پیام یه اسم <b>ریپلای</b> کن.",
	}
	pvTitlePlaceholder = message{en: "Title", fa: "اسم ویدیو"}
	pvBtnSuggest       = message{en: "✅ Use “%s”", fa: "✅ همین «%s»"}
	pvSaved            = message{
		en: "✅ Saved as <b>%s</b>.\n\nOpen Sushi → <b>My videos</b> in the side menu to watch it.",
		fa: "✅ با اسم <b>%s</b> ذخیره شد.\n\nتو سوشی از منوی کناری برو به <b>ویدیوهای من</b> تا ببینیش.",
	}
	pvAlready = message{
		en: "This video is already in your <b>My videos</b>.",
		fa: "این ویدیو از قبل تو <b>ویدیوهای من</b> هست.",
	}
	pvOverQuota = message{
		en: "You've reached today's upload limit. Try again tomorrow.",
		fa: "به سقف آپلود امروز رسیدی. فردا دوباره امتحان کن.",
	}
	pvBadTitle = message{
		en: "A title needs 1–120 characters. <b>Reply</b> again with a title.",
		fa: "اسم باید بین ۱ تا ۱۲۰ کاراکتر باشه. دوباره یه اسم <b>ریپلای</b> کن.",
	}
	pvNothingWaiting = message{
		en: "There's no video waiting for a name. Send me a video first.",
		fa: "ویدیویی منتظر اسم نیست. اول یه ویدیو بفرست.",
	}
	pvUploadHelp = message{
		en: "🎬 Send me a video here and I'll add it to <b>My videos</b> in the app.",
		fa: "🎬 ویدیوت رو همینجا بفرست تا به <b>ویدیوهای من</b> تو اپ اضافه‌اش کنم.",
	}
)

// routeVideo adds a sent video to the person's own library, when personal videos are configured.
func (b *bot) routeVideo(
	ctx context.Context, s screen, msg *tg.Message, locale string, log *slog.Logger,
) bool {
	vid, ok := msg.VideoFile()
	if !ok {
		return false
	}
	// A video is not a Gemini-key paste. Leave the wait armed and the next text message is
	// eaten as a bad key.
	b.clearGeminiWait(s.chatID)
	if b.personal == nil {
		return false
	}
	b.receivePersonal(ctx, s, msg, vid, locale, log)
	return true
}

func (b *bot) receivePersonal(ctx context.Context, s screen, msg *tg.Message, vid tg.VideoFile, locale string, log *slog.Logger) {
	if msg.From == nil {
		return
	}
	res, err := b.personal.Receive(ctx, personal.Upload{
		TGUserID:  msg.From.ID,
		ChatID:    msg.Chat.ID,
		MessageID: msg.ID,
		Caption:   msg.Caption,
		Video: personal.Video{
			UniqueID: vid.UniqueID, FileName: vid.FileName, MimeType: vid.MimeType,
			DurationS: vid.DurationS, SizeBytes: vid.SizeBytes,
		},
	})
	if err != nil {
		log.Error("personal intake", "err", err)
		b.show(ctx, s, failed.pick(locale), tg.InlineKeyboard{}, log)
		return
	}
	switch res.Outcome {
	case personal.AlreadyYours:
		b.show(ctx, s, pvAlready.pick(locale), tg.InlineKeyboard{}, log)
	case personal.OverQuota:
		b.show(ctx, s, pvOverQuota.pick(locale), tg.InlineKeyboard{}, log)
	case personal.NeedsTitle:
		log.Info("personal video awaiting title", "video", res.VideoID)
		b.askPersonalTitle(ctx, s.chatID, res.VideoID, res.Suggestion, locale, log)
	}
}

// askPersonalTitle sends the one-tap suggestion (when there is one) and the ForceReply prompt.
func (b *bot) askPersonalTitle(ctx context.Context, chatID, videoID int64, suggestion, locale string, log *slog.Logger) {
	if suggestion != "" {
		label := fmt.Sprintf(pvBtnSuggest.pick(locale), truncateRunes(suggestion, suggestionButtonRunes))
		kb := tg.InlineKeyboard{Rows: [][]tg.InlineButton{
			{{Text: label, Data: cbPersonalSuggestPrefix + strconv.FormatInt(videoID, 10)}},
		}}
		if _, err := b.client.SendKeyboard(ctx, chatID, pvAskTitle.pick(locale), kb); err != nil {
			log.Error("personal suggestion", "err", err)
		}
	}
	prompt := pvAskTitle.pick(locale)
	if suggestion != "" {
		prompt = pvTitlePlaceholder.pick(locale) + " ↓"
	}
	msg, err := b.client.SendForceReplyHTML(ctx, chatID, prompt, pvTitlePlaceholder.pick(locale))
	if err != nil {
		log.Error("personal title prompt", "err", err)
		b.show(ctx, screen{chatID: chatID}, failed.pick(locale), tg.InlineKeyboard{}, log)
		return
	}
	b.rememberPrompt(chatID, msg.ID)
	b.rememberTitleWait(chatID, videoID)
}

func (b *bot) rememberTitleWait(chatID, videoID int64) {
	if b.waitingTitle == nil {
		b.waitingTitle = map[int64]int64{}
	}
	b.waitingTitle[chatID] = videoID
}

func (b *bot) clearTitleWait(chatID int64) {
	if b.waitingTitle != nil {
		delete(b.waitingTitle, chatID)
	}
}

// awaitingTitle reports the video a free-text reply names. After a restart main-bot no longer
// remembers the prompt, so a reply to *any* message falls back to the newest video waiting for
// this person's title (videoID 0).
func (b *bot) awaitingTitle(chatID int64, in inbound) (videoID int64, ok bool) {
	if b.personal == nil || in.cmd != "" {
		return 0, false
	}
	if id, waiting := b.waitingTitle[chatID]; waiting {
		return id, true
	}
	return 0, in.replyToID != 0
}

func (b *bot) handlePastedTitle(
	ctx context.Context, s screen, user identity.User, videoID int64, in inbound, locale string, log *slog.Logger,
) bool {
	named, err := b.personal.Name(ctx, user.TGUserID, videoID, in.rest)
	switch {
	case errors.Is(err, personal.ErrBadTitle):
		b.show(ctx, s, pvBadTitle.pick(locale), tg.InlineKeyboard{}, log)
		return true
	case errors.Is(err, personal.ErrNoPendingTitle):
		if videoID == 0 {
			return false // a reply to something else entirely; not ours
		}
		b.clearTitleWait(s.chatID)
		b.show(ctx, s, pvNothingWaiting.pick(locale), tg.InlineKeyboard{}, log)
		return true
	case err != nil:
		log.Error("personal title", "err", err)
		b.show(ctx, s, failed.pick(locale), tg.InlineKeyboard{}, log)
		return true
	}
	b.clearTitleWait(s.chatID)
	b.dropMessages(ctx, s.chatID)
	title, _ := personal.CleanTitle(in.rest)
	log.Info("personal video named", "video", named)
	b.show(ctx, s, fmt.Sprintf(pvSaved.pick(locale), html.EscapeString(title)), tg.InlineKeyboard{}, log)
	return true
}

func (b *bot) routePersonalCallback(
	ctx context.Context, q *tg.CallbackQuery, s screen, user identity.User, locale string, log *slog.Logger,
) bool {
	switch {
	case strings.HasPrefix(q.Data, cbPersonalSuggestPrefix):
		b.toast(ctx, q, "")
		b.useSuggestion(ctx, q.Data, s, user, locale, log)
		return true
	default:
		return false
	}
}

func (b *bot) useSuggestion(ctx context.Context, data string, s screen, user identity.User, locale string, log *slog.Logger) {
	if b.personal == nil {
		return
	}
	videoID, err := strconv.ParseInt(strings.TrimPrefix(data, cbPersonalSuggestPrefix), 10, 64)
	if err != nil {
		b.show(ctx, s, pvNothingWaiting.pick(locale), tg.InlineKeyboard{}, log)
		return
	}
	title, err := b.personal.UseSuggestion(ctx, user.TGUserID, videoID)
	switch {
	case errors.Is(err, personal.ErrNoPendingTitle):
		b.show(ctx, s, pvNothingWaiting.pick(locale), tg.InlineKeyboard{}, log)
		return
	case err != nil:
		log.Error("personal suggestion", "err", err)
		b.show(ctx, s, failed.pick(locale), tg.InlineKeyboard{}, log)
		return
	}
	if b.waitingTitle[s.chatID] == videoID {
		b.clearTitleWait(s.chatID)
		b.dropMessages(ctx, s.chatID)
	}
	log.Info("personal video named from suggestion", "video", videoID)
	b.show(ctx, s, fmt.Sprintf(pvSaved.pick(locale), html.EscapeString(title)), tg.InlineKeyboard{}, log)
}

// newPersonalIntake is nil when intake is off: no personal_clone in CHANNELS, or
// PERSONAL_INTAKE=off (ADR 0037 §1).
func newPersonalIntake(cfg config.MainBot, lib *pglib.Personal, client *tg.Client, log *slog.Logger) *personal.Service {
	if cfg.PersonalCloneID == 0 {
		log.Info("personal intake off: no personal_clone in CHANNELS, or PERSONAL_INTAKE=off (ADR 0037)")
		return nil
	}
	log.Info("personal intake on", "personal_clone", cfg.PersonalCloneID, "uploads_per_day", cfg.PersonalUploadsPerDay)
	return personal.New(lib, client, cfg.PersonalCloneID, cfg.PersonalUploadsPerDay)
}

func (b *bot) showUploadHelp(ctx context.Context, s screen, user identity.User, locale string, log *slog.Logger) {
	if b.personal == nil {
		b.showHome(ctx, s, user.Role, locale, log)
		return
	}
	b.show(ctx, s, pvUploadHelp.pick(locale), tg.InlineKeyboard{}, log)
}

// truncateRunes keeps at most n runes of s, so a long title cannot overflow a Telegram field.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
