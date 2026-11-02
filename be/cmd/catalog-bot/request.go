package main

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

func (b *bot) onRequest(ctx context.Context, q *tg.CallbackQuery, arg string, log *slog.Logger) {
	tmdbID, kind, ok := parseRequestArg(arg)
	if !ok || q.From == nil {
		return
	}
	user, err := b.identity.EnsureUser(ctx, q.From.ID, q.From.Name, q.From.Username)
	if err != nil || user.ID == 0 {
		log.Warn("request user", "err", err)
		b.sendText(ctx, q.From.ID, busy, homeKeyboard(), log)
		return
	}
	chatID := q.From.ID
	if q.Message != nil {
		chatID = q.Message.Chat.ID
	}
	b.sendText(ctx, chatID, b.placeRequest(ctx, user, tmdbID, kind, log), homeKeyboard(), log)
}

func (b *bot) placeRequest(
	ctx context.Context, user identity.User, tmdbID uint64, kind catalog.Kind, log *slog.Logger,
) string {
	if msg, stop := b.requestGate(ctx, user, tmdbID, kind, log); stop {
		return msg
	}
	title, poster, imdb := b.requestSnapshot(ctx, tmdbID, kind)
	created, err := b.store.InsertRequest(ctx, api.NewRequest{
		UserID: user.ID,
		TMDBID: tmdbID,
		Kind:   kind,
		Title:  title,
		Poster: poster,
		IMDBID: imdb,
	})
	if err != nil {
		log.Warn("request insert", "err", err)
		return busy
	}
	if !created {
		return reqDuplicate
	}
	if kind == catalog.KindSeries {
		if err := b.store.SetFollow(ctx, user.ID, tmdbID, true); err != nil {
			log.Debug("request follow", "err", err)
		}
	}
	return reqAccepted
}

func (b *bot) requestGate(
	ctx context.Context, user identity.User, tmdbID uint64, kind catalog.Kind, log *slog.Logger,
) (string, bool) {
	serveable, err := b.store.CatalogServeable(ctx, tmdbID, kind)
	if err != nil {
		log.Warn("request serveable", "err", err)
		return busy, true
	}
	if serveable {
		return reqAvailable, true
	}
	staff := user.Role == identity.RoleAdmin || user.Role == identity.RoleSuperadmin
	if staff || b.requestQuota <= 0 {
		return "", false
	}
	used, err := b.store.RequestCountSince(ctx, user.ID, time.Now().Add(-24*time.Hour))
	if err != nil {
		log.Warn("request quota", "err", err)
		return busy, true
	}
	if used >= b.requestQuota {
		return reqQuota, true
	}
	return "", false
}

func (b *bot) requestSnapshot(ctx context.Context, tmdbID uint64, kind catalog.Kind) (title, poster, imdb string) {
	if b.tmdb != nil {
		if item, err := b.tmdb.TitlePage(ctx, tmdbID, kind); err == nil && item.Title != "" {
			return item.Title, item.Poster, item.IMDBID
		}
	}
	title, poster, err := b.store.TitleSnapshot(ctx, tmdbID, kind)
	if err != nil {
		return "", "", ""
	}
	return title, poster, ""
}

func parseRequestArg(arg string) (uint64, catalog.Kind, bool) {
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
