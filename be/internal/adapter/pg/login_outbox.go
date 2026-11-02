package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Aryan-mor/sushi-study/be/internal/app/loginpost"
)

// PutLoginNonce stores the app's 16-byte start nonce until the channel blob is queued.
func (s *Store) PutLoginNonce(ctx context.Context, userID int64, nonce []byte) error {
	if err := s.q.UpsertLoginNonce(ctx, UpsertLoginNonceParams{UserID: userID, Nonce: nonce}); err != nil {
		return fmt.Errorf("put login nonce: %w", err)
	}
	return nil
}

// LoginNonce loads the stored start nonce, if any.
func (s *Store) LoginNonce(ctx context.Context, userID int64) ([]byte, bool, error) {
	nonce, err := s.q.LoginNonceByUser(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("login nonce: %w", err)
	}
	if len(nonce) == 0 {
		return nil, false, nil
	}
	return nonce, true, nil
}

// DeleteLoginNonce drops the start nonce after the outbox row is written.
func (s *Store) DeleteLoginNonce(ctx context.Context, userID int64) error {
	if err := s.q.DeleteLoginNonce(ctx, userID); err != nil {
		return fmt.Errorf("delete login nonce: %w", err)
	}
	return nil
}

// EnqueueLoginPost queues the exact text to post: a sealed s2. blob plus its public #<tag>
// search hashtag (botlogin.SearchTag). Never a BotFather token in the clear.
func (s *Store) EnqueueLoginPost(ctx context.Context, blob string, deleteAfter time.Time) error {
	if err := s.q.EnqueueLoginPost(ctx, EnqueueLoginPostParams{
		Blob: blob,
		DeleteAfter: pgtype.Timestamptz{
			Time:  deleteAfter,
			Valid: true,
		},
	}); err != nil {
		return fmt.Errorf("enqueue login post: %w", err)
	}
	return nil
}

// PendingLoginPosts is the unsent outbox (init-bot drain).
func (s *Store) PendingLoginPosts(ctx context.Context) ([]loginpost.Pending, error) {
	rows, err := s.q.PendingLoginPosts(ctx)
	if err != nil {
		return nil, fmt.Errorf("pending login posts: %w", err)
	}
	out := make([]loginpost.Pending, len(rows))
	for i, r := range rows {
		out[i] = loginpost.Pending{ID: r.ID, Blob: r.Blob}
	}
	return out, nil
}

// MarkLoginPostPosted records the Telegram message id after a successful send.
func (s *Store) MarkLoginPostPosted(ctx context.Context, id, channelMessageID int64) error {
	mid := channelMessageID
	if err := s.q.MarkLoginPostPosted(ctx, MarkLoginPostPostedParams{
		ID:               id,
		ChannelMessageID: &mid,
	}); err != nil {
		return fmt.Errorf("mark login post posted: %w", err)
	}
	return nil
}

// LoginPostsToDelete is the TTL expiry set (init-bot drain).
func (s *Store) LoginPostsToDelete(ctx context.Context) ([]loginpost.Posted, error) {
	rows, err := s.q.LoginPostsToDelete(ctx)
	if err != nil {
		return nil, fmt.Errorf("login posts to delete: %w", err)
	}
	out := make([]loginpost.Posted, 0, len(rows))
	for _, r := range rows {
		if r.ChannelMessageID == nil {
			continue
		}
		out = append(out, loginpost.Posted{ID: r.ID, ChannelMessageID: *r.ChannelMessageID})
	}
	return out, nil
}

// DeleteLoginPost removes an outbox row after the channel message is gone (or failed to delete).
func (s *Store) DeleteLoginPost(ctx context.Context, id int64) error {
	if err := s.q.DeleteLoginPost(ctx, id); err != nil {
		return fmt.Errorf("delete login post: %w", err)
	}
	return nil
}
