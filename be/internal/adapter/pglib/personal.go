package pglib

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Aryan-mor/sushi-study/be/internal/app/personal"
	"github.com/Aryan-mor/sushi-study/be/internal/platform/channels"
)

// Personal is main-bot's write path into the personal-video tables (ADR 0037; R-LIB-1 as amended).
type Personal struct {
	q *Queries
}

// NewPersonal wraps a library pool.
func NewPersonal(db DBTX) *Personal {
	return &Personal{q: New(db)}
}

var _ personal.Store = (*Personal)(nil)

func (p *Personal) VideoByUID(ctx context.Context, uniqueID string) (int64, bool, error) {
	id, err := p.q.PersonalVideoByUID(ctx, uniqueID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("personal video by uid: %w", err)
	}
	return id, true, nil
}

func (p *Personal) InsertVideo(ctx context.Context, uploadedBy int64, v personal.Video) (int64, bool, error) {
	id, err := p.q.InsertPersonalVideo(ctx, InsertPersonalVideoParams{
		UploadedByTg: uploadedBy,
		TgUniqueID:   v.UniqueID,
		SizeBytes:    v.SizeBytes,
		DurationS:    int32(v.DurationS),
		Mime:         v.MimeType,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil // ON CONFLICT DO NOTHING: someone stored it first
	}
	if err != nil {
		return 0, false, fmt.Errorf("insert personal video: %w", err)
	}
	return id, true, nil
}

func (p *Personal) SetCloneLocation(ctx context.Context, videoID, channelID, messageID int64) error {
	if err := p.q.SetPersonalLocation(ctx, SetPersonalLocationParams{
		VideoID: videoID, Role: string(channels.RolePersonalClone), ChannelID: channelID, MessageID: messageID,
	}); err != nil {
		return fmt.Errorf("personal clone location %d: %w", videoID, err)
	}
	return nil
}

func (p *Personal) Grant(ctx context.Context, videoID, tgUserID int64) (int16, string, bool, error) {
	row, err := p.q.PersonalGrant(ctx, PersonalGrantParams{VideoID: videoID, TgUserID: tgUserID})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", false, nil
	}
	if err != nil {
		return 0, "", false, fmt.Errorf("personal grant %d/%d: %w", videoID, tgUserID, err)
	}
	return row.State, row.Title, true, nil
}

func (p *Personal) UpsertPendingGrant(ctx context.Context, videoID, tgUserID int64, suggestion string) error {
	if err := p.q.UpsertPendingGrant(ctx, UpsertPendingGrantParams{
		VideoID: videoID, TgUserID: tgUserID, Suggestion: suggestion,
	}); err != nil {
		return fmt.Errorf("pending grant %d/%d: %w", videoID, tgUserID, err)
	}
	return nil
}

func (p *Personal) ConfirmTitle(ctx context.Context, videoID, tgUserID int64, title string) (bool, error) {
	n, err := p.q.ConfirmGrantTitle(ctx, ConfirmGrantTitleParams{Title: title, VideoID: videoID, TgUserID: tgUserID})
	if err != nil {
		return false, fmt.Errorf("confirm title %d/%d: %w", videoID, tgUserID, err)
	}
	return n > 0, nil
}

func (p *Personal) LatestPending(ctx context.Context, tgUserID int64) (int64, string, bool, error) {
	row, err := p.q.LatestPendingGrant(ctx, tgUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", false, nil
	}
	if err != nil {
		return 0, "", false, fmt.Errorf("latest pending grant %d: %w", tgUserID, err)
	}
	return row.VideoID, row.Title, true, nil
}

func (p *Personal) UploadsSince(ctx context.Context, tgUserID int64, since time.Time) (int, error) {
	n, err := p.q.CountPersonalUploadsSince(ctx, CountPersonalUploadsSinceParams{
		TgUserID: tgUserID, Since: pgtype.Timestamptz{Time: since, Valid: true},
	})
	if err != nil {
		return 0, fmt.Errorf("personal uploads since: %w", err)
	}
	return int(n), nil
}

// DeleteGrant ends one person's access to one video (ADR 0038; apid's only library write).
func (p *Personal) DeleteGrant(ctx context.Context, videoID, tgUserID int64) error {
	if _, err := p.q.DeletePersonalGrant(ctx, DeletePersonalGrantParams{VideoID: videoID, TgUserID: tgUserID}); err != nil {
		return fmt.Errorf("delete personal grant %d/%d: %w", videoID, tgUserID, err)
	}
	return nil
}

var _ personal.ShareStore = (*Personal)(nil)

func (p *Personal) EnsureShare(ctx context.Context, videoID int64, token, title string, createdBy int64) error {
	if err := p.q.EnsurePersonalShare(ctx, EnsurePersonalShareParams{
		Token: token, VideoID: videoID, Title: title, CreatedByTg: createdBy,
	}); err != nil {
		return fmt.Errorf("ensure personal share %d: %w", videoID, err)
	}
	return nil
}

func (p *Personal) ShareByVideo(ctx context.Context, videoID int64) (string, string, bool, error) {
	row, err := p.q.PersonalShareByVideo(ctx, videoID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("personal share for video %d: %w", videoID, err)
	}
	return row.Token, row.Title, true, nil
}

func (p *Personal) ShareByToken(ctx context.Context, token string) (int64, string, int64, bool, error) {
	row, err := p.q.PersonalShareByToken(ctx, token)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", 0, false, nil
	}
	if err != nil {
		return 0, "", 0, false, fmt.Errorf("personal share by token: %w", err)
	}
	return row.VideoID, row.Title, row.CreatedByTg, true, nil
}

func (p *Personal) InsertSharedGrant(ctx context.Context, videoID, tgUserID int64, title string, grantedBy int64) (bool, error) {
	n, err := p.q.InsertSharedGrant(ctx, InsertSharedGrantParams{
		VideoID: videoID, TgUserID: tgUserID, Title: title, GrantedByTg: &grantedBy,
	})
	if err != nil {
		return false, fmt.Errorf("shared grant %d/%d: %w", videoID, tgUserID, err)
	}
	return n > 0, nil
}
