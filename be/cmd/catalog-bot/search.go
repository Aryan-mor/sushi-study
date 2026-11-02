package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

const (
	inlineLimit = 8
	inlineDelay = 350 * time.Millisecond
	thumbWidth  = 64
	thumbHeight = 96
)

type inlineDebouncer struct {
	mu      sync.Mutex
	pending map[int64]string
}

func (d *inlineDebouncer) mark(userID int64, queryID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.pending == nil {
		d.pending = map[int64]string{}
	}
	d.pending[userID] = queryID
}

func (d *inlineDebouncer) latest(userID int64, queryID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.pending[userID] == queryID
}

// gateInlineQuery answers a refused person with no results (ADR 0039).
func (b *bot) gateInlineQuery(ctx context.Context, q *tg.InlineQuery) {
	if b.admitted(ctx, q.From.ID, b.log) {
		b.handleInlineQuery(ctx, q)
		return
	}
	if err := b.client.AnswerInlineQuery(ctx, q.ID, nil, 1); err != nil {
		b.log.Warn("refused inline", "err", err)
	}
}

func (b *bot) handleInlineQuery(ctx context.Context, q *tg.InlineQuery) {
	query := strings.TrimSpace(q.Query)
	if kind, tmdbID, ok := parseShareQuery(query); ok {
		if err := b.client.AnswerInlineQuery(ctx, q.ID, b.shareResults(ctx, kind, tmdbID), 1); err != nil {
			b.log.Warn("share inline", "err", err)
		}
		return
	}
	if query == "" {
		if err := b.client.AnswerInlineQuery(ctx, q.ID, nil, 1); err != nil {
			b.log.Warn("empty inline", "err", err)
		}
		return
	}
	b.inline.mark(q.From.ID, q.ID)
	userID := q.From.ID
	queryID := q.ID
	go func() {
		timer := time.NewTimer(inlineDelay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return
		}
		if !b.inline.latest(userID, queryID) {
			return
		}
		runCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		results := b.inlineResults(runCtx, query)
		if !b.inline.latest(userID, queryID) {
			return
		}
		if err := b.client.AnswerInlineQuery(runCtx, queryID, results, 1); err != nil {
			b.log.Warn("inline answer", "err", err)
		}
	}()
}

func (b *bot) handleChosenInline(ctx context.Context, r *tg.ChosenInlineResult) {
	if !b.admitted(ctx, r.From.ID, b.log) {
		return
	}
	if strings.HasPrefix(r.ResultID, "share:") {
		return
	}
	kind, tmdbID, ok := decodeResultID(r.ResultID)
	if !ok {
		return
	}
	b.beginTitle(ctx, r.From.ID, kind, tmdbID, b.log.With("chat", r.From.ID))
}

type inlineHit struct {
	row       catalog.Row
	inCatalog bool
}

func (b *bot) inlineResults(ctx context.Context, query string) []tg.InlineQueryResultArticle {
	hits := b.inlineHits(ctx, query)
	if len(hits) == 0 {
		return nil
	}
	items := make([]catalog.Item, len(hits))
	var wg sync.WaitGroup
	for i := range hits {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			items[i] = b.postedTitle(ctx, hits[i].row)
		}(i)
	}
	wg.Wait()
	out := make([]tg.InlineQueryResultArticle, 0, len(hits))
	for i, hit := range hits {
		out = append(out, b.inlineArticle(items[i], hit.inCatalog))
	}
	return out
}

func (b *bot) inlineHits(ctx context.Context, query string) []inlineHit {
	rows, err := b.store.SearchItems(ctx, query, 0, inlineLimit)
	if err != nil {
		b.log.Warn("catalog search", "err", err)
	}
	hits := make([]inlineHit, 0, inlineLimit)
	seen := map[string]struct{}{}
	for _, row := range rows {
		id := encodeResultID(row.Kind, row.TMDBID)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		hits = append(hits, inlineHit{row: row, inCatalog: true})
		if len(hits) >= inlineLimit {
			return hits
		}
	}
	if b.tmdb == nil || len(hits) >= inlineLimit {
		return hits
	}
	found, err := b.tmdb.SearchTitles(ctx, query)
	if err != nil {
		return hits
	}
	for _, row := range found {
		id := encodeResultID(row.Kind, row.TMDBID)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		hits = append(hits, inlineHit{row: row, inCatalog: false})
		if len(hits) >= inlineLimit {
			break
		}
	}
	return hits
}

func (b *bot) postedTitle(ctx context.Context, row catalog.Row) catalog.Item {
	item := b.loadTitle(ctx, row.TMDBID, row.Kind, b.log)
	if strings.TrimSpace(item.Title) == "" {
		item.Title = row.Title
	}
	if item.Poster == "" {
		item.Poster = row.Poster
	}
	if item.Year == 0 {
		item.Year = row.Year
	}
	if item.Rating == 0 {
		item.Rating = row.Rating
	}
	if item.TMDBID == 0 {
		item.TMDBID = row.TMDBID
	}
	if item.Kind == catalog.KindUnknown {
		item.Kind = row.Kind
	}
	return item
}

func (b *bot) shareResults(ctx context.Context, kind catalog.Kind, tmdbID uint64) []tg.InlineQueryResultArticle {
	if tmdbID == 0 {
		return nil
	}
	item := b.loadTitle(ctx, tmdbID, kind, b.log)
	caption := catalogCaption(item, shareFooter, b.handle())
	kb := shareInlineKeyboard(kind, tmdbID, b.handle(), b.mainUser)
	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = "عنوان"
	}
	icon := "🎬"
	if kind == catalog.KindSeries {
		icon = "📺"
	}
	head := icon + " " + title
	if item.Year > 0 {
		head = fmt.Sprintf("%s %s (%d)", icon, title, item.Year)
	}
	return []tg.InlineQueryResultArticle{
		postedArticle(item, fmt.Sprintf("share:%s", encodeResultID(kind, tmdbID)), "📤 "+head, sharePickDescription(item), caption, kb),
	}
}

func shareInlineKeyboard(kind catalog.Kind, tmdbID uint64, catalogUser, mainUser string) tg.InlineKeyboard {
	var rows [][]tg.InlineButton
	if u := tg.StartURL(mainUser, tg.AppWatchPayload(kind, tmdbID)); u != "" {
		rows = append(rows, []tg.InlineButton{{Text: playInAppBtn, URL: u, Style: tg.StylePrimary}})
	}
	if u := tg.StartURL(catalogUser, tg.CatalogPayload(kind, tmdbID)); u != "" {
		rows = append(rows, []tg.InlineButton{{
			Text:  "📥 دانلود",
			URL:   u,
			Style: tg.StyleSuccess,
		}})
	}
	if u := tg.StartURL(mainUser, tg.WatchLaterPayload(kind, tmdbID)); u != "" {
		rows = append(rows, []tg.InlineButton{{Text: laterBtn, URL: u}})
	}
	if tmdbID > 0 {
		rows = append(rows, []tg.InlineButton{shareButton(kind, tmdbID)})
	}
	return tg.InlineKeyboard{Rows: rows}
}

func (b *bot) inlineArticle(item catalog.Item, inCatalog bool) tg.InlineQueryResultArticle {
	icon := "🎬"
	if item.Kind == catalog.KindSeries {
		icon = "📺"
	}
	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = "عنوان"
	}
	head := icon + " " + title
	if item.Year > 0 {
		head = fmt.Sprintf("%s (%d)", head, item.Year)
	}
	caption := catalogCaption(item, shareFooter, b.handle())
	kb := shareInlineKeyboard(item.Kind, item.TMDBID, b.handle(), b.mainUser)
	return postedArticle(item, encodeResultID(item.Kind, item.TMDBID), head, inlineHitDescription(item.Row, inCatalog), caption, kb)
}

func postedArticle(item catalog.Item, id, title, desc, caption string, kb tg.InlineKeyboard) tg.InlineQueryResultArticle {
	article := tg.InlineQueryResultArticle{
		Type:            "article",
		ID:              id,
		Title:           title,
		Description:     desc,
		ThumbnailURL:    catalog.PosterURL(item.Poster, "w92"),
		ThumbnailWidth:  thumbWidth,
		ThumbnailHeight: thumbHeight,
		InputMessageContent: tg.InlineTextMessage{
			MessageText: caption,
			ParseMode:   "HTML",
		},
	}
	if len(kb.Rows) > 0 {
		article.ReplyMarkup = &kb
	}
	return article
}

const inlineDescMax = 255

func sharePickDescription(item catalog.Item) string {
	if fa := strings.TrimSpace(item.OverviewFa); fa != "" {
		return truncateRunes(fa, inlineDescMax)
	}
	return inlineHitDescription(item.Row, true) + " · بزن تا بفرستی"
}

func inlineHitDescription(row catalog.Row, inCatalog bool) string {
	kind := kindMovie
	if row.Kind == catalog.KindSeries {
		kind = kindSeries
	}
	parts := []string{kind}
	if row.Year > 0 {
		parts = append(parts, fmt.Sprintf("%d", row.Year))
	}
	if row.Rating > 0 {
		parts = append(parts, fmt.Sprintf("⭐ %.1f", float64(row.Rating)/10))
	}
	if inCatalog {
		parts = append(parts, "آماده")
	}
	parts = append(parts, "بزن تا باز بشه")
	return strings.Join(parts, " · ")
}

func encodeResultID(kind catalog.Kind, tmdbID uint64) string {
	letter := "m"
	if kind == catalog.KindSeries {
		letter = "t"
	}
	return fmt.Sprintf("%s:%d", letter, tmdbID)
}

func decodeResultID(id string) (catalog.Kind, uint64, bool) {
	return tg.ParseCatalogPayload(strings.Replace(id, ":", "_", 1))
}
