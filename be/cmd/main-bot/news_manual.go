package main

import (
	"context"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
	"github.com/Aryan-mor/sushi-study/be/internal/app/newsroom"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

// Staff draft a channel post from a TMDB id or URL, then approve it with the same
// buttons the reconciler uses. manual on the row skips the 90-day premiere window.

var (
	newsDraftBtn   = message{en: "📰 News post", fa: "📰 پست نیوز"}
	newsDraftTitle = message{
		en: "📰 <b>News post</b>\n\nReply to the next message with a TMDB id or a themoviedb.org link. I'll draft the same channel post, and you approve it before it goes out.",
		fa: "📰 <b>پست نیوز</b>\n\nبه پیام بعدی آیدی TMDB یا لینک themoviedb.org را ریپلای کن. همان پست کانال را می‌سازم؛ قبل از ارسال تأییدش می‌کنی.",
	}
	newsDraftPrompt = message{
		en: "Paste a TMDB number or a themoviedb.org link.",
		fa: "عدد TMDB یا لینک themoviedb.org را بفرست.",
	}
	newsDraftBad = message{
		en: "That doesn't look like a TMDB id. Use a number or a themoviedb.org link, and <b>reply</b> here.",
		fa: "این شبیه آیدی TMDB نیست. عدد یا لینک themoviedb.org بفرست و اینجا <b>ریپلای</b> کن.",
	}
	newsDraftLooking = message{en: "🔄 Drafting the post…", fa: "🔄 دارم پست را می‌سازم…"}
	newsDraftNone    = message{
		en: "TMDB has no movie or series with that id. Reply with another, or cancel.",
		fa: "TMDB فیلم یا سریالی با این آیدی ندارد. یکی دیگر ریپلای کن، یا لغو کن.",
	}
	newsDraftFailed = message{
		en: "😅 Couldn't draft that. Try again in a moment.",
		fa: "😅 نشد. یک لحظه بعد دوباره امتحان کن.",
	}
	newsDraftQueued = message{
		en: "That title is already in the news queue.",
		fa: "این عنوان الان در صف نیوز است.",
	}
	newsDraftWaiting = message{
		en: "A preview of that title is already waiting for approval.",
		fa: "پیش‌نمایش این عنوان الان منتظر تأیید است.",
	}
	newsDraftPosted = message{
		en: "⚠️ Already on the channel. Approve to post it again.",
		fa: "⚠️ قبلاً در کانال منتشر شده. تأیید کنی، دوباره می‌رود.",
	}
	newsDraftLater = message{
		en: "Queued. The preview didn't send — it will show up on the next pass.",
		fa: "در صف ماند. پیش‌نمایش نرفت؛ دور بعد می‌آید.",
	}
	newsDraftReady = message{
		en: "Preview is below. Approve it to post, or skip.",
		fa: "پیش‌نمایش پایین است. تأیید کن تا منتشر شود، یا بی‌خیال.",
	}
	newsDraftPick = message{
		en: "That number is both a movie and a series. Which one?",
		fa: "این عدد هم فیلم است هم سریال. کدام؟",
	}
	newsDraftCancelBtn = message{en: "❌ Cancel", fa: "❌ لغو"}
)

const newsDraftPlaceholder = "550 or themoviedb.org/movie/550"

// Optional slug, then an optional season/episode on a TV url.
var reNewsTMDBURL = regexp.MustCompile(`(?i)themoviedb\.org/(movie|tv)/(\d{1,9})(?:-[^/\s?#]*)?(?:/season/(\d{1,4})/episode/(\d{1,5}))?`)

type newsDraft struct {
	tmdb            uint64
	kind            catalog.Kind
	season, episode int32
}

func (b *bot) rememberNewsWait(chatID int64) {
	if b.waitingNews == nil {
		b.waitingNews = map[int64]bool{}
	}
	b.waitingNews[chatID] = true
}

func (b *bot) clearNewsWait(chatID int64) {
	if b.waitingNews != nil {
		delete(b.waitingNews, chatID)
	}
}

func (b *bot) awaitingNewsDraft(chatID int64) bool {
	return b.waitingNews != nil && b.waitingNews[chatID]
}

func (b *bot) routeNewsDraftCallback(
	ctx context.Context, q *tg.CallbackQuery, s screen, user identity.User, locale string, log *slog.Logger,
) bool {
	switch q.Data {
	case cbMenuNews:
		if !isAdmin(user.Role) {
			b.toast(ctx, q, reqToastDenied.pick(locale))
			return true
		}
		b.toast(ctx, q, "")
		b.startNewsDraft(ctx, s, locale, log)
		return true
	case cbNewsDraftCancel:
		if !isAdmin(user.Role) {
			b.toast(ctx, q, reqToastDenied.pick(locale))
			return true
		}
		b.toast(ctx, q, "")
		b.cancelNewsDraft(ctx, s, user.Role, locale, log)
		return true
	default:
		return false
	}
}

func (b *bot) startNewsDraft(ctx context.Context, s screen, locale string, log *slog.Logger) {
	b.clearGeminiWait(s.chatID)
	b.clearIDWait(s.chatID)
	b.show(ctx, s, newsDraftTitle.pick(locale), newsDraftCancelKeyboard(locale), log)
	b.sendNewsDraftPrompt(ctx, s.chatID, locale, log)
}

func (b *bot) cancelNewsDraft(ctx context.Context, s screen, role, locale string, log *slog.Logger) {
	b.clearNewsWait(s.chatID)
	b.dropMessages(ctx, s.chatID)
	b.showAdminMenu(ctx, s, locale, log)
}

func (b *bot) sendNewsDraftPrompt(ctx context.Context, chatID int64, locale string, log *slog.Logger) {
	msg, err := b.client.SendForceReplyHTML(ctx, chatID, newsDraftPrompt.pick(locale), newsDraftPlaceholder)
	if err != nil {
		log.Error("news draft prompt", "err", err)
		b.show(ctx, screen{chatID: chatID}, newsDraftFailed.pick(locale), newsDraftResultKeyboard(locale), log)
		return
	}
	b.rememberPrompt(chatID, msg.ID)
	b.rememberNewsWait(chatID)
}

func (b *bot) handlePastedNewsDraft(
	ctx context.Context, s screen, user identity.User, in inbound, locale string, log *slog.Logger,
) {
	b.clearNewsWait(s.chatID)
	b.dropMessages(ctx, s.chatID, in.msgID, in.replyToID)
	if !isAdmin(user.Role) {
		b.show(ctx, s, unknown.pick(locale), homeKeyboardFor(locale, user.Role), log)
		return
	}
	draft, ok := parseNewsDraft(in.rest)
	if !ok {
		b.show(ctx, screen{chatID: s.chatID}, newsDraftBad.pick(locale), newsDraftCancelKeyboard(locale), log)
		b.sendNewsDraftPrompt(ctx, s.chatID, locale, log)
		return
	}
	pending, err := b.client.SendKeyboard(ctx, s.chatID, newsDraftLooking.pick(locale), newsDraftCancelKeyboard(locale))
	if err != nil {
		log.Error("news draft pending", "err", err)
		b.show(ctx, screen{chatID: s.chatID}, newsDraftFailed.pick(locale), newsDraftResultKeyboard(locale), log)
		return
	}
	b.finishNewsDraft(ctx, screen{chatID: s.chatID, messageID: pending.ID}, draft, locale, log)
}

func (b *bot) pickNewsKind(ctx context.Context, q *tg.CallbackQuery, data, locale string, log *slog.Logger) {
	draft, ok := parseNewsKindCallback(data)
	if !ok {
		b.toast(ctx, q, newsDraftFailed.pick(locale))
		return
	}
	b.toast(ctx, q, "")
	s := callbackScreen(q)
	b.show(ctx, s, newsDraftLooking.pick(locale), newsDraftCancelKeyboard(locale), log)
	b.finishNewsDraft(ctx, s, draft, locale, log)
}

func (b *bot) finishNewsDraft(ctx context.Context, s screen, draft newsDraft, locale string, log *slog.Logger) {
	items, err := lookupIDs(ctx, b.ids, idQuery{tmdb: draft.tmdb, forced: draft.kind})
	if err != nil {
		log.Error("news draft lookup", "err", err, "tmdb", draft.tmdb)
		b.show(ctx, s, newsDraftFailed.pick(locale), newsDraftResultKeyboard(locale), log)
		return
	}
	switch len(items) {
	case 0:
		b.show(ctx, s, newsDraftNone.pick(locale), newsDraftCancelKeyboard(locale), log)
		b.sendNewsDraftPrompt(ctx, s.chatID, locale, log)
	case 1:
		b.commitNewsDraft(ctx, s, locale, items[0], draft.season, draft.episode, log)
	default:
		b.show(ctx, s, newsDraftPick.pick(locale), newsKindKeyboard(locale, items, draft.season, draft.episode), log)
	}
}

func (b *bot) commitNewsDraft(
	ctx context.Context, s screen, locale string, item catalog.Item, season, episode int32, log *slog.Logger,
) {
	if b.news == nil {
		b.show(ctx, s, newsDraftFailed.pick(locale), newsDraftResultKeyboard(locale), log)
		return
	}
	sn, ep := liveSeasonEpisode(item, season, episode)
	repost, postedErr := b.news.AnnouncementWasPosted(ctx, item.TMDBID, item.Kind, sn, ep)
	if postedErr != nil {
		log.Warn("news draft posted-before", "err", postedErr, "tmdb", item.TMDBID)
	}
	id, inserted, err := b.news.InsertManualAnnouncement(ctx, debutFromItem(item, season, episode))
	if err != nil {
		log.Error("news draft queue", "err", err, "tmdb", item.TMDBID)
		b.show(ctx, s, newsDraftFailed.pick(locale), newsDraftResultKeyboard(locale), log)
		return
	}
	if !inserted {
		b.showNewsDraftBlocked(ctx, s, locale, item, season, episode, log)
		return
	}
	b.sendNewsDraft(ctx, s, locale, announcementFromItem(id, item, season, episode), repost, log)
}

func (b *bot) showNewsDraftBlocked(
	ctx context.Context, s screen, locale string, item catalog.Item, season, episode int32, log *slog.Logger,
) {
	sn, ep := liveSeasonEpisode(item, season, episode)
	_, status, found, err := b.news.LiveAnnouncement(ctx, item.TMDBID, item.Kind, sn, ep)
	if err != nil || !found {
		log.Error("news draft live", "err", err, "tmdb", item.TMDBID, "found", found)
		b.show(ctx, s, newsDraftFailed.pick(locale), newsDraftResultKeyboard(locale), log)
		return
	}
	b.show(ctx, s, newsDraftBlockedText(locale, status), newsDraftResultKeyboard(locale), log)
}

func newsDraftBlockedText(locale string, status int16) string {
	if status == statusAwaitingApproval {
		return newsDraftWaiting.pick(locale)
	}
	return newsDraftQueued.pick(locale)
}

func (b *bot) sendNewsDraft(
	ctx context.Context, s screen, locale string, a newsroom.Announcement, repost bool, log *slog.Logger,
) {
	a = b.prepareAnnouncement(ctx, a, log)
	msgID, err := (newsPreview{client: b.client}).SendPreview(ctx, s.chatID, newsroom.StaffPreview{
		ID:           a.ID,
		PhotoURL:     newsroom.PosterURL(a.Poster),
		Rich:         appendRepostWarning(newsroom.FormatPreview(a, locale), locale, repost, true),
		Caption:      appendRepostWarning(newsroom.FormatPreviewCaption(a, locale), locale, repost, false),
		Locale:       locale,
		Announcement: a,
	})
	if err != nil {
		log.Error("news draft preview", "err", err, "announcement", a.ID)
		b.show(ctx, s, newsDraftLater.pick(locale), newsDraftResultKeyboard(locale), log)
		return
	}
	if err := b.news.MarkAwaitingApproval(ctx, a.ID, s.chatID, msgID); err != nil {
		log.Error("news draft mark", "err", err, "announcement", a.ID)
	}
	log.Info("news draft ready", "announcement", a.ID, "tmdb", a.TMDBID, "repost", repost)
	b.show(ctx, s, newsDraftReadyText(locale, repost), newsDraftResultKeyboard(locale), log)
}

func newsDraftReadyText(locale string, repost bool) string {
	text := newsDraftReady.pick(locale)
	if !repost {
		return text
	}
	return text + "\n\n" + newsDraftPosted.pick(locale)
}

func appendRepostWarning(body, locale string, repost, rich bool) string {
	if !repost {
		return body
	}
	note := newsDraftPosted.pick(locale)
	if rich {
		return body + "\n<p>" + note + "</p>"
	}
	return body + "\n\n" + note
}

func newsDraftCancelKeyboard(locale string) tg.InlineKeyboard {
	return tg.InlineKeyboard{Rows: [][]tg.InlineButton{
		{{Text: newsDraftCancelBtn.pick(locale), Data: cbNewsDraftCancel}},
	}}
}

func newsDraftResultKeyboard(locale string) tg.InlineKeyboard {
	return tg.InlineKeyboard{Rows: [][]tg.InlineButton{
		{{Text: newsDraftBtn.pick(locale), Data: cbMenuNews}},
		{
			{Text: adminBtnBack.pick(locale), Data: cbMenuAdmin},
			{Text: reqBtnHome.pick(locale), Data: cbMenuHome},
		},
	}}
}

func newsKindKeyboard(locale string, items []catalog.Item, season, episode int32) tg.InlineKeyboard {
	rows := make([][]tg.InlineButton, 0, len(items)+1)
	for _, item := range items {
		label := kindWord(item.Kind, locale)
		if t := strings.TrimSpace(item.Title); t != "" {
			label = kindIcon(item.Kind) + " " + t
		}
		rows = append(rows, []tg.InlineButton{{
			Text: label,
			Data: newsKindData(item.Kind, item.TMDBID, season, episode),
		}})
	}
	rows = append(rows, []tg.InlineButton{{Text: newsDraftCancelBtn.pick(locale), Data: cbNewsDraftCancel}})
	return tg.InlineKeyboard{Rows: rows}
}

func newsKindData(kind catalog.Kind, id uint64, season, episode int32) string {
	base := cbNewsKindPrefix + strconv.Itoa(int(kind)) + ":" + strconv.FormatUint(id, 10)
	if season > 0 && episode > 0 {
		base += ":" + strconv.FormatInt(int64(season), 10) + ":" + strconv.FormatInt(int64(episode), 10)
	}
	return base
}

func parseNewsDraft(raw string) (newsDraft, bool) {
	s := strings.TrimSpace(raw)
	if s == "" || reIDsIMDBURL.MatchString(s) || reIDsIMDBBare.MatchString(s) {
		return newsDraft{}, false
	}
	if m := reNewsTMDBURL.FindStringSubmatch(s); len(m) == 5 {
		return newsDraftFromURL(m)
	}
	if m := reIDsTMDBTag.FindStringSubmatch(s); len(m) == 2 {
		return newsDraftID(m[1], catalog.KindUnknown)
	}
	return newsDraftID(s, catalog.KindUnknown)
}

func newsDraftFromURL(m []string) (newsDraft, bool) {
	d, ok := newsDraftID(m[2], catalog.KindMovie)
	if !ok {
		return newsDraft{}, false
	}
	if strings.EqualFold(m[1], "tv") {
		d.kind = catalog.KindSeries
	}
	if m[3] == "" || m[4] == "" || d.kind != catalog.KindSeries {
		return d, true
	}
	sn, errS := strconv.ParseInt(m[3], 10, 32)
	ep, errE := strconv.ParseInt(m[4], 10, 32)
	if errS != nil || errE != nil || sn <= 0 || ep <= 0 {
		return d, true
	}
	d.season, d.episode = int32(sn), int32(ep)
	return d, true
}

func newsDraftID(raw string, kind catalog.Kind) (newsDraft, bool) {
	id, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil || id == 0 || id > 999_999_999 {
		return newsDraft{}, false
	}
	return newsDraft{tmdb: id, kind: kind}, true
}

func parseNewsKindCallback(data string) (newsDraft, bool) {
	parts := strings.Split(strings.TrimPrefix(data, cbNewsKindPrefix), ":")
	if len(parts) != 2 && len(parts) != 4 {
		return newsDraft{}, false
	}
	k, err := strconv.Atoi(parts[0])
	if err != nil {
		return newsDraft{}, false
	}
	kind := catalog.Kind(k)
	if kind != catalog.KindMovie && kind != catalog.KindSeries {
		return newsDraft{}, false
	}
	d, ok := newsDraftID(parts[1], kind)
	if !ok || len(parts) == 2 {
		return d, ok
	}
	sn, errS := strconv.ParseInt(parts[2], 10, 32)
	ep, errE := strconv.ParseInt(parts[3], 10, 32)
	if errS != nil || errE != nil || sn <= 0 || ep <= 0 {
		return newsDraft{}, false
	}
	d.season, d.episode = int32(sn), int32(ep)
	return d, true
}

func debutFromItem(item catalog.Item, season, episode int32) newsroom.Debut {
	sn, ep := liveSeasonEpisode(item, season, episode)
	return newsroom.Debut{
		TMDBID:           item.TMDBID,
		Kind:             item.Kind,
		Title:            item.Title,
		Season:           sn,
		Episode:          ep,
		Poster:           item.Poster,
		Genres:           item.Detail.Genres,
		Overview:         item.Overview,
		RuntimeS:         item.Detail.RuntimeS,
		Year:             int32(item.Year),
		Rating:           int32(item.Rating),
		PremiereOn:       item.ReleasedOn,
		FirstAvailableAt: time.Now(),
		Manual:           true,
	}
}

func announcementFromItem(id int64, item catalog.Item, season, episode int32) newsroom.Announcement {
	sn, ep := liveSeasonEpisode(item, season, episode)
	return applyTitlePage(newsroom.Announcement{
		ID:      id,
		TMDBID:  item.TMDBID,
		Kind:    item.Kind,
		Season:  sn,
		Episode: ep,
		Manual:  true,
	}, item)
}

func liveSeasonEpisode(item catalog.Item, season, episode int32) (int32, int32) {
	if item.Kind != catalog.KindSeries || season <= 0 || episode <= 0 {
		return 0, 0
	}
	return season, episode
}

func applyTitlePage(a newsroom.Announcement, item catalog.Item) newsroom.Announcement {
	d := item.Detail
	if a.Title == "" {
		a.Title = item.Title
	}
	if a.Overview == "" {
		a.Overview = item.Overview
	}
	if a.Poster == "" {
		a.Poster = item.Poster
	}
	if a.Genres == "" {
		a.Genres = d.Genres
	}
	if a.Year == 0 {
		a.Year = int32(item.Year)
	}
	if a.Rating == 0 {
		a.Rating = int32(item.Rating)
	}
	if a.RuntimeS == 0 {
		a.RuntimeS = d.RuntimeS
	}
	if a.PremiereOn.IsZero() {
		a.PremiereOn = item.ReleasedOn
	}
	if a.Backdrop == "" {
		a.Backdrop = d.Backdrop
	}
	if a.Logo == "" {
		a.Logo = d.Logo
	}
	if a.Country == "" {
		a.Country = d.Country
	}
	if a.Network == "" {
		a.Network = d.Network
	}
	if a.TrailerKey == "" {
		a.TrailerKey = d.TrailerKey
	}
	if !a.BackdropsKnown {
		a.Backdrops = d.Backdrops
		a.BackdropsKnown = d.Backdrops != nil || d.Backdrop != ""
	}
	return a
}
