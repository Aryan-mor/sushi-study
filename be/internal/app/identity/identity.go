// Package identity turns a Telegram chat into somebody, and hands out the assignment that lets a
// client stop talking to main-bot.
//
// There is no username, no password and no login code: the client is a Telegram identity, and
// Telegram already proves who it is (docs/04 §1).
package identity

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
)

// tokenBytes is the size of the per-binding token.
//
// It is not a password — it is an epoch check that lets the server say "your binding is stale,
// rebind", and catches a client restored from an old backup (docs/02 §9). Sixteen bytes is far
// more than that needs and costs nothing.
const tokenBytes = 16

// Role values stored in users.role (ADR 0014 §D1). Not a content permission — the catalog is
// public (R-DATA-6). The role only routes content-request admin alerts and exempts staff from the
// per-day request quota.
const (
	RoleUser       = "user"
	RoleAdmin      = "admin"
	RoleSuperadmin = "superadmin"
)

// Store is the persistence identity needs.
type Store interface {
	// EnsureUser records the person on first contact and refreshes last-seen. Created is true
	// only on the insert. name / username are the Telegram display fields; "" leaves whatever is
	// stored (docs/04 §6, ADR 0014 §D4).
	EnsureUser(ctx context.Context, tgUserID int64, name, username string) (User, error)
	APIBotPool(ctx context.Context) ([]string, error)
	SupersedeBindings(ctx context.Context, chatID int64) error
	CreateBinding(ctx context.Context, b NewBinding) (Binding, error)
	BindingByChat(ctx context.Context, chatID int64) (Binding, error)
	// SetUserRole corrects one row when the resolved role no longer matches the configured lists.
	SetUserRole(ctx context.Context, tgUserID int64, role string) error
	// SyncUserRoles resets every row to what the lists say and returns how many changed.
	SyncUserRoles(ctx context.Context, superadminIDs, adminIDs []int64) (int64, error)
	// TombstoneUser removes the account and records the Telegram id so the next /start cannot
	// silently recreate it (docs/04 §6).
	TombstoneUser(ctx context.Context, tgUserID int64) error
	// LiftTombstone drops the deletion marker so EnsureUser can mint a fresh empty account.
	LiftTombstone(ctx context.Context, tgUserID int64) error
}

// User is a person, keyed by their Telegram id.
//
// There is no link to an the prototype account any more: since ADR 0003 identity, preferences and
// progress are all Sushi's, so somebody who arrived here and never used the prototype is an ordinary
// user rather than a dead end.
type User struct {
	ID       int64
	TGUserID int64
	// Role is users.role — RoleUser unless the SUPERADMIN_IDS / ADMIN_IDS lists say otherwise
	// (ADR 0014 §D1).
	Role string
	// Name and Username are the Telegram display fields stored on the row. Empty until a contact
	// with a real `from` has refreshed them.
	Name     string
	Username string
	// Created is true when EnsureUser inserted the row (first contact), not a last-seen refresh.
	Created bool
	// Deleted means this Telegram id is tombstoned. ID is zero; EnsureUser did not recreate a row.
	Deleted bool
}

// NewBinding is a binding about to be created.
type NewBinding struct {
	UserID     int64
	ChatID     int64
	Token      []byte
	ProviderID int32
	// ReaderUsername is this reader's own Telegram handle, learned from getMe on the presented
	// token for a bot-login reader; empty for a session reader, which is always resolvable cold
	// and has nothing to fall back to (docs/06 §3).
	ReaderUsername string
}

// Binding is the record the client mirrors locally (docs/04 §4).
type Binding struct {
	ID           int64
	UserID       int64
	TGUserID     int64
	ReaderChatID int64
	ProviderID   int32
	Token        []byte
	Epoch        uint32
	// Role is the bound user's users.role, carried so the transport can put it on the Principal
	// (ADR 0014 §D2). Never read from a payload (R-SEC-9).
	Role string
	// BetaTester is users.beta_tester, set by hand. Admin and superadmin are nightly testers
	// from their role even when this is false. Never read from a payload (R-SEC-9).
	BetaTester bool
}

var (
	// ErrNoBinding means this chat has never run /initbot, or its binding was superseded.
	ErrNoBinding = errors.New("identity: chat has no binding")
	// ErrProtected means a staff account cannot be deleted from the bot.
	ErrProtected = errors.New("identity: staff account cannot be deleted")
	// ErrAlreadyDeleted means this Telegram id is already tombstoned.
	ErrAlreadyDeleted = errors.New("identity: account already deleted")
	// ErrNoUser means there is no users row to tombstone.
	ErrNoUser = errors.New("identity: no such user")
)

// Service issues and resolves bindings.
type Service struct {
	store Store
	// roleSync and the two lists are set by WithRoleSync. Only main-bot turns this on — it is the
	// single writer of users.role (ADR 0014 §D1). apid and init-bot leave the column alone.
	roleSync      bool
	superadminIDs []int64
	adminIDs      []int64
	// registration and staffIDs are set by WithClosedRegistration. A nil registration means open
	// (ADR 0039).
	registration RegistrationStore
	staffIDs     []int64
}

// New builds a Service.
func New(store Store) *Service { return &Service{store: store} }

// WithRoleSync turns on env-authoritative role management (ADR 0014 §D1): the two id lists become
// the source of truth for users.role — applied wholesale by SyncRoles at boot and corrected one
// row at a time in EnsureUser. Only main-bot calls this; it is the single writer of the column.
// Passing empty lists is meaningful: it demotes every stray admin back to RoleUser.
func (s *Service) WithRoleSync(superadminIDs, adminIDs []int64) *Service {
	s.roleSync = true
	s.superadminIDs = superadminIDs
	s.adminIDs = adminIDs
	return s
}

// SyncRoles resets every user's role to match the configured lists and returns how many changed.
// main-bot calls it once at boot so a user who never sends a message is still classified correctly
// (ADR 0014 §D1). A no-op when WithRoleSync was not called.
func (s *Service) SyncRoles(ctx context.Context) (int64, error) {
	if !s.roleSync {
		return 0, nil
	}
	n, err := s.store.SyncUserRoles(ctx, s.superadminIDs, s.adminIDs)
	if err != nil {
		return 0, fmt.Errorf("identity: sync roles: %w", err)
	}
	return n, nil
}

// desiredRole is what the configured lists say this Telegram id should be. The superadmin list
// wins when an id somehow appears in both.
func (s *Service) desiredRole(tgUserID int64) string {
	switch {
	case slices.Contains(s.superadminIDs, tgUserID):
		return RoleSuperadmin
	case slices.Contains(s.adminIDs, tgUserID):
		return RoleAdmin
	default:
		return RoleUser
	}
}

// Bind writes a live binding for this chat: the person, a fresh token, a new epoch.
//
// It does not pick an API bot (ADR 0012) — the client round-robins the pool. It does refuse when
// the pool is empty, so onboarding cannot finish before apid has registered at least one.
//
// Idempotent in the way that matters: running it again returns a working binding. It supersedes
// rather than reuses, so a client that lost its token always gets one it can use — and the old row
// stays for audit (docs/02 §1).
//
// readerUsername is the reader's own handle for a bot-login client (learned from getMe on the
// token it presented) and empty for a session client -- see NewBinding.ReaderUsername.
func (s *Service) Bind(ctx context.Context, tgUserID, chatID int64, readerUsername string) (Binding, error) {
	if err := s.admit(ctx, tgUserID); err != nil {
		return Binding{}, err
	}
	// Bind runs after onboarding, which already recorded the name via EnsureUser; "" here keeps it.
	user, err := s.store.EnsureUser(ctx, tgUserID, "", "")
	if err != nil {
		return Binding{}, fmt.Errorf("identity: ensure user: %w", err)
	}

	pool, err := s.store.APIBotPool(ctx)
	if err != nil {
		return Binding{}, fmt.Errorf("identity: api bot pool: %w", err)
	}
	if len(pool) == 0 {
		return Binding{}, errors.New("identity: no active API bot is registered")
	}

	token := make([]byte, tokenBytes)
	if _, err = rand.Read(token); err != nil {
		return Binding{}, fmt.Errorf("identity: token: %w", err)
	}

	if err = s.store.SupersedeBindings(ctx, chatID); err != nil {
		return Binding{}, fmt.Errorf("identity: supersede: %w", err)
	}

	binding, err := s.store.CreateBinding(ctx, NewBinding{
		UserID:         user.ID,
		ChatID:         chatID,
		Token:          token,
		ProviderID:     1,
		ReaderUsername: readerUsername,
	})
	if err != nil {
		return Binding{}, fmt.Errorf("identity: create binding: %w", err)
	}

	binding.UserID = user.ID
	binding.TGUserID = user.TGUserID
	binding.ReaderChatID = chatID
	binding.Token = token
	return binding, nil
}

// EnsureUser creates the person on first contact, or refreshes their last-seen stamp and Telegram
// display name / username.
//
// Separate from Bind because the onboarding questions come first: we need somebody to attach the
// answers to before there is anything to bind.
func (s *Service) EnsureUser(ctx context.Context, tgUserID int64, name, username string) (User, error) {
	if err := s.admit(ctx, tgUserID); err != nil {
		return User{}, err
	}
	user, err := s.store.EnsureUser(ctx, tgUserID, name, username)
	if err != nil {
		return User{}, fmt.Errorf("identity: ensure user: %w", err)
	}
	if user.Deleted {
		return user, nil
	}
	if s.roleSync {
		if want := s.desiredRole(tgUserID); want != user.Role {
			// A failed correction is not worth failing the person's message over — SyncRoles is
			// the authoritative pass and re-runs every boot. Leave the local view at the stored
			// value so it reflects what is actually in the row.
			if setErr := s.store.SetUserRole(ctx, tgUserID, want); setErr == nil {
				user.Role = want
			}
		}
	}
	return user, nil
}

// Pool lists the API bots a client should recognise and round-robin protocol DMs across (ADR 0011).
func (s *Service) Pool(ctx context.Context) ([]string, error) {
	pool, err := s.store.APIBotPool(ctx)
	if err != nil {
		return nil, fmt.Errorf("identity: pool: %w", err)
	}
	return pool, nil
}

// Resolve answers "who is this chat?" for every request an API bot serves.
func (s *Service) Resolve(ctx context.Context, chatID int64) (Binding, error) {
	binding, err := s.store.BindingByChat(ctx, chatID)
	if err != nil {
		return Binding{}, fmt.Errorf("identity: resolve chat %d: %w", chatID, err)
	}
	return binding, nil
}

// Tombstone removes this person's account. The Telegram id is recorded so the next /start does
// not silently recreate it (docs/04 §6). Staff accounts (env lists or users.role) are refused.
func (s *Service) Tombstone(ctx context.Context, tgUserID int64) error {
	if s.roleSync && s.desiredRole(tgUserID) != RoleUser {
		return ErrProtected
	}
	if err := s.store.TombstoneUser(ctx, tgUserID); err != nil {
		return fmt.Errorf("identity: tombstone: %w", err)
	}
	return nil
}

// Revive drops the tombstone so the next EnsureUser mints a new empty account.
func (s *Service) Revive(ctx context.Context, tgUserID int64) error {
	if err := s.store.LiftTombstone(ctx, tgUserID); err != nil {
		return fmt.Errorf("identity: revive: %w", err)
	}
	return nil
}
