package identity

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ErrNotAuthorized means registration is closed and this Telegram id has neither an account nor an
// admin's authorization (ADR 0039). No users row was written.
var ErrNotAuthorized = errors.New("identity: registration is closed for this Telegram id")

// RegistrationStore answers the one question the testing-mode gate asks.
type RegistrationStore interface {
	RegistrationAllowed(ctx context.Context, tgUserID int64) (bool, error)
}

// WithClosedRegistration turns on the testing-mode gate (ADR 0039): only existing accounts, ids an
// admin authorized, and staffIDs may be created or served. staffIDs bypass the store so a fresh
// deployment's admins can reach /admin and authorize the first person.
func (s *Service) WithClosedRegistration(store RegistrationStore, staffIDs []int64) *Service {
	s.registration = store
	s.staffIDs = staffIDs
	return s
}

// Authorized reports whether this Telegram id may use Sushi. Always true when registration is open.
// A store failure is an error rather than a yes: the gate fails closed.
func (s *Service) Authorized(ctx context.Context, tgUserID int64) (bool, error) {
	if s.registration == nil || slices.Contains(s.staffIDs, tgUserID) {
		return true, nil
	}
	allowed, err := s.registration.RegistrationAllowed(ctx, tgUserID)
	if err != nil {
		return false, fmt.Errorf("identity: registration check: %w", err)
	}
	return allowed, nil
}

func (s *Service) admit(ctx context.Context, tgUserID int64) error {
	allowed, err := s.Authorized(ctx, tgUserID)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrNotAuthorized
	}
	return nil
}

// NotPublicText is the one answer every bot gives a person the gate refused. language is Telegram's
// IETF tag or a stored locale. It carries their id because that is what an admin has to authorize,
// and a bot cannot look a person up by @username.
func NotPublicText(language string, tgUserID int64) string {
	if strings.HasPrefix(language, "fa") {
		return fmt.Sprintf("🧪 سوشی یک پروژهٔ آزمایشی و آموزشیه و برای استفادهٔ عمومی باز نیست.\n\n"+
			"ثبت‌نام جدید بسته است. آیدی تلگرام شما: %d", tgUserID)
	}
	return fmt.Sprintf("🧪 Sushi is a test and study project and is not open for public use.\n\n"+
		"New registrations are closed. Your Telegram ID: %d", tgUserID)
}
