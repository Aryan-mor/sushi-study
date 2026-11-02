package main

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
	"github.com/Aryan-mor/sushi-study/be/internal/app/newsroom"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

// The announce channel's UI half (ADR 0017): the staff preview's approve/reject buttons, and the one
// button that rides on the published post. The prose is in app/newsroom; only Telegram lives here.

// announcement statuses, as stored (db/migrations/env/029). Named here because this is the only
// place that has to tell "still waiting for staff" apart from "somebody already decided".
const statusAwaitingApproval = 2

// newsStore is what the callbacks need. Declared by the consumer (R-GO-5); *pg.Store satisfies it.
type newsStore interface {
	AnnouncementForDecision(ctx context.Context, id int64) (newsroom.Announcement, int16, bool, error)
	MarkAnnouncementPosted(ctx context.Context, id, channelMessageID int64) (bool, error)
	RejectAnnouncement(ctx context.Context, id int64) (bool, error)
	SetWatchLater(ctx context.Context, userID int64, tmdbID uint64, kind catalog.Kind, on bool) error
	Item(ctx context.Context, tmdbID uint64, kind catalog.Kind) (catalog.Item, bool, error)
	BrowseItem(ctx context.Context, tmdbID uint64, kind catalog.Kind) (catalog.Item, bool, error)
	InsertManualAnnouncement(ctx context.Context, d newsroom.Debut) (id int64, inserted bool, err error)
	LiveAnnouncement(ctx context.Context, tmdbID uint64, kind catalog.Kind, season, episode int32) (id int64, status int16, found bool, err error)
	AnnouncementWasPosted(ctx context.Context, tmdbID uint64, kind catalog.Kind, season, episode int32) (bool, error)
	MarkAwaitingApproval(ctx context.Context, id, adminChatID, adminMessageID int64) error
}

// newsPreview adapts *tg.Client to newsroom.Messenger. The reconciler hands over a caption and an
// id; the buttons are built here because a keyboard is a Telegram concept.
type newsPreview struct{ client *tg.Client }

func (p newsPreview) SendPreview(ctx context.Context, chatID int64, prev newsroom.StaffPreview) (int64, error) {
	kb := newsDecisionKeyboard(prev.ID, prev.Locale)
	html, jpeg := newsHeroPreview(ctx, prev)
	if msg, err := p.client.SendRichKeyboard(ctx, chatID, html, kb, jpeg); err == nil {
		return msg.ID, nil
	}
	if len(jpeg) > 0 {
		if msg, err := p.client.SendRichKeyboard(ctx, chatID, prev.Rich, kb, nil); err == nil {
			return msg.ID, nil
		}
	}
	return p.sendPreviewFallback(ctx, chatID, prev, kb)
}

func (p newsPreview) sendPreviewFallback(
	ctx context.Context, chatID int64, prev newsroom.StaffPreview, kb tg.InlineKeyboard,
) (int64, error) {
	if prev.PhotoURL != "" {
		msg, err := p.client.SendPhotoKeyboard(ctx, chatID, prev.PhotoURL, prev.Caption, kb)
		if err == nil {
			return msg.ID, nil
		}
	}
	msg, err := p.client.SendKeyboard(ctx, chatID, prev.Caption, kb)
	if err != nil {
		return 0, err
	}
	return msg.ID, nil
}

func newsDecisionKeyboard(id int64, locale string) tg.InlineKeyboard {
	return tg.InlineKeyboard{Rows: [][]tg.InlineButton{{
		{Text: newsBtnApprove.pick(locale), Data: cbNewsApprovePrefix + strconv.FormatInt(id, 10)},
		{Text: newsBtnReject.pick(locale), Data: cbNewsRejectPrefix + strconv.FormatInt(id, 10)},
	}}}
}

// newsPostKeyboard is what a reader sees under a channel post (ADR 0022). Deep links are t.me —
// Telegram's domain, so R-SEC-14 does not reach them. App + watch-later share the main-bot;
// file download is omitted when no catalog-bot is configured. Two buttons to a row, tinted:
// primary for the app, success for a file. Old posts still carry the nw:l: callback.
func newsPostKeyboard(a newsroom.Announcement, mainUser, catalogUser string) tg.InlineKeyboard {
	var rows [][]tg.InlineButton
	if u := tg.StartURL(mainUser, tg.AppWatchPayload(a.Kind, a.TMDBID)); u != "" {
		rows = append(rows, []tg.InlineButton{{
			Text: newsBtnApp, URL: u, Style: tg.StylePrimary,
		}})
	}

	var actions []tg.InlineButton
	if u := tg.StartURL(catalogUser, tg.CatalogPayload(a.Kind, a.TMDBID)); u != "" {
		actions = append(actions, tg.InlineButton{Text: newsBtnDownload, URL: u, Style: tg.StyleSuccess})
	}
	later := tg.InlineButton{Text: newsBtnLater}
	if u := tg.StartURL(mainUser, tg.WatchLaterPayload(a.Kind, a.TMDBID)); u != "" {
		later.URL = u
	} else {
		later.Data = cbNewsLaterPrefix + strconv.FormatUint(a.TMDBID, 10) + ":" + strconv.Itoa(int(a.Kind))
	}
	actions = append(actions, later)
	rows = append(rows, actions)
	return tg.InlineKeyboard{Rows: rows}
}

// routeNewsCallback owns the nw: prefixes. It answers the callback itself, so handleCallback must
// not pre-answer. Returns false for data it does not own.
func (b *bot) routeNewsCallback(
	ctx context.Context, q *tg.CallbackQuery, user identity.User, locale string, log *slog.Logger,
) bool {
	data := q.Data
	switch {
	case strings.HasPrefix(data, cbNewsLaterPrefix):
		// Any reader may press this one, staff or not — it writes their own state, nobody else's.
		b.addWatchLater(ctx, q, user, strings.TrimPrefix(data, cbNewsLaterPrefix), locale, log)
	case strings.HasPrefix(data, cbNewsApprovePrefix):
		if !b.staffOnly(ctx, q, user, locale) {
			return true
		}
		b.approveAnnouncement(ctx, q, atoi64(strings.TrimPrefix(data, cbNewsApprovePrefix)), locale, log)
	case strings.HasPrefix(data, cbNewsRejectPrefix):
		if !b.staffOnly(ctx, q, user, locale) {
			return true
		}
		b.rejectAnnouncement(ctx, q, atoi64(strings.TrimPrefix(data, cbNewsRejectPrefix)), locale, log)
	case strings.HasPrefix(data, cbNewsKindPrefix):
		if !b.staffOnly(ctx, q, user, locale) {
			return true
		}
		b.pickNewsKind(ctx, q, data, locale, log)
	default:
		return false
	}
	return true
}

func (b *bot) staffOnly(ctx context.Context, q *tg.CallbackQuery, user identity.User, locale string) bool {
	if isAdmin(user.Role) {
		return true
	}
	b.toast(ctx, q, newsToastDenied.pick(locale))
	return false
}

// approveAnnouncement posts to the channel and then records it, in that order: a post nobody knows
// about is recoverable by hand, whereas a row marked posted with nothing in the channel is a title
// that silently never gets announced.
func (b *bot) approveAnnouncement(
	ctx context.Context, q *tg.CallbackQuery, id int64, locale string, log *slog.Logger,
) {
	if b.newsChannelID == 0 {
		b.toast(ctx, q, newsToastNoChannel.pick(locale))
		return
	}
	a, status, found, err := b.news.AnnouncementForDecision(ctx, id)
	if err != nil {
		log.Warn("news approve: load", "err", err, "announcement", id)
		b.toast(ctx, q, newsToastFailed.pick(locale))
		return
	}
	if !found || status != statusAwaitingApproval {
		b.toast(ctx, q, newsToastHandled.pick(locale))
		b.closeNewsPreview(ctx, q, newsToastHandled.pick(locale), log)
		return
	}
	if !a.Manual && !newsroom.FreshAnnouncement(a, time.Now()) {
		if _, rejectErr := b.news.RejectAnnouncement(ctx, id); rejectErr != nil {
			log.Warn("news approve: reject stale", "err", rejectErr, "announcement", id)
		}
		b.toast(ctx, q, newsToastRejected.pick(locale))
		b.closeNewsPreview(ctx, q, newsToastRejected.pick(locale), log)
		return
	}

	msgID, err := b.postAnnouncement(ctx, a)
	if err != nil {
		log.Error("news approve: post", "err", err, "announcement", id)
		b.toast(ctx, q, newsToastFailed.pick(locale))
		return
	}

	if _, err := b.news.MarkAnnouncementPosted(ctx, id, msgID); err != nil {
		// The post is live; only the bookkeeping failed. Say it worked, because it did, and leave
		// the row for the next tick — the live-row index stops it being announced twice.
		log.Error("news approve: mark posted", "err", err, "announcement", id, "message", msgID)
	}
	log.Info("announcement posted", "announcement", id, "tmdb", a.TMDBID, "message", msgID)
	b.toast(ctx, q, newsToastPosted.pick(locale))
	b.closeNewsPreview(ctx, q, newsToastPosted.pick(locale), log)
}

// postAnnouncement sends the Persian post. A title with no poster still gets announced, as text:
// discovery already skips posterless titles, so this only fires when TMDB dropped the image between
// queueing and approval.
func (b *bot) postAnnouncement(ctx context.Context, a newsroom.Announcement) (int64, error) {
	a = b.prepareAnnouncement(ctx, a, b.log)
	kb := newsPostKeyboard(a, b.client.Username(), b.catalogUser)
	html, jpeg := newsHeroPost(ctx, a)
	if msg, err := b.client.SendRichKeyboard(ctx, b.newsChannelID, html, kb, jpeg); err == nil {
		return msg.ID, nil
	}
	if len(jpeg) > 0 {
		if msg, err := b.client.SendRichKeyboard(ctx, b.newsChannelID, newsroom.FormatPost(a), kb, nil); err == nil {
			return msg.ID, nil
		}
	}
	return b.postAnnouncementFallback(ctx, a, kb)
}

// prepareAnnouncement fills catalog gaps from TMDB, then the same country, network, gallery
// and Persian synopsis the reconciler applies before a staff preview.
func (b *bot) prepareAnnouncement(ctx context.Context, a newsroom.Announcement, log *slog.Logger) newsroom.Announcement {
	a = b.fillNewsCard(ctx, a, log)
	a = newsroom.FillCountry(ctx, b.countries, a)
	a = newsroom.FillNetwork(ctx, b.networks, a)
	a = newsroom.FillBackdrops(ctx, b.backdrops, b.gallery, log, a)
	a = newsroom.FillOverview(ctx, b.translate, b.overviews, log, a)
	a.Handle = b.client.Username()
	return a
}

// fillNewsCard loads a title page when the catalog join left the card with no artwork and no
// country — a manual draft of a title this deployment has not ingested yet.
func (b *bot) fillNewsCard(ctx context.Context, a newsroom.Announcement, log *slog.Logger) newsroom.Announcement {
	if b.ids == nil || a.TMDBID == 0 || newsCardHasCatalogExtras(a) {
		return a
	}
	item, err := b.ids.TitlePage(ctx, a.TMDBID, a.Kind)
	if err != nil {
		if log != nil {
			log.Warn("news card tmdb", "err", err, "tmdb", a.TMDBID)
		}
		return a
	}
	if item.TMDBID == 0 {
		return a
	}
	return applyTitlePage(a, item)
}

func newsCardHasCatalogExtras(a newsroom.Announcement) bool {
	return a.Backdrop != "" || a.Logo != "" || catalog.CountryISO(a.Country) != ""
}

func (b *bot) postAnnouncementFallback(
	ctx context.Context, a newsroom.Announcement, kb tg.InlineKeyboard,
) (int64, error) {
	caption := newsroom.FormatCaption(a)
	if url := newsroom.PosterURL(a.Poster); url != "" {
		if msg, err := b.client.SendPhotoKeyboard(ctx, b.newsChannelID, url, caption, kb); err == nil {
			return msg.ID, nil
		}
	}
	msg, err := b.client.SendKeyboard(ctx, b.newsChannelID, caption, kb)
	if err != nil {
		return 0, err
	}
	return msg.ID, nil
}

func (b *bot) rejectAnnouncement(
	ctx context.Context, q *tg.CallbackQuery, id int64, locale string, log *slog.Logger,
) {
	wasPending, err := b.news.RejectAnnouncement(ctx, id)
	if err != nil {
		log.Warn("news reject", "err", err, "announcement", id)
		b.toast(ctx, q, newsToastFailed.pick(locale))
		return
	}
	text := newsToastRejected.pick(locale)
	if !wasPending {
		text = newsToastHandled.pick(locale)
	}
	b.toast(ctx, q, text)
	b.closeNewsPreview(ctx, q, text, log)
}

// closeNewsPreview strips the keyboard off the preview so the decision cannot be pressed twice, and
// leaves a one-line record of what was decided in its place.
func (b *bot) closeNewsPreview(ctx context.Context, q *tg.CallbackQuery, outcome string, log *slog.Logger) {
	if q.Message == nil || q.Message.Chat.ID == 0 {
		return
	}
	chatID, messageID := q.Message.Chat.ID, q.Message.ID
	html := "<p>" + outcome + "</p>"
	if err := b.client.EditRichKeyboard(ctx, chatID, messageID, html, tg.InlineKeyboard{}); err == nil {
		return
	}
	if err := b.client.EditCaptionKeyboard(ctx, chatID, messageID, outcome, tg.InlineKeyboard{}); err == nil {
		return
	}
	if err := b.client.EditKeyboard(ctx, chatID, messageID, outcome, tg.InlineKeyboard{}); err != nil {
		log.Debug("news preview: close", "err", err)
	}
}

// addWatchLater files a title away from the channel post (ADR 0017 §7).
//
// This is the server writing per-user state, which the client otherwise owns (docs/11 §4) — the
// reason ItemRes carries the flag at all, so the title page can read what the device never saw.
func (b *bot) addWatchLater(
	ctx context.Context, q *tg.CallbackQuery, user identity.User, arg, locale string, log *slog.Logger,
) {
	tmdbID, kind, ok := parseNewsTarget(arg)
	if !ok {
		b.toast(ctx, q, newsToastFailed.pick(locale))
		return
	}
	// A reader who has never opened Sushi has no user row to hang the flag on. EnsureUser has
	// already created one by the time we get here, so this only guards the impossible — but an
	// unbound id must never be written as user zero.
	if user.ID == 0 {
		b.toast(ctx, q, newsToastLaterNeedsAccount.pick(locale))
		return
	}
	if !b.allowNewsTap(user.ID) {
		b.toast(ctx, q, newsToastLaterBusy.pick(locale))
		return
	}

	if err := b.news.SetWatchLater(ctx, user.ID, tmdbID, kind, true); err != nil {
		log.Warn("news watch-later", "err", err, "user", user.ID, "tmdb", tmdbID)
		b.toast(ctx, q, newsToastFailed.pick(locale))
		return
	}
	log.Info("watch-later added from channel", "user", user.ID, "tmdb", tmdbID, "kind", kind)
	b.toast(ctx, q, newsToastLaterAdded.pick(locale))
	if q.From != nil {
		b.sendLaterCard(ctx, q.From.ID, tmdbID, kind, locale, log)
	}
}

// parseNewsTarget reads "TMDBID:KIND". A malformed value is refused rather than defaulted: guessing
// the kind would file the wrong title away.
func parseNewsTarget(arg string) (uint64, catalog.Kind, bool) {
	idPart, kindPart, found := strings.Cut(arg, ":")
	if !found {
		return 0, 0, false
	}
	tmdbID, err := strconv.ParseUint(strings.TrimSpace(idPart), 10, 64)
	if err != nil || tmdbID == 0 {
		return 0, 0, false
	}
	k, err := strconv.Atoi(strings.TrimSpace(kindPart))
	if err != nil {
		return 0, 0, false
	}
	kind := catalog.Kind(k)
	if kind != catalog.KindMovie && kind != catalog.KindSeries {
		return 0, 0, false
	}
	return tmdbID, kind, true
}

// allowNewsTap is the R-SEC-11 quota for channel-post taps: a fixed window per user, in memory.
//
// In memory rather than in the database because the thing being protected *is* the database write,
// and because main-bot is one process per environment — a counter that survives a restart would buy
// nothing that matters here. Read and written only from the update loop, which is one goroutine.
func (b *bot) allowNewsTap(userID int64) bool {
	if b.newsTapQuota <= 0 {
		return true
	}
	if b.newsTaps == nil {
		b.newsTaps = map[int64]newsTapWindow{}
	}
	now := time.Now()
	w := b.newsTaps[userID]
	if now.After(w.resetsAt) {
		b.newsTaps[userID] = newsTapWindow{taps: 1, resetsAt: now.Add(time.Hour)}
		return true
	}
	if w.taps >= b.newsTapQuota {
		return false
	}
	w.taps++
	b.newsTaps[userID] = w
	return true
}

type newsTapWindow struct {
	taps     int
	resetsAt time.Time
}

func atoi64(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
