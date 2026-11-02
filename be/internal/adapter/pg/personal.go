package pg

import (
	"context"
	"fmt"

	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
)

// CanViewPersonal reports whether userID holds a ready grant on videoID (ADR 0037). Read from the
// replica; grants are keyed by Telegram id, joined through users.
func (s *Store) CanViewPersonal(ctx context.Context, userID, videoID int64) (bool, error) {
	if userID == 0 || videoID == 0 {
		return false, nil
	}
	ok, err := s.q.CanViewPersonal(ctx, CanViewPersonalParams{UserID: userID, VideoID: videoID})
	if err != nil {
		return false, fmt.Errorf("can view personal %d/%d: %w", userID, videoID, err)
	}
	return ok, nil
}

// ListPersonalVideos is one person's My videos, newest first, with their own progress.
func (s *Store) ListPersonalVideos(ctx context.Context, userID int64, limit, offset int32) ([]api.PersonalVideo, error) {
	if userID == 0 {
		return nil, nil
	}
	rows, err := s.q.ListPersonalVideos(ctx, ListPersonalVideosParams{
		UserID: userID, RowLimit: limit, RowOffset: offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list personal videos %d: %w", userID, err)
	}
	out := make([]api.PersonalVideo, len(rows))
	for i, r := range rows {
		out[i] = api.PersonalVideo{
			ID:        r.ID,
			Title:     r.Title,
			DurationS: r.DurationS,
			SizeBytes: r.SizeBytes,
			PositionS: r.PositionS,
			Done:      r.Done,
		}
	}
	return out, nil
}

// TelegramIDForUser is the Telegram identity grants are keyed by (ADR 0037).
func (s *Store) TelegramIDForUser(ctx context.Context, userID int64) (int64, error) {
	tg, err := s.q.UserTelegramID(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("telegram id for user %d: %w", userID, err)
	}
	return tg, nil
}

// EnqueuePersonalShare queues main-bot to send userID the video's share link (ADR 0038).
func (s *Store) EnqueuePersonalShare(ctx context.Context, userID, videoID int64) error {
	if err := s.q.EnqueuePersonalShare(ctx, EnqueuePersonalShareParams{UserID: userID, VideoID: videoID}); err != nil {
		return fmt.Errorf("enqueue personal share %d/%d: %w", userID, videoID, err)
	}
	return nil
}

// PendingPersonalShare is one Share tap main-bot has not answered yet (ADR 0038).
type PendingPersonalShare struct {
	ID       int64
	VideoID  int64
	TGUserID int64
	Locale   string
}

// PendingPersonalShares is the oldest unsent share requests.
func (s *Store) PendingPersonalShares(ctx context.Context, limit int32) ([]PendingPersonalShare, error) {
	rows, err := s.q.PendingPersonalShares(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("pending personal shares: %w", err)
	}
	out := make([]PendingPersonalShare, len(rows))
	for i, r := range rows {
		out[i] = PendingPersonalShare{ID: r.ID, VideoID: r.VideoID, TGUserID: r.TgUserID, Locale: r.Locale}
	}
	return out, nil
}

// MarkPersonalShareSent closes a request whose messages went out.
func (s *Store) MarkPersonalShareSent(ctx context.Context, id int64) error {
	if err := s.q.MarkPersonalShareSent(ctx, id); err != nil {
		return fmt.Errorf("mark personal share %d sent: %w", id, err)
	}
	return nil
}

// NotePersonalShareFailure counts a failed send; the third closes the request.
func (s *Store) NotePersonalShareFailure(ctx context.Context, id int64, reason string) error {
	if len(reason) > 300 {
		reason = reason[:300]
	}
	if err := s.q.NotePersonalShareFailure(ctx, NotePersonalShareFailureParams{ID: id, Error: reason}); err != nil {
		return fmt.Errorf("note personal share %d failure: %w", id, err)
	}
	return nil
}
