package main

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	adapterllm "github.com/Aryan-mor/sushi-study/be/internal/adapter/llm"
	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/newsroom"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
	"github.com/Aryan-mor/sushi-study/be/internal/platform/config"
)

// captionWatch is one open card whose caption still shows the original synopsis.
// A finished translation edits that message in place.
type captionWatch struct {
	gen     uint64
	chatID  int64
	msgID   int64
	footer  string
	season  int32
	ep      int32
	epTitle string
	kb      tg.InlineKeyboard
	item    catalog.Item
}

// catalogTranslator is the same Persian-synopsis route main-bot uses for news (ADR 0032).
// Unconfigured or broken is not fatal: the caption keeps TMDB's text.
func catalogTranslator(ctx context.Context, cfg config.CatalogBot, log *slog.Logger) newsroom.Translator {
	if !cfg.LLM.Configured() || cfg.TranslateModel == "" {
		log.Info("LLM not configured; catalog captions keep TMDB's synopsis untranslated")
		return nil
	}
	root, err := adapterllm.New(ctx, adapterllm.Config{
		Provider:           cfg.LLM.Provider,
		FallbackModel:      cfg.LLM.FallbackModel,
		VertexProject:      cfg.LLM.VertexProject,
		VertexLocation:     cfg.LLM.VertexLocation,
		VertexSAJSON:       cfg.LLM.VertexSAJSON,
		OpenRouterAPIKey:   cfg.LLM.OpenRouterAPIKey,
		OpenRouterProvider: cfg.LLM.OpenRouterProvider,
	})
	if err != nil {
		log.Warn("catalog translator unavailable; synopses stay untranslated", "err", err)
		return nil
	}
	log.Info("catalog synopses are translated to Persian", "provider", cfg.LLM.Provider, "model", cfg.TranslateModel)
	return newsroom.NewLLMTranslator(root.Named(cfg.TranslateModel))
}

// withPersian prefers a stored Persian synopsis. A miss returns the original text immediately
// and translates in the background; the next open (and any card armed below) reads OverviewFa.
func (b *bot) withPersian(ctx context.Context, item catalog.Item) catalog.Item {
	if b == nil || item.TMDBID == 0 || strings.TrimSpace(item.OverviewFa) != "" {
		return item
	}
	key := imdbCacheKey(item.Kind, item.TMDBID)
	if fa, ok := b.lookupOverviewFa(key); ok {
		item.OverviewFa = fa
		return item
	}
	if b.translate == nil || strings.TrimSpace(item.Overview) == "" {
		return item
	}
	b.ensureOverview(ctx, key, item)
	return item
}

func (b *bot) lookupOverviewFa(key string) (string, bool) {
	b.faMu.Lock()
	defer b.faMu.Unlock()
	fa, ok := b.faCache[key]
	return fa, ok
}

func (b *bot) ensureOverview(ctx context.Context, key string, item catalog.Item) {
	b.faMu.Lock()
	if _, ok := b.faCache[key]; ok {
		b.faMu.Unlock()
		return
	}
	if _, ok := b.faWait[key]; ok {
		b.faMu.Unlock()
		return
	}
	ch := make(chan struct{})
	if b.faWait == nil {
		b.faWait = map[string]chan struct{}{}
	}
	b.faWait[key] = ch
	b.faMu.Unlock()

	go b.finishOverview(context.WithoutCancel(ctx), key, item, ch)
}

func (b *bot) finishOverview(ctx context.Context, key string, item catalog.Item, done chan struct{}) {
	fa := b.askOverviewFa(ctx, item)

	b.faMu.Lock()
	if b.faCache == nil {
		b.faCache = map[string]string{}
	}
	b.faCache[key] = fa
	watches := append([]captionWatch(nil), b.faWatch[key]...)
	delete(b.faWatch, key)
	delete(b.faWait, key)
	b.faMu.Unlock()

	if fa != "" {
		for i := range watches {
			watches[i].item.OverviewFa = fa
			b.editWatched(ctx, watches[i])
		}
	}
	close(done)
}

// overviewReady is closed after the background translation for key has been stored.
func (b *bot) overviewReady(key string) <-chan struct{} {
	b.faMu.Lock()
	defer b.faMu.Unlock()
	if _, ok := b.faCache[key]; ok {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	if ch, ok := b.faWait[key]; ok {
		return ch
	}
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (b *bot) armOverviewPatch(ctx context.Context, item catalog.Item, chatID, msgID int64, footer string, season, ep int32, epTitle string, kb tg.InlineKeyboard) {
	if !b.wantsOverviewPatch(item, msgID) {
		return
	}
	key := imdbCacheKey(item.Kind, item.TMDBID)
	b.faMu.Lock()
	fa, hit := b.faCache[key]
	w := b.nextCaptionWatchLocked(chatID, msgID, footer, season, ep, epTitle, kb, item)
	if !hit {
		b.queueCaptionWatchLocked(key, w)
		b.faMu.Unlock()
		return
	}
	b.faMu.Unlock()
	if strings.TrimSpace(fa) == "" {
		return
	}
	w.item.OverviewFa = fa
	b.editWatched(ctx, w)
}

func (b *bot) wantsOverviewPatch(item catalog.Item, msgID int64) bool {
	if b == nil || msgID == 0 || item.TMDBID == 0 || b.translate == nil {
		return false
	}
	if strings.TrimSpace(item.OverviewFa) != "" || strings.TrimSpace(item.Overview) == "" {
		return false
	}
	return true
}

func (b *bot) nextCaptionWatchLocked(chatID, msgID int64, footer string, season, ep int32, epTitle string, kb tg.InlineKeyboard, item catalog.Item) captionWatch {
	if b.faGen == nil {
		b.faGen = map[string]uint64{}
	}
	id := watchID(chatID, msgID)
	b.faGen[id]++
	return captionWatch{
		gen: b.faGen[id], chatID: chatID, msgID: msgID,
		footer: footer, season: season, ep: ep, epTitle: epTitle, kb: kb, item: item,
	}
}

func (b *bot) queueCaptionWatchLocked(key string, w captionWatch) {
	if b.faWatch == nil {
		b.faWatch = map[string][]captionWatch{}
	}
	list := b.faWatch[key]
	for i := range list {
		if list[i].chatID == w.chatID && list[i].msgID == w.msgID {
			list[i] = w
			b.faWatch[key] = list
			return
		}
	}
	b.faWatch[key] = append(list, w)
}

func (b *bot) editWatched(ctx context.Context, w captionWatch) {
	if b.patchCaption == nil && b.client == nil {
		return
	}
	mu := b.captionMu(w.chatID, w.msgID)
	mu.Lock()
	defer mu.Unlock()

	b.faMu.Lock()
	gen := b.faGen[watchID(w.chatID, w.msgID)]
	b.faMu.Unlock()
	if gen != w.gen {
		return
	}

	caption := catalogCaptionAt(w.item, w.footer, b.handle(), w.season, w.ep, w.epTitle)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if b.patchCaption != nil {
		if err := b.patchCaption(ctx, w.chatID, w.msgID, caption, w.kb); err != nil && b.log != nil {
			b.log.Debug("patch overview", "err", err, "chat", w.chatID, "msg", w.msgID)
		}
		return
	}
	if err := b.client.EditCaptionKeyboard(ctx, w.chatID, w.msgID, caption, w.kb); err == nil {
		return
	} else if err := b.client.EditKeyboard(ctx, w.chatID, w.msgID, caption, w.kb); err != nil && b.log != nil {
		b.log.Debug("patch overview", "err", err, "chat", w.chatID, "msg", w.msgID)
	}
}

func (b *bot) captionMu(chatID, msgID int64) *sync.Mutex {
	v, _ := b.captionMus.LoadOrStore(watchID(chatID, msgID), &sync.Mutex{})
	mu, ok := v.(*sync.Mutex)
	if !ok {
		fresh := &sync.Mutex{}
		b.captionMus.Store(watchID(chatID, msgID), fresh)
		return fresh
	}
	return mu
}

func watchID(chatID, msgID int64) string {
	return strconv.FormatInt(chatID, 10) + ":" + strconv.FormatInt(msgID, 10)
}

func (b *bot) askOverviewFa(ctx context.Context, item catalog.Item) string {
	log := b.log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	filled := newsroom.FillOverview(ctx, b.translate, b.overviews, log, newsroom.Announcement{
		TMDBID:   item.TMDBID,
		Kind:     item.Kind,
		Title:    item.Title,
		Year:     int32(item.Year),
		Genres:   item.Detail.Genres,
		Overview: item.Overview,
	})
	return strings.TrimSpace(filled.OverviewFa)
}
