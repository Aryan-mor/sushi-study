package pg

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
)

// Bot roles, as stored in bots.role (ADR 0004).
const (
	roleAPI      int16 = 1
	roleDelivery int16 = 2
	roleMain     int16 = 3
	roleInit     int16 = 4
	roleFetcher  int16 = 5
)

// EnsureUser creates a person on first contact, or refreshes their last-seen stamp and Telegram
// display fields.
func (s *Store) EnsureUser(ctx context.Context, tgUserID int64, name, username string) (identity.User, error) {
	gone, err := s.q.DeletedTelegramIDExists(ctx, tgUserID)
	if err != nil {
		return identity.User{}, fmt.Errorf("ensure user: tombstone: %w", err)
	}
	if gone {
		return identity.User{TGUserID: tgUserID, Deleted: true}, nil
	}
	row, err := s.q.EnsureUser(ctx, EnsureUserParams{TgUserID: tgUserID, Name: name, Username: username})
	if err != nil {
		return identity.User{}, fmt.Errorf("ensure user: %w", err)
	}
	return identity.User{
		ID:       row.ID,
		TGUserID: row.TgUserID,
		Role:     row.Role,
		Name:     row.TgName,
		Username: row.TgUsername,
		Created:  row.Inserted,
	}, nil
}

// RequireIdentitySchema fails closed when identity migrations are missing.
//
// EnsureUser hits deleted_telegram_ids on every message (env 027). BindingByChat
// reads users.beta_tester (env 040); a skipped goose turned every /initbot into
// ERR_CODE_AUTH and left Home empty.
func (s *Store) RequireIdentitySchema(ctx context.Context) error {
	if _, err := s.q.DeletedTelegramIDExists(ctx, 0); err != nil {
		return fmt.Errorf("identity schema: apply db/migrations/env (need 027_account_tombstone): %w", err)
	}
	if _, err := s.BindingByChat(ctx, 0); err != nil && !errors.Is(err, identity.ErrNoBinding) {
		return fmt.Errorf("identity schema: apply db/migrations/env (need 040_beta_tester_app_tracks): %w", err)
	}
	if _, err := s.RegistrationAllowed(ctx, 0); err != nil {
		return fmt.Errorf("identity schema: apply db/migrations/env (need 058_user_invites): %w", err)
	}
	return nil
}

// SetUserRole corrects one user's role to match the configured lists (ADR 0014 §D1). The query's
// own IS DISTINCT FROM guard makes an already-correct row a no-op.
func (s *Store) SetUserRole(ctx context.Context, tgUserID int64, role string) error {
	if err := s.q.SetUserRole(ctx, SetUserRoleParams{TgUserID: tgUserID, Role: role}); err != nil {
		return fmt.Errorf("set user %d role: %w", tgUserID, err)
	}
	return nil
}

// SyncUserRoles resets every user's role to what the two lists say and returns how many rows
// changed (ADR 0014 §D1). nil lists are coerced to empty arrays so `= ANY(...)` is a clean false
// rather than NULL.
func (s *Store) SyncUserRoles(ctx context.Context, superadminIDs, adminIDs []int64) (int64, error) {
	if superadminIDs == nil {
		superadminIDs = []int64{}
	}
	if adminIDs == nil {
		adminIDs = []int64{}
	}
	n, err := s.q.SyncUserRoles(ctx, SyncUserRolesParams{SuperadminIds: superadminIDs, AdminIds: adminIDs})
	if err != nil {
		return 0, fmt.Errorf("sync user roles: %w", err)
	}
	return n, nil
}

// RegisterBot records a bot Sushi is running, so assignment has something to choose from.
//
// Called at boot by whichever service owns the token: the pool secret says which bots exist, and
// this is how the database learns the same thing (docs/08 §2.1).
func (s *Store) RegisterBot(ctx context.Context, username string, role int16) error {
	if _, err := s.q.UpsertBot(ctx, UpsertBotParams{Username: username, Role: role}); err != nil {
		return fmt.Errorf("register bot %s: %w", username, err)
	}
	return nil
}

// RegisterAPIBot records an API bot and returns its id, which the poller needs to report health
// problems against (docs/02 §7) -- the one registration caller that needs the id back, besides
// RegisterDeliveryBot.
func (s *Store) RegisterAPIBot(ctx context.Context, username string) (int32, error) {
	row, err := s.q.UpsertBot(ctx, UpsertBotParams{Username: username, Role: roleAPI})
	if err != nil {
		return 0, fmt.Errorf("register api bot %s: %w", username, err)
	}
	return row.ID, nil
}

// RegisterMainBot records main-bot.
func (s *Store) RegisterMainBot(ctx context.Context, username string) error {
	return s.RegisterBot(ctx, username, roleMain)
}

// RegisterInitBot records init-bot, the machine handshake (ADR 0004).
func (s *Store) RegisterInitBot(ctx context.Context, username string) error {
	return s.RegisterBot(ctx, username, roleInit)
}

// RegisterDeliveryBot records a delivery bot and returns its id, which deliveryd needs to claim
// and complete rows under (bots.id is the FK deliveries.bot_id and deliveries.claimed_by point at)
// -- the one registration caller that actually needs the id back, unlike the others above.
func (s *Store) RegisterDeliveryBot(ctx context.Context, username string) (int32, error) {
	row, err := s.q.UpsertBot(ctx, UpsertBotParams{Username: username, Role: roleDelivery})
	if err != nil {
		return 0, fmt.Errorf("register delivery bot %s: %w", username, err)
	}
	return row.ID, nil
}

// RegisterFetcherBot records the bot that copies files into Sushi's own channel.
func (s *Store) RegisterFetcherBot(ctx context.Context, username string) error {
	return s.RegisterBot(ctx, username, roleFetcher)
}

// APIBotPool lists the handles a client should round-robin protocol DMs across (ADR 0011/0012).
// Lifts expired quarantines first (docs/02 §7) so a bot whose rate-limit has cleared is not
// missing from the list forever.
func (s *Store) APIBotPool(ctx context.Context) ([]string, error) {
	if err := s.ReactivateExpiredQuarantines(ctx); err != nil {
		return nil, err
	}
	pool, err := s.q.APIBotPool(ctx)
	if err != nil {
		return nil, fmt.Errorf("api bot pool: %w", err)
	}
	return pool, nil
}

// ReactivateExpiredQuarantines lifts any bot whose quarantine has timed out. Called before
// selection reads bots.state rather than by a background sweeper (docs/02 §7) -- the table is
// tiny, so this costs nothing extra on the read path it protects.
func (s *Store) ReactivateExpiredQuarantines(ctx context.Context) error {
	if err := s.q.ReactivateExpiredQuarantines(ctx); err != nil {
		return fmt.Errorf("reactivate expired quarantines: %w", err)
	}
	return nil
}

// MarkBotQuarantined puts a bot on the bench until until, unless it already is -- fresh reports
// whether this call actually changed anything, which is what gates a superadmin notification on
// the transition rather than on every repeat failure while already quarantined.
func (s *Store) MarkBotQuarantined(ctx context.Context, botID int32, until time.Time) (fresh bool, err error) {
	if _, err := s.q.MarkBotQuarantined(ctx, MarkBotQuarantinedParams{ID: botID, QuarantinedUntil: pgtype.Timestamptz{Time: until, Valid: true}}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("mark bot %d quarantined: %w", botID, err)
	}
	return true, nil
}

// MarkBotRetired permanently removes a banned/token-revoked bot from rotation. fresh is false when
// it was already retired, so a repeat failure does not re-notify (docs/02 §7).
func (s *Store) MarkBotRetired(ctx context.Context, botID int32) (fresh bool, err error) {
	if _, err := s.q.MarkBotRetired(ctx, botID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("mark bot %d retired: %w", botID, err)
	}
	return true, nil
}

// ActiveDeliveryBotIDs lists delivery bots deliveryd may still hand new work to -- checked per
// pass, unlike the static slot list built once at boot, so a bot ops quarantines or retires
// mid-run is skipped without a restart.
func (s *Store) ActiveDeliveryBotIDs(ctx context.Context) (map[int32]bool, error) {
	ids, err := s.q.ActiveDeliveryBotIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("active delivery bot ids: %w", err)
	}
	out := make(map[int32]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// SupersedeBindings retires whatever this chat was bound to.
func (s *Store) SupersedeBindings(ctx context.Context, chatID int64) error {
	if err := s.q.SupersedeBindingsForChat(ctx, chatID); err != nil {
		return fmt.Errorf("supersede bindings: %w", err)
	}
	return nil
}

// CreateBinding writes a new live binding for a chat.
func (s *Store) CreateBinding(ctx context.Context, b identity.NewBinding) (identity.Binding, error) {
	var readerUsername *string
	if b.ReaderUsername != "" {
		readerUsername = &b.ReaderUsername
	}
	row, err := s.q.CreateBinding(ctx, CreateBindingParams{
		UserID:         b.UserID,
		ReaderChatID:   b.ChatID,
		BindingToken:   b.Token,
		ReaderUsername: readerUsername,
	})
	if err != nil {
		return identity.Binding{}, fmt.Errorf("create binding: %w", err)
	}
	if row.Epoch < 0 {
		return identity.Binding{}, fmt.Errorf("create binding: negative epoch %d", row.Epoch)
	}
	return identity.Binding{ID: row.ID, Epoch: uint32(row.Epoch), ProviderID: b.ProviderID}, nil
}

// BindingByChat resolves who a chat belongs to.
func (s *Store) BindingByChat(ctx context.Context, chatID int64) (identity.Binding, error) {
	row, err := s.q.BindingByChat(ctx, chatID)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Binding{}, identity.ErrNoBinding
	}
	if err != nil {
		return identity.Binding{}, fmt.Errorf("binding for chat %d: %w", chatID, err)
	}
	if row.Epoch < 0 {
		return identity.Binding{}, fmt.Errorf("binding for chat %d: negative epoch", chatID)
	}

	return identity.Binding{
		ID:           row.ID,
		UserID:       row.UserID,
		TGUserID:     row.TgUserID,
		ReaderChatID: chatID,
		ProviderID:   row.ProviderID,
		Token:        row.BindingToken,
		Epoch:        uint32(row.Epoch),
		Role:         row.Role,
		BetaTester:   row.BetaTester,
	}, nil
}

// readerTokenAAD binds a sealed token to the row it belongs to, so a ciphertext moved between users
// -- by a bad restore, a bad UPDATE, or somebody with a psql prompt -- fails to open rather than
// handing over another person's bot.
func readerTokenAAD(userID int64) []byte {
	return []byte("reader_bot_token:" + strconv.FormatInt(userID, 10))
}

// PutReaderBotToken stores the personal-bot token so main-bot can re-issue a channel handoff.
//
// Sealed here, never in SQL: the database holds a ciphertext it has no key for (env/017).
func (s *Store) PutReaderBotToken(ctx context.Context, userID int64, token, username string) error {
	sealed, err := s.secrets.Seal(token, readerTokenAAD(userID))
	if err != nil {
		return fmt.Errorf("put reader bot token: %w", err)
	}
	if err := s.q.UpsertReaderBotToken(ctx, UpsertReaderBotTokenParams{
		UserID:   userID,
		TokenEnc: sealed,
		Username: username,
	}); err != nil {
		return fmt.Errorf("put reader bot token: %w", err)
	}
	return nil
}

// ReaderBotToken loads the stored personal-bot token, if any.
func (s *Store) ReaderBotToken(ctx context.Context, userID int64) (token, username string, ok bool, err error) {
	row, err := s.q.ReaderBotTokenByUser(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("reader bot token: %w", err)
	}
	// env/017 could not convert the pre-encryption rows -- the key is in this process, not in the
	// database -- so it emptied them. An empty ciphertext is a user who presents their token once
	// more, which is a state main-bot already knows how to handle.
	if len(row.TokenEnc) == 0 {
		return "", row.Username, false, nil
	}
	token, err = s.secrets.Open(row.TokenEnc, readerTokenAAD(userID))
	if err != nil {
		return "", "", false, fmt.Errorf("reader bot token: %w", err)
	}
	return token, row.Username, true, nil
}

// DeleteReaderBotToken drops the stored personal-bot token. The previous binding is left alone —
// the next enrol writes a new reader. Missing row is success (already gone).
func (s *Store) DeleteReaderBotToken(ctx context.Context, userID int64) error {
	if err := s.q.DeleteReaderBotToken(ctx, userID); err != nil {
		return fmt.Errorf("delete reader bot token: %w", err)
	}
	return nil
}

// TombstoneUser records the Telegram id then deletes the users row (CASCADE). Staff roles are
// refused here as well as in identity.Service, so a store called without role-sync still cannot
// wipe an admin.
func (s *Store) TombstoneUser(ctx context.Context, tgUserID int64) error {
	return s.InTx(ctx, func(tx *Store) error {
		return tombstoneUserTx(ctx, tx, tgUserID)
	})
}

func tombstoneUserTx(ctx context.Context, tx *Store, tgUserID int64) error {
	row, err := tx.q.UserByTgID(ctx, tgUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		gone, existsErr := tx.q.DeletedTelegramIDExists(ctx, tgUserID)
		if existsErr != nil {
			return fmt.Errorf("tombstone user %d: %w", tgUserID, existsErr)
		}
		if gone {
			return identity.ErrAlreadyDeleted
		}
		return identity.ErrNoUser
	}
	if err != nil {
		return fmt.Errorf("tombstone user %d: %w", tgUserID, err)
	}
	if row.Role == identity.RoleAdmin || row.Role == identity.RoleSuperadmin {
		return identity.ErrProtected
	}
	if err := tx.q.InsertDeletedTelegramID(ctx, tgUserID); err != nil {
		return fmt.Errorf("tombstone user %d: %w", tgUserID, err)
	}
	if err := tx.q.DeleteUserByTgID(ctx, tgUserID); err != nil {
		return fmt.Errorf("tombstone user %d: %w", tgUserID, err)
	}
	return nil
}

// LiftTombstone drops the deletion marker so EnsureUser can create a fresh empty account.
func (s *Store) LiftTombstone(ctx context.Context, tgUserID int64) error {
	if err := s.q.LiftDeletedTelegramID(ctx, tgUserID); err != nil {
		return fmt.Errorf("lift tombstone %d: %w", tgUserID, err)
	}
	return nil
}
