package main

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

func (b *bot) handleCallback(ctx context.Context, q *tg.CallbackQuery) {
	if q.From == nil {
		b.ack(ctx, q.ID)
		return
	}
	chatID, msgID := callbackChat(q)
	log := b.log.With("chat", chatID)
	if !b.admitted(ctx, q.From.ID, log) {
		b.ack(ctx, q.ID)
		b.refuse(ctx, q.From.ID, q.From, log)
		return
	}
	b.dispatchCallback(ctx, q, chatID, msgID, log)
}

func callbackChat(q *tg.CallbackQuery) (chatID, msgID int64) {
	if q.Message != nil {
		return q.Message.Chat.ID, q.Message.ID
	}
	return q.From.ID, 0
}

func (b *bot) ack(ctx context.Context, id string) {
	if err := b.client.AnswerCallback(ctx, id); err != nil {
		b.log.Debug("answer callback", "err", err)
	}
}

func (b *bot) ackText(ctx context.Context, id, text string) {
	if err := b.client.AnswerCallbackText(ctx, id, text); err != nil {
		b.log.Debug("answer callback", "err", err)
	}
}

func (b *bot) dispatchCallback(
	ctx context.Context, q *tg.CallbackQuery, chatID, msgID int64, log *slog.Logger,
) {
	data := q.Data
	switch {
	case data == cbHome:
		b.ack(ctx, q.ID)
		b.sendHome(ctx, chatID, log)
	case data == cbSearch:
		b.ack(ctx, q.ID)
		b.sendSearch(ctx, chatID, log)
	case strings.HasPrefix(data, cbFilePrefix):
		b.ackText(ctx, q.ID, sending)
		b.deliverFile(ctx, chatID, parseIntSuffix(data, cbFilePrefix), log)
	case strings.HasPrefix(data, cbBulkDLPrefix):
		b.ackText(ctx, q.ID, sending)
		go b.onBulkDeliver(context.WithoutCancel(ctx), chatID, strings.TrimPrefix(data, cbBulkDLPrefix), log)
	case strings.HasPrefix(data, cbSeasonPrefix):
		b.ack(ctx, q.ID)
		b.onSeason(ctx, chatID, msgID, strings.TrimPrefix(data, cbSeasonPrefix), log)
	case strings.HasPrefix(data, cbEpisodePrefix):
		b.ack(ctx, q.ID)
		b.onEpisode(ctx, chatID, msgID, strings.TrimPrefix(data, cbEpisodePrefix), log)
	case strings.HasPrefix(data, cbSeasonsPrefix):
		b.ack(ctx, q.ID)
		b.onSeasons(ctx, chatID, msgID, parseUintSuffix(data, cbSeasonsPrefix), log)
	case strings.HasPrefix(data, cbBulkPrefix):
		b.ack(ctx, q.ID)
		b.onBulkPick(ctx, chatID, msgID, strings.TrimPrefix(data, cbBulkPrefix), log)
	case strings.HasPrefix(data, cbRequestPrefix):
		b.ack(ctx, q.ID)
		b.onRequest(ctx, q, strings.TrimPrefix(data, cbRequestPrefix), log)
	case strings.HasPrefix(data, cbDownloadPrefix):
		b.ack(ctx, q.ID)
		b.onDownload(ctx, chatID, msgID, strings.TrimPrefix(data, cbDownloadPrefix), log)
	case strings.HasPrefix(data, cbBackPrefix):
		b.ack(ctx, q.ID)
		b.onBack(ctx, chatID, msgID, strings.TrimPrefix(data, cbBackPrefix), log)
	default:
		b.ack(ctx, q.ID)
	}
}

func (b *bot) onDownload(ctx context.Context, chatID, msgID int64, arg string, log *slog.Logger) {
	tmdbID, kind, ok := parseRequestArg(arg)
	if !ok {
		return
	}
	item, found, err := b.catalogItem(ctx, tmdbID, kind)
	if err != nil || !found {
		b.sendText(ctx, chatID, busy, homeKeyboard(), log)
		return
	}
	if kind == catalog.KindMovie {
		b.showMovie(ctx, chatID, msgID, item, log)
		return
	}
	b.showSeasons(ctx, chatID, msgID, item, log)
}

func (b *bot) onBack(ctx context.Context, chatID, msgID int64, arg string, log *slog.Logger) {
	tmdbID, kind, ok := parseRequestArg(arg)
	if !ok {
		return
	}
	item, found, err := b.catalogItem(ctx, tmdbID, kind)
	if err != nil || !found {
		b.sendText(ctx, chatID, busy, homeKeyboard(), log)
		return
	}
	b.showDownloadPrompt(ctx, chatID, msgID, item, log)
}

func (b *bot) onSeason(ctx context.Context, chatID, msgID int64, arg string, log *slog.Logger) {
	tmdbID, season, page, ok := parseSeasonArg(arg)
	if !ok {
		return
	}
	item, found, err := b.catalogItem(ctx, tmdbID, catalog.KindSeries)
	if err != nil || !found {
		b.sendText(ctx, chatID, busy, homeKeyboard(), log)
		return
	}
	nums := episodeNums(item, season)
	_, pages, safePage := episodePageSlice(nums, page)
	kb := episodeKeyboard(tmdbID, season, nums, safePage)
	b.deliverCard(ctx, chatID, msgID, item, episodeListFooter(season, safePage, pages), season, 0, "", kb, log)
}

func (b *bot) onEpisode(ctx context.Context, chatID, msgID int64, arg string, log *slog.Logger) {
	tmdbID, season, epNo, ok := parseEpisodeArg(arg)
	if !ok {
		return
	}
	item, found, err := b.catalogItem(ctx, tmdbID, catalog.KindSeries)
	if err != nil || !found {
		b.sendText(ctx, chatID, busy, homeKeyboard(), log)
		return
	}
	ep, ok := findEpisode(item, season, epNo)
	if !ok {
		b.sendText(ctx, chatID, missingFiles, homeKeyboard(), log)
		return
	}
	files, err := b.readyFiles(ctx, ep.ID)
	if err != nil || len(files) == 0 {
		b.sendText(ctx, chatID, missingFiles, homeKeyboard(), log)
		return
	}
	nums := episodeNums(item, season)
	kb := episodeQualityKeyboard(files, item.TMDBID, season, epNo, nums, catalog.PersianOrigin(item.Detail.Country))
	b.deliverCard(ctx, chatID, msgID, item, episodePickedFooter(season, epNo), season, epNo, ep.Title, kb, log)
}

func (b *bot) onSeasons(ctx context.Context, chatID, msgID int64, tmdbID uint64, log *slog.Logger) {
	item, found, err := b.catalogItem(ctx, tmdbID, catalog.KindSeries)
	if err != nil || !found {
		b.sendText(ctx, chatID, busy, homeKeyboard(), log)
		return
	}
	b.showSeasons(ctx, chatID, msgID, item, log)
}

func (b *bot) onBulkPick(ctx context.Context, chatID, msgID int64, arg string, log *slog.Logger) {
	tmdbID, season, _, ok := parseSeasonArg(arg)
	if !ok {
		return
	}
	item, found, err := b.catalogItem(ctx, tmdbID, catalog.KindSeries)
	if err != nil || !found {
		b.sendText(ctx, chatID, busy, homeKeyboard(), log)
		return
	}
	labels, samples := b.seasonQualityLabels(ctx, item, season)
	if len(labels) == 0 {
		b.sendText(ctx, chatID, missingFiles, homeKeyboard(), log)
		return
	}
	kb := bulkQualityKeyboard(tmdbID, season, samples, labels, catalog.PersianOrigin(item.Detail.Country))
	b.deliverCard(ctx, chatID, msgID, item, bulkPickFooter(season), season, 0, "", kb, log)
}

func (b *bot) onBulkDeliver(ctx context.Context, chatID int64, arg string, log *slog.Logger) {
	tmdbID, season, idx, ok := parseBulkDeliverArg(arg)
	if !ok {
		return
	}
	item, found, err := b.catalogItem(ctx, tmdbID, catalog.KindSeries)
	if err != nil || !found {
		b.sendText(ctx, chatID, busy, homeKeyboard(), log)
		return
	}
	labels, _ := b.seasonQualityLabels(ctx, item, season)
	if idx < 0 || idx >= len(labels) {
		b.sendText(ctx, chatID, noCopy, homeKeyboard(), log)
		return
	}
	ids := b.seasonFileIDsByLabel(ctx, item, season, labels[idx])
	b.deliverFiles(ctx, chatID, item, ids, true, log)
}

func (b *bot) seasonQualityLabels(ctx context.Context, item catalog.Item, season int32) ([]string, []catalog.File) {
	var grouped [][]catalog.File
	for _, e := range item.Episodes {
		if e.SeasonNo != season {
			continue
		}
		files, err := b.readyFiles(ctx, e.ID)
		if err != nil || len(files) == 0 {
			continue
		}
		grouped = append(grouped, files)
	}
	return distinctQualityLabels(grouped, catalog.PersianOrigin(item.Detail.Country))
}

func pickFileIDByLabel(files []catalog.File, label string, persian bool) int64 {
	picked := int64(0)
	var pickedSize int64
	for _, f := range files {
		if fileQualityLabelAt(f, persian) != label {
			continue
		}
		if picked == 0 || f.SizeBytes > pickedSize {
			picked = f.ID
			pickedSize = f.SizeBytes
		}
	}
	if picked == 0 && len(files) > 0 {
		return files[0].ID
	}
	return picked
}

func (b *bot) seasonFileIDsByLabel(ctx context.Context, item catalog.Item, season int32, label string) []int64 {
	var ids []int64
	persian := catalog.PersianOrigin(item.Detail.Country)
	for _, e := range item.Episodes {
		if e.SeasonNo != season {
			continue
		}
		files, err := b.readyFiles(ctx, e.ID)
		if err != nil || len(files) == 0 {
			continue
		}
		if picked := pickFileIDByLabel(files, label, persian); picked != 0 {
			ids = append(ids, picked)
		}
	}
	return ids
}

func parseIntSuffix(data, prefix string) int64 {
	id, err := strconv.ParseInt(strings.TrimPrefix(data, prefix), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

func parseUintSuffix(data, prefix string) uint64 {
	id, err := strconv.ParseUint(strings.TrimPrefix(data, prefix), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

func parseSeasonArg(arg string) (tmdbID uint64, season int32, page int, ok bool) {
	parts := strings.Split(arg, ":")
	if len(parts) < 2 {
		return 0, 0, 0, false
	}
	id, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil || id == 0 {
		return 0, 0, 0, false
	}
	s, err := strconv.Atoi(parts[1])
	if err != nil || s <= 0 {
		return 0, 0, 0, false
	}
	p := 0
	if len(parts) >= 3 {
		n, err := strconv.Atoi(parts[2])
		if err == nil {
			p = n
		}
	}
	return id, int32(s), p, true
}

func parseEpisodeArg(arg string) (tmdbID uint64, season, ep int32, ok bool) {
	parts := strings.Split(arg, ":")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	id, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil || id == 0 {
		return 0, 0, 0, false
	}
	s, err := strconv.Atoi(parts[1])
	if err != nil || s <= 0 {
		return 0, 0, 0, false
	}
	e, err := strconv.Atoi(parts[2])
	if err != nil || e <= 0 {
		return 0, 0, 0, false
	}
	return id, int32(s), int32(e), true
}

func parseBulkDeliverArg(arg string) (tmdbID uint64, season int32, idx int, ok bool) {
	parts := strings.Split(arg, ":")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	id, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil || id == 0 {
		return 0, 0, 0, false
	}
	s, err := strconv.Atoi(parts[1])
	if err != nil || s <= 0 {
		return 0, 0, 0, false
	}
	n, err := strconv.Atoi(parts[2])
	if err != nil || n < 0 {
		return 0, 0, 0, false
	}
	return id, int32(s), n, true
}

func episodeNums(item catalog.Item, season int32) []int32 {
	var out []int32
	for _, e := range item.Episodes {
		if e.SeasonNo == season {
			out = append(out, e.EpisodeNo)
		}
	}
	return out
}

func findEpisode(item catalog.Item, season, ep int32) (catalog.Episode, bool) {
	for _, e := range item.Episodes {
		if e.SeasonNo == season && e.EpisodeNo == ep {
			return e, true
		}
	}
	return catalog.Episode{}, false
}
