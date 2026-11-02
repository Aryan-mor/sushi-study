package pg

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/Aryan-mor/sushi-study/be/internal/app/onboarding"
)

// Prefs returns a person's onboarding answers, seeded from the prototype's where Sushi has none.
func (s *Store) Prefs(ctx context.Context, userID int64) (onboarding.Prefs, error) {
	row, err := s.q.GetPrefs(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		// No user row means nothing has been asked yet, which is a valid state, not a failure.
		return onboarding.Prefs{}, nil
	}
	if err != nil {
		return onboarding.Prefs{}, fmt.Errorf("prefs for user %d: %w", userID, err)
	}
	return onboarding.Prefs{
		Locale:    row.Locale,
		MaxHeight: row.MaxHeight,
		Audio:     row.Audio,
	}, nil
}

// PlaybackPrefs is the audio / max-height / subtitle-lang half of Prefs, for api.PlaybackPrefsStore.
func (s *Store) PlaybackPrefs(ctx context.Context, userID int64) (audio string, maxHeight int32, subtitleLang, subs string, err error) {
	row, err := s.q.GetPrefs(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, "", "", nil
	}
	if err != nil {
		return "", 0, "", "", fmt.Errorf("playback prefs for user %d: %w", userID, err)
	}
	return row.Audio, row.MaxHeight, row.SubtitleLang, row.Subs, nil
}

func geminiKeyAAD(userID int64) []byte {
	return []byte("gemini_api_key:" + strconv.FormatInt(userID, 10))
}

// PutGeminiAPIKey seals the user's Gemini key (env/028). Empty is refused; replace is an upsert.
func (s *Store) PutGeminiAPIKey(ctx context.Context, userID int64, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("put gemini api key: empty")
	}
	sealed, err := s.secrets.Seal(key, geminiKeyAAD(userID))
	if err != nil {
		return fmt.Errorf("put gemini api key: %w", err)
	}
	if err := s.q.SetGeminiKeyEnc(ctx, SetGeminiKeyEncParams{UserID: userID, GeminiApiKeyEnc: sealed}); err != nil {
		return fmt.Errorf("put gemini api key: %w", err)
	}
	return nil
}

// GeminiAPIKey opens the stored key, if any. ok is false when the user has none.
func (s *Store) GeminiAPIKey(ctx context.Context, userID int64) (key string, ok bool, err error) {
	enc, err := s.q.GetGeminiKeyEnc(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("gemini api key: %w", err)
	}
	if len(enc) == 0 {
		return "", false, nil
	}
	key, err = s.secrets.Open(enc, geminiKeyAAD(userID))
	if err != nil {
		return "", false, fmt.Errorf("gemini api key: %w", err)
	}
	return key, true, nil
}

// HasGeminiAPIKey reports whether a ciphertext is stored. It does not open it.
func (s *Store) HasGeminiAPIKey(ctx context.Context, userID int64) (bool, error) {
	enc, err := s.q.GetGeminiKeyEnc(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("has gemini api key: %w", err)
	}
	return len(enc) > 0, nil
}

// DeleteGeminiAPIKey clears the sealed key. Missing row is success.
func (s *Store) DeleteGeminiAPIKey(ctx context.Context, userID int64) error {
	if err := s.q.ClearGeminiKeyEnc(ctx, userID); err != nil {
		return fmt.Errorf("delete gemini api key: %w", err)
	}
	return nil
}

// SetLocale records the language answer.
func (s *Store) SetLocale(ctx context.Context, userID int64, locale string) error {
	if err := s.q.SetLocale(ctx, SetLocaleParams{UserID: userID, Locale: locale}); err != nil {
		return fmt.Errorf("set locale: %w", err)
	}
	return nil
}

// SetMaxHeight records the quality answer.
func (s *Store) SetMaxHeight(ctx context.Context, userID int64, height int32) error {
	if err := s.q.SetMaxHeight(ctx, SetMaxHeightParams{UserID: userID, MaxHeight: height}); err != nil {
		return fmt.Errorf("set max height: %w", err)
	}
	return nil
}

// SetAudio records the audio answer.
func (s *Store) SetAudio(ctx context.Context, userID int64, audio string) error {
	if err := s.q.SetAudio(ctx, SetAudioParams{UserID: userID, Audio: audio}); err != nil {
		return fmt.Errorf("set audio: %w", err)
	}
	return nil
}
