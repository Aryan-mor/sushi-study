package main

import (
	"context"
	"log/slog"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
	"github.com/Aryan-mor/sushi-study/be/internal/app/newsroom"
	"github.com/Aryan-mor/sushi-study/be/internal/app/personal"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

func isWatchLaterStart(rest string) bool {
	_, _, ok := tg.ParseWatchLaterPayload(rest)
	return ok
}

func isAppWatchStart(rest string) bool {
	_, _, ok := tg.ParseAppWatchPayload(rest)
	return ok
}

func (b *bot) routeStart(
	ctx context.Context, s screen, user identity.User, rest, locale string, log *slog.Logger,
) {
	switch {
	case rest == "login":
		b.startLogin(ctx, s, user, locale, log)
	case isAppCodeStart(rest):
		b.rememberStartNonce(ctx, user, rest, log)
		b.startAppCode(ctx, s, user, locale, log)
	case isDeleteAccountStart(rest):
		b.showDeleteSummary(ctx, s, locale, log)
	case isAiKeyStart(rest):
		b.promptGeminiKey(ctx, s, locale, log)
	case isAppWatchStart(rest):
		b.handleAppWatchStart(ctx, s, user, rest, locale, log)
	case isWatchLaterStart(rest):
		b.handleWatchLaterStart(ctx, s, user, rest, locale, log)
	case isDownloadStart(rest):
		b.showDownload(ctx, s, locale, log)
	case rest == startUpload:
		b.showUploadHelp(ctx, s, user, locale, log)
	case personal.IsSharePayload(rest):
		b.claimShare(ctx, s, user, rest, locale, log)
	default:
		b.showHome(ctx, s, user.Role, locale, log)
	}
}

func (b *bot) handleWatchLaterStart(
	ctx context.Context, s screen, user identity.User, rest, locale string, log *slog.Logger,
) {
	kind, tmdbID, ok := tg.ParseWatchLaterPayload(rest)
	if !ok {
		b.showHome(ctx, s, user.Role, locale, log)
		return
	}
	if user.ID == 0 {
		b.show(ctx, s, newsToastLaterNeedsAccount.pick(locale), homeKeyboardFor(locale, user.Role), log)
		return
	}
	if !b.allowNewsTap(user.ID) {
		b.show(ctx, s, newsToastLaterBusy.pick(locale), homeKeyboardFor(locale, user.Role), log)
		return
	}
	if err := b.news.SetWatchLater(ctx, user.ID, tmdbID, kind, true); err != nil {
		log.Warn("news watch-later start", "err", err, "user", user.ID, "tmdb", tmdbID)
		b.show(ctx, s, newsToastFailed.pick(locale), homeKeyboardFor(locale, user.Role), log)
		return
	}
	log.Info("watch-later added from start", "user", user.ID, "tmdb", tmdbID, "kind", kind)
	b.sendLaterCard(ctx, s.chatID, tmdbID, kind, locale, log)
}

func (b *bot) handleAppWatchStart(
	ctx context.Context, s screen, user identity.User, rest, locale string, log *slog.Logger,
) {
	kind, tmdbID, ok := tg.ParseAppWatchPayload(rest)
	if !ok {
		b.showDownload(ctx, s, locale, log)
		return
	}
	if user.ID != 0 && b.news != nil && b.allowNewsTap(user.ID) {
		if err := b.news.SetWatchLater(ctx, user.ID, tmdbID, kind, true); err != nil {
			log.Warn("news app-watch later", "err", err, "user", user.ID, "tmdb", tmdbID)
		} else {
			log.Info("watch-later added from app-watch", "user", user.ID, "tmdb", tmdbID, "kind", kind)
		}
	}
	b.showDownloadLead(ctx, s, locale, newsAppDl.pick(locale), log)
}

func (b *bot) sendLaterCard(ctx context.Context, chatID int64, tmdbID uint64, kind catalog.Kind, locale string, log *slog.Logger) {
	item := b.loadLaterItem(ctx, tmdbID, kind, log)
	caption := newsroom.FormatLaterAdded(locale, item)
	url := newsroom.PosterURL(item.Poster)
	if url != "" {
		if _, err := b.client.SendPhotoHTML(ctx, chatID, url, caption); err == nil {
			return
		}
	}
	if _, err := b.client.SendHTML(ctx, chatID, caption); err != nil {
		log.Debug("watch-later card", "err", err, "chat", chatID)
	}
}

func (b *bot) loadLaterItem(ctx context.Context, tmdbID uint64, kind catalog.Kind, log *slog.Logger) catalog.Item {
	item, found, err := b.news.Item(ctx, tmdbID, kind)
	if err != nil {
		log.Debug("watch-later item", "err", err, "tmdb", tmdbID)
	}
	if found && item.Title != "" {
		return item
	}
	browse, found, err := b.news.BrowseItem(ctx, tmdbID, kind)
	if err != nil {
		log.Debug("watch-later browse", "err", err, "tmdb", tmdbID)
	}
	if found {
		return browse
	}
	if item.TMDBID == 0 {
		item.TMDBID = tmdbID
		item.Kind = kind
	}
	return item
}
