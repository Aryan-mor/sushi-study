// Package onboarding is the language question main-bot asks the first time a person taps /start.
//
// That locale is only the language main-bot uses when it talks to them. The app has its own
// language and does not read this column. Quality, audio, and the rest of the taste profile live
// in the app wizard (user_prefs taste columns). Since ADR 0003 the answers are entirely Sushi's:
// they live in the environment's user_prefs, with no read of the prototype's public.users and no seed
// from it. A returning person is not re-asked because Sushi already has their row.
package onboarding

import (
	"context"
	"fmt"
)

// Step is what to ask next.
type Step uint8

const (
	// StepDone means every bot question is answered.
	StepDone Step = iota
	StepLanguage
)

// Prefs is what a person told the bot (locale only). Playback prefs may still exist on the row
// from older bot onboarding or the app wizard; they are not part of Next().
type Prefs struct {
	Locale    string // bot-chat language (BotLocales); empty means unasked
	MaxHeight int32  // still stored; read by Sync / app
	Audio     string // still stored; read by Sync / app
}

// Next reports the first unanswered bot question.
func (p Prefs) Next() Step {
	if p.Locale == "" {
		return StepLanguage
	}
	return StepDone
}

// Complete reports whether bot onboarding is finished.
func (p Prefs) Complete() bool { return p.Next() == StepDone }

// Store persists answers.
type Store interface {
	Prefs(ctx context.Context, userID int64) (Prefs, error)
	SetLocale(ctx context.Context, userID int64, locale string) error
	SetMaxHeight(ctx context.Context, userID int64, height int32) error
	SetAudio(ctx context.Context, userID int64, audio string) error
}

// Service records answers and reports what to ask next.
type Service struct{ store Store }

// New builds a Service.
func New(store Store) *Service { return &Service{store: store} }

// Prefs returns what we know.
func (s *Service) Prefs(ctx context.Context, userID int64) (Prefs, error) {
	p, err := s.store.Prefs(ctx, userID)
	if err != nil {
		return Prefs{}, fmt.Errorf("onboarding: prefs: %w", err)
	}
	return p, nil
}

// BotLocales is every language main-bot can speak. Order is the picker order.
// Codes match user_prefs.locale (env migration 046).
var BotLocales = []string{"fa", "en", "ar", "de", "ru", "uk", "it", "fr", "tr"}

var validLocales = func() map[string]bool {
	m := make(map[string]bool, len(BotLocales))
	for _, code := range BotLocales {
		m[code] = true
	}
	return m
}()

// SetLocale records the language answer.
func (s *Service) SetLocale(ctx context.Context, userID int64, locale string) error {
	if !validLocales[locale] {
		return fmt.Errorf("onboarding: %q is not a supported locale", locale)
	}
	if err := s.store.SetLocale(ctx, userID, locale); err != nil {
		return fmt.Errorf("onboarding: set locale: %w", err)
	}
	return nil
}

// SetQuality records a max-height preference (settings / legacy). Not part of bot Next().
func (s *Service) SetQuality(ctx context.Context, userID int64, height int32) error {
	validHeights := map[int32]bool{480: true, 720: true, 1080: true, 2160: true}
	if !validHeights[height] {
		return fmt.Errorf("onboarding: %d is not an offered quality", height)
	}
	if err := s.store.SetMaxHeight(ctx, userID, height); err != nil {
		return fmt.Errorf("onboarding: set quality: %w", err)
	}
	return nil
}

// SetAudio records an audio preference (settings / legacy). Not part of bot Next().
func (s *Service) SetAudio(ctx context.Context, userID int64, audio string) error {
	validAudio := map[string]bool{"original": true, "dub": true}
	if !validAudio[audio] {
		return fmt.Errorf("onboarding: %q is not an offered audio preference", audio)
	}
	if err := s.store.SetAudio(ctx, userID, audio); err != nil {
		return fmt.Errorf("onboarding: set audio: %w", err)
	}
	return nil
}
