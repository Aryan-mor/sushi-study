package main

import (
	"context"
	"log/slog"
	"strings"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
)

// Alert-post buttons (ADR 0023). Distinct prefixes from the /admin panel so a press never rewrites
// the channel card into the request list.

func (b *bot) routeRequestCallback(
	ctx context.Context, q *tg.CallbackQuery, s screen, user identity.User, locale string, log *slog.Logger,
) bool {
	data := q.Data
	owned := data == cbMenuRequests ||
		strings.HasPrefix(data, cbReqPagePrefix) ||
		strings.HasPrefix(data, cbReqRejectPrefix) ||
		strings.HasPrefix(data, cbReqRecheckPrefix) ||
		strings.HasPrefix(data, cbReqAlertRejectPrefix) ||
		strings.HasPrefix(data, cbReqAlertRecheckPrefix)
	if !owned {
		return false
	}
	if !isAdmin(user.Role) {
		b.toast(ctx, q, reqToastDenied.pick(locale))
		return true
	}

	switch {
	case data == cbMenuRequests:
		b.toast(ctx, q, "")
		b.showRequestList(ctx, s, 0, locale, log)
	case strings.HasPrefix(data, cbReqPagePrefix):
		b.toast(ctx, q, "")
		b.showRequestList(ctx, s, atoiSafe(strings.TrimPrefix(data, cbReqPagePrefix)), locale, log)
	case strings.HasPrefix(data, cbReqRejectPrefix):
		page, id := parsePageID(strings.TrimPrefix(data, cbReqRejectPrefix))
		b.rejectRequest(ctx, q, s, id, page, locale, log)
	case strings.HasPrefix(data, cbReqRecheckPrefix):
		page, id := parsePageID(strings.TrimPrefix(data, cbReqRecheckPrefix))
		b.recheckRequest(ctx, q, s, id, page, locale, log)
	case strings.HasPrefix(data, cbReqAlertRejectPrefix):
		id := atoi64(strings.TrimPrefix(data, cbReqAlertRejectPrefix))
		b.rejectRequestAlert(ctx, q, id, locale, log)
	case strings.HasPrefix(data, cbReqAlertRecheckPrefix):
		id := atoi64(strings.TrimPrefix(data, cbReqAlertRecheckPrefix))
		b.recheckRequestAlert(ctx, q, id, locale, log)
	}
	return true
}

func (b *bot) rejectRequestAlert(ctx context.Context, q *tg.CallbackQuery, id int64, locale string, log *slog.Logger) {
	ok, err := b.requests.RejectRequest(ctx, id)
	switch {
	case err != nil:
		log.Warn("request alert: reject", "err", err, "request", id)
		b.toast(ctx, q, reqPanelFailed.pick(locale))
		return
	case !ok:
		b.toast(ctx, q, reqToastGone.pick(locale))
	default:
		b.toast(ctx, q, reqToastReject.pick(locale))
	}
	b.closeRequestPost(ctx, q, id, reqToastReject.pick(locale), b.requestTopics.Reject, log)
}

func (b *bot) recheckRequestAlert(ctx context.Context, q *tg.CallbackQuery, id int64, locale string, log *slog.Logger) {
	tmdbID, kind, found, err := b.requests.RequestTarget(ctx, id)
	if err != nil {
		log.Warn("request alert: recheck target", "err", err, "request", id)
		b.toast(ctx, q, reqPanelFailed.pick(locale))
		return
	}
	if !found {
		b.toast(ctx, q, reqToastGone.pick(locale))
		b.closeRequestPost(ctx, q, id, reqToastGone.pick(locale), b.requestTopics.Reject, log)
		return
	}

	serveable, err := b.requests.CatalogServeable(ctx, tmdbID, kind)
	if err != nil {
		log.Warn("request alert: recheck serveable", "err", err, "tmdb", tmdbID)
		b.toast(ctx, q, reqPanelFailed.pick(locale))
		return
	}
	if !serveable {
		b.toast(ctx, q, reqToastPending.pick(locale))
		return
	}

	if _, err := b.requests.MarkTargetFulfilled(ctx, tmdbID, kind); err != nil {
		log.Warn("request alert: recheck mark", "err", err, "tmdb", tmdbID)
		b.toast(ctx, q, reqPanelFailed.pick(locale))
		return
	}
	log.Info("request fulfilled via alert re-check", "tmdb", tmdbID)
	b.toast(ctx, q, reqToastAdded.pick(locale))
	b.closeRequestPost(ctx, q, id, reqToastAdded.pick(locale), b.requestTopics.Done, log)
}

func (b *bot) closeRequestPost(ctx context.Context, q *tg.CallbackQuery, id int64, outcome string, thread int64, log *slog.Logger) {
	if q.Message == nil || q.Message.Chat.ID == 0 {
		return
	}
	chatID, messageID := q.Message.Chat.ID, q.Message.ID
	if thread > 0 && chatID == b.requestChannelID {
		b.moveRequestPost(ctx, chatID, messageID, id, thread, outcome, log)
		return
	}
	empty := tg.InlineKeyboard{Rows: [][]tg.InlineButton{}}
	if err := b.client.EditCaptionKeyboard(ctx, chatID, messageID, outcome, empty); err == nil {
		return
	}
	if err := b.client.EditKeyboard(ctx, chatID, messageID, outcome, empty); err != nil {
		log.Debug("request alert: close", "err", err)
	}
}

func (b *bot) moveRequestPost(ctx context.Context, chatID, messageID, id, thread int64, outcome string, log *slog.Logger) {
	if err := b.client.MoveToForumTopic(ctx, chatID, messageID, thread, outcome); err != nil {
		log.Debug("request alert: move", "err", err, "request", id)
		return
	}
	if err := b.requests.ClearRequestChannelMessage(ctx, id); err != nil {
		log.Warn("request alert: clear channel message", "err", err, "request", id)
	}
}
