package main

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/newsroom"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

func (b *bot) beginTitle(ctx context.Context, chatID int64, kind catalog.Kind, tmdbID uint64, log *slog.Logger) {
	item := b.loadTitle(ctx, tmdbID, kind, log)
	found, err := b.store.CatalogServeable(ctx, tmdbID, kind)
	if err != nil {
		log.Warn("serveable", "err", err, "tmdb", tmdbID)
		b.sendText(ctx, chatID, busy, homeKeyboard(), log)
		return
	}
	if !found {
		card, inCatalog := b.loadCatalogItem(ctx, tmdbID, kind, log)
		if inCatalog {
			b.showTitle(ctx, chatID, card, missingFiles, requestKeyboard(kind, tmdbID, b.mainUser), log)
			return
		}
		b.showTitle(ctx, chatID, item, missingTitle, requestKeyboard(kind, tmdbID, b.mainUser), log)
		return
	}
	full, ok, err := b.catalogItem(ctx, tmdbID, kind)
	if err != nil || !ok {
		log.Warn("item", "err", err, "tmdb", tmdbID)
		b.sendText(ctx, chatID, busy, homeKeyboard(), log)
		return
	}
	b.showDownloadPrompt(ctx, chatID, 0, full, log)
}

func (b *bot) showDownloadPrompt(ctx context.Context, chatID, msgID int64, item catalog.Item, log *slog.Logger) {
	kb := downloadKeyboard(item.Kind, item.TMDBID, b.mainUser)
	b.deliverCard(ctx, chatID, msgID, item, pickDownload, 0, 0, "", kb, log)
}

func (b *bot) showMovie(ctx context.Context, chatID, msgID int64, item catalog.Item, log *slog.Logger) {
	ep, ok := movieEpisode(item)
	if !ok {
		b.showTitle(ctx, chatID, item, missingFiles, requestKeyboard(item.Kind, item.TMDBID, b.mainUser), log)
		return
	}
	files, err := b.readyFiles(ctx, ep.ID)
	if err != nil {
		log.Warn("files", "err", err, "episode", ep.ID)
		b.sendText(ctx, chatID, busy, homeKeyboard(), log)
		return
	}
	if len(files) == 0 {
		b.showTitle(ctx, chatID, item, missingFiles, requestKeyboard(item.Kind, item.TMDBID, b.mainUser), log)
		return
	}
	kb := qualityKeyboard(files, item.TMDBID, catalog.PersianOrigin(item.Detail.Country))
	b.deliverCard(ctx, chatID, msgID, item, pickQuality, 0, 0, "", kb, log)
}

func (b *bot) showSeasons(ctx context.Context, chatID, msgID int64, item catalog.Item, log *slog.Logger) {
	seasons := seasonNums(item)
	if len(seasons) == 0 {
		b.showTitle(ctx, chatID, item, missingFiles, requestKeyboard(item.Kind, item.TMDBID, b.mainUser), log)
		return
	}
	kb := seasonKeyboard(item.TMDBID, seasons)
	b.deliverCard(ctx, chatID, msgID, item, pickSeason, 0, 0, "", kb, log)
}

func (b *bot) showTitle(ctx context.Context, chatID int64, item catalog.Item, footer string, kb tg.InlineKeyboard, log *slog.Logger) {
	b.deliverCard(ctx, chatID, 0, item, footer, 0, 0, "", kb, log)
}

func (b *bot) sendText(ctx context.Context, chatID int64, text string, kb tg.InlineKeyboard, log *slog.Logger) {
	if _, err := b.client.SendKeyboard(ctx, chatID, text, kb); err != nil {
		log.Warn("send", "err", err)
	}
}

func (b *bot) deliverCard(
	ctx context.Context, chatID, msgID int64, item catalog.Item,
	footer string, season, ep int32, epTitle string, kb tg.InlineKeyboard, log *slog.Logger,
) {
	caption := catalogCaptionAt(item, footer, b.handle(), season, ep, epTitle)
	id := b.sendOrEdit(ctx, chatID, msgID, item.Poster, caption, kb, log)
	b.armOverviewPatch(ctx, item, chatID, id, footer, season, ep, epTitle, kb)
}

func (b *bot) sendOrEdit(ctx context.Context, chatID, msgID int64, poster, caption string, kb tg.InlineKeyboard, log *slog.Logger) int64 {
	if msgID != 0 {
		if err := b.client.EditCaptionKeyboard(ctx, chatID, msgID, caption, kb); err == nil {
			return msgID
		}
		if err := b.client.EditKeyboard(ctx, chatID, msgID, caption, kb); err == nil {
			return msgID
		}
	}
	url := newsroom.PosterURL(poster)
	if url != "" {
		if msg, err := b.client.SendPhotoKeyboard(ctx, chatID, url, caption, kb); err == nil {
			return msg.ID
		}
	}
	msg, err := b.client.SendKeyboard(ctx, chatID, caption, kb)
	if err != nil {
		if log != nil {
			log.Warn("send", "err", err)
		}
		return 0
	}
	return msg.ID
}

func (b *bot) loadTitle(ctx context.Context, tmdbID uint64, kind catalog.Kind, log *slog.Logger) catalog.Item {
	if item, ok, err := b.store.Item(ctx, tmdbID, kind); err != nil {
		log.Debug("load item", "err", err)
	} else if ok {
		return b.withPersian(ctx, b.withIMDB(ctx, item))
	}
	if item, ok, err := b.store.BrowseItem(ctx, tmdbID, kind); err != nil {
		log.Debug("load browse", "err", err)
	} else if ok {
		return b.withPersian(ctx, b.withIMDB(ctx, item))
	}
	if b.tmdb != nil {
		if item, err := b.tmdb.TitlePage(ctx, tmdbID, kind); err == nil && item.Title != "" {
			b.rememberIMDB(item)
			return b.withPersian(ctx, item)
		}
	}
	return catalog.Item{Row: catalog.Row{TMDBID: tmdbID, Kind: kind}}
}

func (b *bot) loadCatalogItem(ctx context.Context, tmdbID uint64, kind catalog.Kind, log *slog.Logger) (catalog.Item, bool) {
	item, ok, err := b.store.Item(ctx, tmdbID, kind)
	if err != nil {
		log.Debug("catalog item", "err", err)
		return catalog.Item{}, false
	}
	if !ok {
		return catalog.Item{}, false
	}
	return b.withPersian(ctx, b.withIMDB(ctx, item)), true
}

func (b *bot) catalogItem(ctx context.Context, tmdbID uint64, kind catalog.Kind) (catalog.Item, bool, error) {
	item, ok, err := b.store.Item(ctx, tmdbID, kind)
	if err != nil || !ok {
		return item, ok, err
	}
	return b.withPersian(ctx, b.withIMDB(ctx, item)), true, nil
}

func (b *bot) withIMDB(ctx context.Context, item catalog.Item) catalog.Item {
	if canonicalIMDB(item.IMDBID) != "" || item.TMDBID == 0 {
		return item
	}
	if id, ok := b.lookupIMDB(item.Kind, item.TMDBID); ok {
		item.IMDBID = id
		return item
	}
	if b.tmdb == nil {
		return item
	}
	page, err := b.tmdb.TitlePage(ctx, item.TMDBID, item.Kind)
	if err != nil || canonicalIMDB(page.IMDBID) == "" {
		return item
	}
	item.IMDBID = page.IMDBID
	b.rememberIMDB(item)
	return item
}

func (b *bot) lookupIMDB(kind catalog.Kind, tmdbID uint64) (string, bool) {
	if b == nil {
		return "", false
	}
	v, ok := b.imdbIDs.Load(imdbCacheKey(kind, tmdbID))
	if !ok {
		return "", false
	}
	id, ok := v.(string)
	if !ok {
		return "", false
	}
	id = canonicalIMDB(id)
	return id, id != ""
}

func (b *bot) rememberIMDB(item catalog.Item) {
	if b == nil {
		return
	}
	id := canonicalIMDB(item.IMDBID)
	if id == "" || item.TMDBID == 0 {
		return
	}
	b.imdbIDs.Store(imdbCacheKey(item.Kind, item.TMDBID), id)
}

func imdbCacheKey(kind catalog.Kind, tmdbID uint64) string {
	return kind.String() + ":" + strconv.FormatUint(tmdbID, 10)
}

func movieEpisode(item catalog.Item) (catalog.Episode, bool) {
	for _, e := range item.Episodes {
		if e.SeasonNo == 0 && e.EpisodeNo == 0 {
			return e, true
		}
	}
	return catalog.Episode{}, false
}

func seasonNums(item catalog.Item) []int32 {
	seen := map[int32]struct{}{}
	var out []int32
	for _, e := range item.Episodes {
		if e.SeasonNo <= 0 {
			continue
		}
		if _, ok := seen[e.SeasonNo]; ok {
			continue
		}
		seen[e.SeasonNo] = struct{}{}
		out = append(out, e.SeasonNo)
	}
	return out
}

func (b *bot) readyFiles(ctx context.Context, episodeID int64) ([]catalog.File, error) {
	files, err := b.store.Files(ctx, episodeID)
	if err != nil {
		return nil, err
	}
	var out []catalog.File
	for _, f := range files {
		if f.State == catalog.FileReady {
			out = append(out, f)
		}
	}
	return out, nil
}
