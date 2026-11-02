package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
	"github.com/Aryan-mor/sushi-study/be/internal/app/delivery"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

// Delivery returns the current row for (readerChatID, kind, fileID), and whether one exists at all.
// A zero MessageID (api.Delivery.Pending()) means deliveryd has not finished the copy yet.
func (s *Store) Delivery(ctx context.Context, readerChatID int64, kind catalog.MediaKind, fileID int64) (api.Delivery, bool, error) {
	row, err := s.q.GetDelivery(ctx, GetDeliveryParams{ReaderChatID: readerChatID, Kind: int16(kind), FileID: fileID})
	if errors.Is(err, pgx.ErrNoRows) {
		return api.Delivery{}, false, nil
	}
	if err != nil {
		return api.Delivery{}, false, fmt.Errorf("delivery %d/%d: %w", readerChatID, fileID, err)
	}
	return api.Delivery{
		BotID:     deref(row.BotID),
		MessageID: deref(row.MessageID),
		Locator:   row.Locator,
	}, true, nil
}

// PendingCount is the concurrent-copy-jobs cap check (R-SEC-11).
func (s *Store) PendingCount(ctx context.Context, readerChatID int64) (int, error) {
	n, err := s.q.PendingCountForReader(ctx, readerChatID)
	if err != nil {
		return 0, fmt.Errorf("pending count for %d: %w", readerChatID, err)
	}
	return int(n), nil
}

// UnclaimedPendingOlderThan is how Play tells a dead copier from the concurrent cap.
func (s *Store) UnclaimedPendingOlderThan(ctx context.Context, readerChatID int64, age time.Duration) (int, error) {
	n, err := s.q.CountStaleUnclaimedPendingForReader(ctx, CountStaleUnclaimedPendingForReaderParams{
		ReaderChatID:  readerChatID,
		OlderThanSecs: int32(age.Seconds()),
	})
	if err != nil {
		return 0, fmt.Errorf("unclaimed pending for %d: %w", readerChatID, err)
	}
	return int(n), nil
}

// StaleUnclaimedCount is the cluster-wide form deliverywatch uses.
func (s *Store) StaleUnclaimedCount(ctx context.Context, olderThan time.Duration) (int, error) {
	n, err := s.q.CountStaleUnclaimedPending(ctx, int32(olderThan.Seconds()))
	if err != nil {
		return 0, fmt.Errorf("stale unclaimed pending: %w", err)
	}
	return int(n), nil
}

// DeliveredCountSince is the per-hour deliveries cap check (R-SEC-11).
func (s *Store) DeliveredCountSince(ctx context.Context, readerChatID int64, since time.Time) (int, error) {
	n, err := s.q.DeliveredCountForReaderSince(ctx, DeliveredCountForReaderSinceParams{
		ReaderChatID: readerChatID,
		DeliveredAt:  pgtype.Timestamptz{Time: since, Valid: true},
	})
	if err != nil {
		return 0, fmt.Errorf("delivered count for %d: %w", readerChatID, err)
	}
	return int(n), nil
}

// Enqueue writes the pending row Play answers with and deliveryd later claims.
func (s *Store) Enqueue(ctx context.Context, readerChatID int64, kind catalog.MediaKind, fileID int64, locator string) error {
	if err := s.q.EnqueuePending(ctx, EnqueuePendingParams{
		ReaderChatID: readerChatID, Kind: int16(kind), FileID: fileID, Locator: locator,
	}); err != nil {
		return fmt.Errorf("enqueue %d/%d: %w", readerChatID, fileID, err)
	}
	return nil
}

// ForceRedeliver drops any existing row so Play's caller falls through to a fresh enqueue
// (docs/05 §4, "broken row").
func (s *Store) ForceRedeliver(ctx context.Context, readerChatID int64, kind catalog.MediaKind, fileID int64) error {
	if err := s.q.ForceRedeliver(ctx, ForceRedeliverParams{ReaderChatID: readerChatID, Kind: int16(kind), FileID: fileID}); err != nil {
		return fmt.Errorf("force redeliver %d/%d: %w", readerChatID, fileID, err)
	}
	return nil
}

// Ack records the message id the client actually read (receiver-side). copyMessage's return is
// the sender's numbering and is the wrong id for bot-login getMessages.
func (s *Store) Ack(ctx context.Context, readerChatID int64, kind catalog.MediaKind, fileID, messageID int64) error {
	if err := s.q.TouchAck(ctx, TouchAckParams{
		ReaderChatID: readerChatID, Kind: int16(kind), FileID: fileID, MessageID: &messageID,
	}); err != nil {
		return fmt.Errorf("ack %d/%d: %w", readerChatID, fileID, err)
	}
	return nil
}

// ClaimPendingDeliveries hands one bot the next batch of unclaimed (or staled-out) pending rows.
// One short statement, not a held transaction (see db/queries/env/deliveries.sql) -- the actual
// copy is a slow network call and must never happen while a row lock is open.
func (s *Store) ClaimPendingDeliveries(
	ctx context.Context, botID int32, batchSize int, staleAfter time.Duration,
) ([]delivery.PendingJob, error) {
	rows, err := s.q.ClaimPendingDeliveries(ctx, ClaimPendingDeliveriesParams{
		BotID:     &botID,
		StaleSecs: int32(staleAfter.Seconds()),
		BatchSize: int32(batchSize),
	})
	if err != nil {
		return nil, fmt.Errorf("claim pending deliveries for bot %d: %w", botID, err)
	}
	out := make([]delivery.PendingJob, len(rows))
	for i, r := range rows {
		out[i] = delivery.PendingJob{
			ReaderChatID: r.ReaderChatID, Kind: catalog.MediaKind(r.Kind), FileID: r.FileID, Locator: r.Locator,
		}
	}
	return out, nil
}

// CompleteDelivery fills in the bot and message id a claimed row was missing. Guarded at the SQL
// level by message_id IS NULL, so a delayed retry of an already-completed job cannot overwrite a
// real message id with a second, different one.
func (s *Store) CompleteDelivery(
	ctx context.Context, readerChatID int64, kind catalog.MediaKind, fileID int64, botID int32, messageID int64,
) error {
	if err := s.q.CompleteDelivery(ctx, CompleteDeliveryParams{
		BotID: &botID, MessageID: &messageID, ReaderChatID: readerChatID, Kind: int16(kind), FileID: fileID,
	}); err != nil {
		return fmt.Errorf("complete delivery %d/%d: %w", readerChatID, fileID, err)
	}
	return nil
}

// ReaderUsername is deliveryd's fallback lookup: the reader's own handle, if this is a bot-login
// reader deliveryd has never successfully addressed by chat id before.
func (s *Store) ReaderUsername(ctx context.Context, readerChatID int64) (string, bool, error) {
	username, err := s.q.ReaderUsernameForChat(ctx, readerChatID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("reader username for %d: %w", readerChatID, err)
	}
	if username == nil || *username == "" {
		return "", false, nil
	}
	return *username, true, nil
}

// deref reads a nullable sqlc column as its zero value when absent — exactly the "pending" sentinel
// api.Delivery.Pending() checks for.
func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}
