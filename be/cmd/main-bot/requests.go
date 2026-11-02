package main

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"strconv"
	"strings"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/adminreq"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

const reqPageSize = 10

// toast answers a button press with a short pop-up (empty text just dismisses the spinner). A
// failed answer is cosmetic — log it and move on.
func (b *bot) toast(ctx context.Context, q *tg.CallbackQuery, text string) {
	if err := b.client.AnswerCallbackText(ctx, q.ID, text); err != nil {
		b.log.Debug("answerCallback", "err", err)
	}
}

func (b *bot) showRequestList(ctx context.Context, s screen, page int, locale string, log *slog.Logger) {
	if page < 0 {
		page = 0
	}
	rows, err := b.requests.PendingRequestPage(ctx, reqPageSize, int32(page*reqPageSize))
	if err != nil {
		log.Warn("request panel: page", "err", err)
		b.show(ctx, s, reqPanelFailed.pick(locale), staffNavKeyboard(locale), log)
		return
	}
	if len(rows) == 0 {
		if page > 0 { // the page emptied out under us — fall back to the first
			b.showRequestList(ctx, s, 0, locale, log)
			return
		}
		b.show(ctx, s, reqPanelTitle.pick(locale)+"\n\n"+reqPanelEmpty.pick(locale),
			staffNavKeyboard(locale), log)
		return
	}

	total, cerr := b.requests.PendingRequestCount(ctx)
	if cerr != nil {
		log.Warn("request panel: count", "err", cerr)
		total = page*reqPageSize + len(rows) // no "next" button, but the page still renders
	}
	first := page*reqPageSize + 1
	last := first + len(rows) - 1

	var text strings.Builder
	fmt.Fprintf(&text, "%s  (%d–%d / %d)", reqPanelTitle.pick(locale), first, last, total)
	kb := tg.InlineKeyboard{}
	for i, r := range rows {
		n := first + i
		fmt.Fprintf(&text, "\n\n%d. %s <b>%s</b>\n%s\n%s",
			n, kindIcon(r.Kind), html.EscapeString(r.Title), idLinks(r), requesterShort(r))
		kb.Rows = append(kb.Rows, []tg.InlineButton{
			{Text: fmt.Sprintf("%s %d", reqBtnReject.pick(locale), n), Data: fmt.Sprintf("%s%d:%d", cbReqRejectPrefix, page, r.ID)},
			{Text: fmt.Sprintf("%s %d", reqBtnRecheck.pick(locale), n), Data: fmt.Sprintf("%s%d:%d", cbReqRecheckPrefix, page, r.ID)},
		})
	}
	if last < total {
		kb.Rows = append(kb.Rows, []tg.InlineButton{
			{Text: reqBtnNextPage.pick(locale), Data: fmt.Sprintf("%s%d", cbReqPagePrefix, page+1)},
		})
	}
	kb.Rows = append(kb.Rows, staffNavKeyboard(locale).Rows...)
	b.show(ctx, s, text.String(), kb, log)
}

func (b *bot) rejectRequest(ctx context.Context, q *tg.CallbackQuery, s screen, id int64, page int, locale string, log *slog.Logger) {
	ok, err := b.requests.RejectRequest(ctx, id)
	switch {
	case err != nil:
		log.Warn("request panel: reject", "err", err, "request", id)
		b.toast(ctx, q, reqPanelFailed.pick(locale))
		return
	case !ok:
		b.toast(ctx, q, reqToastGone.pick(locale))
	default:
		b.toast(ctx, q, reqToastReject.pick(locale))
	}
	b.showRequestList(ctx, s, page, locale, log)
}

func (b *bot) recheckRequest(ctx context.Context, q *tg.CallbackQuery, s screen, id int64, page int, locale string, log *slog.Logger) {
	tmdbID, kind, found, err := b.requests.RequestTarget(ctx, id)
	if err != nil {
		log.Warn("request panel: recheck target", "err", err, "request", id)
		b.toast(ctx, q, reqPanelFailed.pick(locale))
		return
	}
	if !found {
		b.toast(ctx, q, reqToastGone.pick(locale))
		b.showRequestList(ctx, s, page, locale, log)
		return
	}

	serveable, err := b.requests.CatalogServeable(ctx, tmdbID, kind)
	if err != nil {
		log.Warn("request panel: recheck serveable", "err", err, "tmdb", tmdbID)
		b.toast(ctx, q, reqPanelFailed.pick(locale))
		return
	}
	if !serveable {
		b.toast(ctx, q, reqToastPending.pick(locale))
		return
	}

	if _, err := b.requests.MarkTargetFulfilled(ctx, tmdbID, kind); err != nil {
		log.Warn("request panel: recheck mark", "err", err, "tmdb", tmdbID)
		b.toast(ctx, q, reqPanelFailed.pick(locale))
		return
	}
	log.Info("request fulfilled via admin re-check", "tmdb", tmdbID)
	b.toast(ctx, q, reqToastAdded.pick(locale))
	b.showRequestList(ctx, s, page, locale, log)
}

func kindIcon(k catalog.Kind) string {
	if k == catalog.KindSeries {
		return "📺"
	}
	return "🎬"
}

func tmdbPath(k catalog.Kind) string {
	if k == catalog.KindSeries {
		return "tv"
	}
	return "movie"
}

// idLinks is the reference line under a request — "TMDB: 123 · IMDB: tt123", labels linking out
// and ids monospace, so an admin can eyeball exactly what was asked for before rejecting or
// re-checking. IMDB is dropped when TMDB never handed us an id.
func idLinks(r adminreq.Request) string {
	s := fmt.Sprintf(
		"<a href=\"https://www.themoviedb.org/%s/%d\">TMDB</a>: <code>%d</code>",
		tmdbPath(r.Kind), r.TMDBID, r.TMDBID,
	)
	if imdb := strings.TrimSpace(r.IMDBID); imdb != "" {
		e := html.EscapeString(imdb)
		s += fmt.Sprintf(" · <a href=\"https://www.imdb.com/title/%s/\">IMDB</a>: <code>%s</code>", e, e)
	}
	return s
}

// requesterShort names who asked, as briefly as possible: the @username as a t.me link, or the
// display name when there is no username, or the Telegram id only as a last resort. The id is
// left off whenever a handle or a name is available — it just clutters the list.
func requesterShort(r adminreq.Request) string {
	if u := strings.TrimPrefix(strings.TrimSpace(r.RequesterUsername), "@"); u != "" {
		e := html.EscapeString(u)
		return fmt.Sprintf("<a href=\"https://t.me/%s\">@%s</a>", e, e)
	}
	if n := strings.TrimSpace(r.RequesterName); n != "" {
		return html.EscapeString(n)
	}
	return fmt.Sprintf("<code>%d</code>", r.RequesterTG)
}

func atoiSafe(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// parsePageID splits "PAGE:ID"; a malformed value lands on page 0 / id 0 (a no-op reject/recheck).
func parsePageID(s string) (page int, id int64) {
	p, rest, ok := strings.Cut(s, ":")
	if !ok {
		return 0, 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(rest), 10, 64)
	if err != nil {
		return atoiSafe(p), 0
	}
	return atoiSafe(p), n
}
