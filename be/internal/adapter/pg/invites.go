package pg

import (
	"context"
	"fmt"
)

// pendingInvitesLimit bounds the admin panel: one button per row, and Telegram keyboards are finite.
const pendingInvitesLimit = 20

// RegistrationAllowed reports whether this Telegram id already has an account or an invite (ADR 0039).
func (s *Store) RegistrationAllowed(ctx context.Context, tgUserID int64) (bool, error) {
	allowed, err := s.q.RegistrationAllowed(ctx, tgUserID)
	if err != nil {
		return false, fmt.Errorf("registration allowed %d: %w", tgUserID, err)
	}
	return allowed, nil
}

// GrantInvite lets a Telegram id register. Granting twice keeps the first row.
func (s *Store) GrantInvite(ctx context.Context, tgUserID, invitedBy int64) error {
	if err := s.q.GrantUserInvite(ctx, GrantUserInviteParams{TgUserID: tgUserID, InvitedBy: invitedBy}); err != nil {
		return fmt.Errorf("grant invite %d: %w", tgUserID, err)
	}
	return nil
}

// RevokeInvite drops an authorization and reports whether one existed.
func (s *Store) RevokeInvite(ctx context.Context, tgUserID int64) (bool, error) {
	n, err := s.q.RevokeUserInvite(ctx, tgUserID)
	if err != nil {
		return false, fmt.Errorf("revoke invite %d: %w", tgUserID, err)
	}
	return n > 0, nil
}

// PendingInvites lists Telegram ids authorized but not registered yet, newest first.
func (s *Store) PendingInvites(ctx context.Context) ([]int64, error) {
	ids, err := s.q.PendingUserInvites(ctx, pendingInvitesLimit)
	if err != nil {
		return nil, fmt.Errorf("pending invites: %w", err)
	}
	return ids, nil
}
