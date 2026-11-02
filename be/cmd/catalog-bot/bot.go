package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/pg"
	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tmdb"
	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
	"github.com/Aryan-mor/sushi-study/be/internal/app/newsroom"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

type catalogStore interface {
	Item(ctx context.Context, tmdbID uint64, kind catalog.Kind) (catalog.Item, bool, error)
	BrowseItem(ctx context.Context, tmdbID uint64, kind catalog.Kind) (catalog.Item, bool, error)
	Files(ctx context.Context, episodeID int64) ([]catalog.File, error)
	FileOrigin(ctx context.Context, fileID int64) (pg.FileOrigin, bool, error)
	FileCopies(ctx context.Context, fileID int64) ([]pg.FileCopy, error)
	SearchItems(ctx context.Context, query string, offset, limit uint32) ([]catalog.Row, error)
	CatalogServeable(ctx context.Context, tmdbID uint64, kind catalog.Kind) (bool, error)
	TitleSnapshot(ctx context.Context, tmdbID uint64, kind catalog.Kind) (string, string, error)
	InsertRequest(ctx context.Context, r api.NewRequest) (bool, error)
	RequestCountSince(ctx context.Context, userID int64, since time.Time) (int, error)
	SetFollow(ctx context.Context, userID int64, tmdbID uint64, on bool) error
}

type bot struct {
	client       *tg.Client
	store        catalogStore
	identity     *identity.Service
	tmdb         *tmdb.Client
	mainUser     string
	requestQuota int
	log          *slog.Logger
	offset       int64
	inline       inlineDebouncer
	imdbIDs      sync.Map
	translate    newsroom.Translator
	overviews    newsroom.OverviewStore
	faMu         sync.Mutex
	faCache      map[string]string
	faWait       map[string]chan struct{}
	faWatch      map[string][]captionWatch
	faGen        map[string]uint64
	captionMus   sync.Map
	// patchCaption, when set, replaces the Telegram edit used after a background translation.
	patchCaption func(ctx context.Context, chatID, msgID int64, caption string, kb tg.InlineKeyboard) error
}

func (b *bot) poll(ctx context.Context) error {
	b.log.Info("polling", "bot", b.client.Username())
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		updates, err := b.client.GetUpdates(ctx, b.offset, 30*time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			b.log.Warn("getUpdates failed", "err", err)
			select {
			case <-time.After(3 * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}
		b.dispatch(ctx, updates)
	}
}

func (b *bot) dispatch(ctx context.Context, updates []tg.Update) {
	for _, u := range updates {
		if u.ID >= b.offset {
			b.offset = u.ID + 1
		}
		switch {
		case u.InlineQuery != nil:
			b.gateInlineQuery(ctx, u.InlineQuery)
		case u.ChosenInline != nil:
			b.handleChosenInline(ctx, u.ChosenInline)
		case u.Callback != nil:
			b.handleCallback(ctx, u.Callback)
		case u.Message != nil:
			b.handleMessage(ctx, u.Message)
		}
	}
}
