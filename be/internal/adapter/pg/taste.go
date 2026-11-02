package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
)

// Taste returns the taste profile for a user. Missing prefs row is an empty profile.
func (s *Store) Taste(ctx context.Context, userID int64) (api.TasteProfile, error) {
	row, err := s.q.GetTaste(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return api.TasteProfile{}, nil
	}
	if err != nil {
		return api.TasteProfile{}, fmt.Errorf("taste for user %d: %w", userID, err)
	}
	titles, err := api.UnmarshalLikedTitles(row.LikedTitles)
	if err != nil {
		return api.TasteProfile{}, fmt.Errorf("taste titles for user %d: %w", userID, err)
	}
	people := make([]uint64, 0, len(row.LikedPeople))
	for _, id := range row.LikedPeople {
		if id > 0 {
			people = append(people, uint64(id))
		}
	}
	var completed *time.Time
	if row.TasteCompletedAt.Valid {
		t := row.TasteCompletedAt.Time
		completed = &t
	}
	return api.TasteProfile{
		WatchKind:        row.WatchKind,
		Genres:           append([]string(nil), row.Genres...),
		LikedTitles:      titles,
		LikedPeople:      people,
		MaxHeight:        row.MaxHeight,
		Audio:            row.Audio,
		SubtitleLang:     row.SubtitleLang,
		Subs:             row.Subs,
		TasteCompletedAt: completed,
	}, nil
}

// SetTaste upserts the taste profile and stamps taste_completed_at.
func (s *Store) SetTaste(ctx context.Context, userID int64, p api.TasteProfile) error {
	raw, err := api.MarshalLikedTitles(p.LikedTitles)
	if err != nil {
		return fmt.Errorf("set taste: marshal titles: %w", err)
	}
	people := make([]int64, 0, len(p.LikedPeople))
	for _, id := range p.LikedPeople {
		people = append(people, int64(id))
	}
	if err := s.q.SetTaste(ctx, SetTasteParams{
		UserID:       userID,
		WatchKind:    p.WatchKind,
		Genres:       p.Genres,
		LikedTitles:  raw,
		LikedPeople:  people,
		MaxHeight:    p.MaxHeight,
		Audio:        p.Audio,
		SubtitleLang: p.SubtitleLang,
		Subs:         p.Subs,
	}); err != nil {
		return fmt.Errorf("set taste: %w", err)
	}
	return nil
}
