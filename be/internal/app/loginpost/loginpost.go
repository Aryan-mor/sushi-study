package loginpost

import (
	"context"
	"log/slog"
	"time"
)

// TTL is how long a posted blob stays on the channel. Building a bot in BotFather can take
// hours, so the QR / channel handoff must still be there when the person comes back. The app
// gives up on the same window. The app finds its post with a #<tag> search (ADR 0029).
const TTL = 8 * time.Hour

// DrainEvery is how often init-bot looks at the outbox.
const DrainEvery = 400 * time.Millisecond

// Store is the outbox. pg.Store satisfies it.
type Store interface {
	PendingLoginPosts(ctx context.Context) ([]Pending, error)
	MarkLoginPostPosted(ctx context.Context, id, channelMessageID int64) error
	LoginPostsToDelete(ctx context.Context) ([]Posted, error)
	DeleteLoginPost(ctx context.Context, id int64) error
}

// Pending is a blob that has not been sent yet.
type Pending struct {
	ID   int64
	Blob string
}

// Posted is a channel message that has passed TTL.
type Posted struct {
	ID               int64
	ChannelMessageID int64
}

// Channel posts and deletes. Implemented in cmd/init-bot over *tg.Client.
type Channel interface {
	Post(ctx context.Context, chatID int64, text string) (int64, error)
	Delete(ctx context.Context, chatID, messageID int64) error
}

// Run drains the outbox until ctx is cancelled. One goroutine; the caller owns the errgroup.
func Run(ctx context.Context, store Store, ch Channel, channelID int64, log *slog.Logger) error {
	tick := time.NewTicker(DrainEvery)
	defer tick.Stop()

	for {
		if err := drainOnce(ctx, store, ch, channelID, log); err != nil {
			log.Warn("login post drain", "err", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

func drainOnce(ctx context.Context, store Store, ch Channel, channelID int64, log *slog.Logger) error {
	pending, err := store.PendingLoginPosts(ctx)
	if err != nil {
		return err
	}
	for _, p := range pending {
		msgID, postErr := ch.Post(ctx, channelID, p.Blob)
		if postErr != nil {
			return postErr
		}
		if markErr := store.MarkLoginPostPosted(ctx, p.ID, msgID); markErr != nil {
			return markErr
		}
		log.Info("login post sent", "id", p.ID)
	}

	old, err := store.LoginPostsToDelete(ctx)
	if err != nil {
		return err
	}
	for _, p := range old {
		if err := ch.Delete(ctx, channelID, p.ChannelMessageID); err != nil {
			log.Debug("login post delete", "err", err)
		}
		if err := store.DeleteLoginPost(ctx, p.ID); err != nil {
			return err
		}
	}
	return nil
}
