// Package enroll implements bot-login enrolment (docs/04 §3, ADR 0013): a person pastes a bot
// token into main-bot, and the bot behind it becomes their reader.
//
// The reader this produces -- reader_chat_id/reader_username learned from getMe on the presented
// token -- is the same shape of binding a session login gets, with a bot as the reader instead of
// the person's own account (docs/06 §3). main-bot then stores the token so it can re-issue the
// channel handoff; init-bot does not accept tokens.
package enroll

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
)

// tokenPattern is a Telegram bot token's shape: "<botId>:<secret>". Cheap enough to run on every
// stray message init-bot receives, so Telegram's getMe is only ever called on something that
// already looks like a real attempt.
var tokenPattern = regexp.MustCompile(`^\d{6,}:[A-Za-z0-9_-]{20,}$`)

// LooksLikeToken reports whether text has a bot token's shape. It is a filter, not a validator --
// the only real check is TokenVerifier.VerifyToken actually calling Telegram.
func LooksLikeToken(text string) bool {
	return tokenPattern.MatchString(strings.TrimSpace(text))
}

// TokenVerifier is the one Telegram call enrolment needs: getMe on a token nobody has vetted yet.
type TokenVerifier interface {
	// VerifyToken reports the identity behind token -- its own numeric chat id and handle, both
	// learned from Telegram, never trusted from what the person typed.
	VerifyToken(ctx context.Context, token string) (tgUserID int64, username string, err error)
}

// Binder is the identity write enrolment needs. identity.Service already satisfies this.
type Binder interface {
	Bind(ctx context.Context, tgUserID, chatID int64, readerUsername string) (identity.Binding, error)
}

// ErrInvalidToken means the text had a token's shape but Telegram rejected it -- a typo, a revoked
// token, or something that was never a bot token at all.
var ErrInvalidToken = errors.New("enroll: telegram rejected the token")

// Service runs bot-login enrolment.
type Service struct {
	verifier TokenVerifier
	binder   Binder
}

// New builds a Service.
func New(verifier TokenVerifier, binder Binder) *Service {
	return &Service{verifier: verifier, binder: binder}
}

// Enroll verifies token and binds the bot behind it as personTGUserID's reader.
// The username is Telegram's getMe handle, never taken from the pasted text.
//
// Order matters: the bot's own tg id and username are learned from Telegram (VerifyToken) before
// anything is written, never taken from the pasted text -- the whole point of getMe here is that a
// person's typing is not evidence (docs/06 §3, confirmed live 2026-08-26).
func (s *Service) Enroll(ctx context.Context, personTGUserID int64, token string) (identity.Binding, string, error) {
	botID, botUsername, err := s.verifier.VerifyToken(ctx, token)
	if err != nil {
		return identity.Binding{}, "", fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}

	binding, err := s.binder.Bind(ctx, personTGUserID, botID, botUsername)
	if err != nil {
		return identity.Binding{}, "", fmt.Errorf("enroll: bind: %w", err)
	}
	return binding, botUsername, nil
}
