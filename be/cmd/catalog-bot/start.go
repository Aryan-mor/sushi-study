package main

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

func registerCatalogCommands(ctx context.Context, client *tg.Client) error {
	cmds := catalogCommands()
	if err := client.SetMyCommands(ctx, cmds, ""); err != nil {
		return err
	}
	return client.SetMyCommands(ctx, cmds, "fa")
}

func (b *bot) handleMessage(ctx context.Context, msg *tg.Message) {
	if msg.From == nil {
		return
	}
	log := b.log.With("chat", msg.Chat.ID)
	user, err := b.identity.EnsureUser(ctx, msg.From.ID, msg.From.Name, msg.From.Username)
	if errors.Is(err, identity.ErrNotAuthorized) {
		b.refuse(ctx, msg.Chat.ID, msg.From, log)
		return
	}
	if err != nil {
		log.Error("ensure user", "err", err)
		return
	}
	if user.Deleted {
		return
	}
	text := strings.TrimSpace(msg.Text)

	if kind, tmdbID, ok := parseTMDBPick(text); ok {
		b.beginTitle(ctx, msg.Chat.ID, kind, tmdbID, log)
		return
	}

	cmd, rest := splitCommand(text)
	switch cmd {
	case "start":
		if kind, tmdbID, ok := tg.ParseCatalogPayload(rest); ok {
			b.beginTitle(ctx, msg.Chat.ID, kind, tmdbID, log)
			return
		}
		b.sendHome(ctx, msg.Chat.ID, log)
	case "search":
		b.sendSearch(ctx, msg.Chat.ID, log)
	case "":
		b.sendHomeQuery(ctx, msg.Chat.ID, text, log)
	default:
		b.sendHome(ctx, msg.Chat.ID, log)
	}
}

// refuse tells a person the gate turned away that the app is not public (ADR 0039).
func (b *bot) refuse(ctx context.Context, chatID int64, from *tg.User, log *slog.Logger) {
	log.Info("registration refused", "tg_user", from.ID)
	if _, err := b.client.SendMessage(ctx, chatID, identity.NotPublicText(from.Language, from.ID)); err != nil {
		log.Warn("refusal", "err", err)
	}
}

// admitted reports whether this person may browse. Search and buttons never create an account, so
// without this a stranger could use the whole catalog without ever passing EnsureUser (ADR 0039).
// A failed check refuses: this gate fails closed.
func (b *bot) admitted(ctx context.Context, tgUserID int64, log *slog.Logger) bool {
	allowed, err := b.identity.Authorized(ctx, tgUserID)
	if err != nil {
		log.Error("registration check failed", "err", err)
		return false
	}
	return allowed
}

func (b *bot) sendHome(ctx context.Context, chatID int64, log *slog.Logger) {
	b.sendHomeQuery(ctx, chatID, "", log)
}

func (b *bot) sendHomeQuery(ctx context.Context, chatID int64, query string, log *slog.Logger) {
	if _, err := b.client.SendKeyboard(ctx, chatID, homeTitle, homeKeyboardQuery(query)); err != nil {
		log.Warn("home", "err", err)
	}
}

func (b *bot) sendSearch(ctx context.Context, chatID int64, log *slog.Logger) {
	kb := tg.InlineKeyboard{Rows: [][]tg.InlineButton{{
		{Text: searchBtn, SwitchInlineQueryCurrentChat: " "},
	}}}
	if _, err := b.client.SendKeyboard(ctx, chatID, searchPrompt, kb); err != nil {
		log.Warn("search prompt", "err", err)
	}
}

func splitCommand(text string) (cmd, rest string) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return "", ""
	}
	raw := strings.ToLower(fields[0])
	if at := strings.IndexByte(raw, '@'); at > 0 {
		raw = raw[:at]
	}
	cmd = strings.TrimPrefix(raw, "/")
	if cmd == raw {
		return "", text
	}
	if len(fields) > 1 {
		rest = strings.Join(fields[1:], " ")
	}
	return cmd, rest
}

func parseTMDBPick(text string) (catalog.Kind, uint64, bool) {
	lower := strings.ToLower(strings.TrimSpace(text))
	const mark = "#tmdb:"
	i := strings.Index(lower, mark)
	if i < 0 {
		return 0, 0, false
	}
	fields := strings.Fields(strings.TrimSpace(text[i+len(mark):]))
	if len(fields) == 0 {
		return 0, 0, false
	}
	return tg.ParseCatalogPayload(strings.Replace(fields[0], ":", "_", 1))
}
