package main

import (
	"context"
	"log/slog"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/contentnotify"
	"github.com/Aryan-mor/sushi-study/be/internal/platform/config"
)

// requestForumStore is the three topic ids main-bot created in the requests supergroup.
type requestForumStore interface {
	RequestForumTopics(ctx context.Context) (map[string]int64, error)
	SaveRequestForumTopic(ctx context.Context, name string, threadID int64) error
}

// Telegram topic icon colours. createForumTopic accepts only these six.
const (
	topicBlue  = 7322096
	topicRed   = 16478047
	topicGreen = 9367192
)

type topicSpec struct {
	name  string
	color int
}

func (b *bot) bindRequestForum(
	ctx context.Context, client *tg.Client, store requestForumStore, cfg config.MainBot, log *slog.Logger,
) (contentnotify.ForumTopics, error) {
	if cfg.RequestChannelID == 0 {
		log.Info("CHANNELS names no requests channel; request alerts still DM admins (ADR 0023)")
	}
	topics, err := ensureRequestForum(ctx, client, store, cfg.RequestChannelID, cfg.RequestForum, log)
	if err != nil {
		return contentnotify.ForumTopics{}, err
	}
	b.requestChannelID = cfg.RequestChannelID
	b.requestTopics = topics
	return topics, nil
}

// ensureRequestForum returns the open / reject / done thread ids. REQUEST_TOPICS wins when it
// names all three. Otherwise ids already stored are reused, and any missing topic is created.
// A chat that is not a forum yet logs and returns a disabled set so alerts still post.
func ensureRequestForum(
	ctx context.Context, client *tg.Client, store requestForumStore,
	channelID int64, override config.RequestForum, log *slog.Logger,
) (contentnotify.ForumTopics, error) {
	if channelID == 0 {
		return contentnotify.ForumTopics{}, nil
	}
	if override.Enabled() {
		log.Info("request forum topics from REQUEST_TOPICS",
			"open", override.Open, "reject", override.Reject, "done", override.Done)
		return contentnotify.ForumTopics{Open: override.Open, Reject: override.Reject, Done: override.Done}, nil
	}
	saved, err := store.RequestForumTopics(ctx)
	if err != nil {
		return contentnotify.ForumTopics{}, err
	}
	return fillRequestForum(ctx, client, store, channelID, saved, log)
}

func fillRequestForum(
	ctx context.Context, client *tg.Client, store requestForumStore,
	channelID int64, saved map[string]int64, log *slog.Logger,
) (contentnotify.ForumTopics, error) {
	specs := []topicSpec{{"open", topicBlue}, {"reject", topicRed}, {"done", topicGreen}}
	ids := map[string]int64{}
	for _, spec := range specs {
		id, err := oneRequestTopic(ctx, client, store, channelID, spec, saved[spec.name], log)
		if err != nil {
			return contentnotify.ForumTopics{}, err
		}
		if id == 0 {
			return contentnotify.ForumTopics{}, nil
		}
		ids[spec.name] = id
	}
	topics := contentnotify.ForumTopics{Open: ids["open"], Reject: ids["reject"], Done: ids["done"]}
	log.Info("request forum topics ready", "open", topics.Open, "reject", topics.Reject, "done", topics.Done)
	return topics, nil
}

func oneRequestTopic(
	ctx context.Context, client *tg.Client, store requestForumStore,
	channelID int64, spec topicSpec, saved int64, log *slog.Logger,
) (int64, error) {
	if saved > 0 {
		return saved, nil
	}
	created, err := client.CreateForumTopic(ctx, channelID, spec.name, spec.color)
	if err != nil {
		log.Warn("request forum: create topic", "name", spec.name, "err", err)
		return 0, nil
	}
	if err := store.SaveRequestForumTopic(ctx, spec.name, created.MessageThreadID); err != nil {
		return 0, err
	}
	return created.MessageThreadID, nil
}
