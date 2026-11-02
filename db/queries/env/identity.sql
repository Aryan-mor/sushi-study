-- Identity and bindings (docs/02 §1, docs/04 §4).

-- name: EnsureUser :one
-- Created on first contact. The Telegram id is the natural key, and there is nothing to recover
-- because the Telegram identity is the credential (docs/04 §6). name/username are refreshed when
-- given and kept otherwise — a bot-to-bot ping or an update with no `from` passes '' and must not
-- wipe a real name.
INSERT INTO users (tg_user_id, tg_name, tg_username)
VALUES ($1, sqlc.arg(name)::text, sqlc.arg(username)::text)
ON CONFLICT (tg_user_id) DO UPDATE SET
  last_seen_at = now(),
  tg_name     = COALESCE(NULLIF(EXCLUDED.tg_name, ''), users.tg_name),
  tg_username = COALESCE(NULLIF(EXCLUDED.tg_username, ''), users.tg_username)
RETURNING id, tg_user_id, role, tg_name, tg_username, (xmax = 0) AS inserted;

-- name: SetUserRole :exec
-- The per-message correction path: main-bot's identity service calls this only when the role it
-- resolved for a chat no longer matches the SUPERADMIN_IDS / ADMIN_IDS lists (ADR 0014 §D1). The
-- IS DISTINCT FROM guard makes the common case (already correct) a no-op write.
UPDATE users SET role = $2 WHERE tg_user_id = $1 AND role IS DISTINCT FROM $2;

-- name: SyncUserRoles :execrows
-- Boot sweep: set every row to what the two lists say, so a user who never messages is still
-- correct (ADR 0014 §D1). The lists are authoritative -- a row promoted by hand and in neither
-- list is reset to 'user'. Runs once at main-bot start; the WHERE keeps it to the rows that
-- actually change.
UPDATE users SET role = desired.role
FROM (
  SELECT id,
    CASE
      WHEN tg_user_id = ANY(sqlc.arg(superadmin_ids)::bigint[]) THEN 'superadmin'
      WHEN tg_user_id = ANY(sqlc.arg(admin_ids)::bigint[])      THEN 'admin'
      ELSE 'user'
    END AS role
  FROM users
) AS desired
WHERE users.id = desired.id AND users.role IS DISTINCT FROM desired.role;

-- name: UpsertBot :one
-- Never resets state/quarantined_until: a restart must not un-quarantine or un-retire a bot ops
-- has not fixed yet (docs/02 §7). health_at is a separate boot-time heartbeat, harmless to bump.
INSERT INTO bots (username, role, health_at) VALUES ($1, $2, now())
ON CONFLICT (username) DO UPDATE SET health_at = now(), role = EXCLUDED.role
RETURNING id, username, role, state;

-- name: ReactivateExpiredQuarantines :exec
-- Run before any selection query touches bots: a quarantine's own expiry is what lifts it, not a
-- background sweeper (docs/02 §7). Idempotent and cheap on a table this small.
UPDATE bots SET state = 1, quarantined_until = NULL
WHERE state = 3 AND quarantined_until <= now();

-- name: MarkBotQuarantined :one
-- Rate-limited past what the caller's own retry loop already absorbed (docs/02 §6). Guarded by
-- "state != 3" so re-hitting this while already quarantined is a no-op the caller can tell apart
-- from a fresh transition (zero rows back) -- that's what gates the superadmin notification.
UPDATE bots SET state = 3, quarantined_until = $2
WHERE id = $1 AND state != 3
RETURNING id;

-- name: MarkBotRetired :one
-- A real ban/token revocation (401): permanent until ops swaps the bot in config, never auto-lifted
-- like a quarantine. "state != 4" makes a repeat failure against an already-retired bot silent.
UPDATE bots SET state = 4 WHERE id = $1 AND state != 4
RETURNING id;

-- name: APIBotPool :many
SELECT username FROM bots WHERE role = 1 AND state IN (1, 2) ORDER BY id;

-- name: ActiveDeliveryBotIDs :many
-- deliveryd checks this per pass (unlike the static slot list built once at boot) so a bot ops
-- quarantines or retires mid-run is skipped without a restart (docs/02 §7).
SELECT id FROM bots WHERE role = 2 AND state IN (1, 2);

-- name: SupersedeBindingsForChat :exec
UPDATE bindings SET superseded_at = now() WHERE reader_chat_id = $1 AND superseded_at IS NULL;

-- name: CreateBinding :one
-- reader_username is set only for a bot-login reader (main-bot learns it from getMe on the
-- presented bot token); NULL for a session reader, which is always resolvable cold. It is what
-- lets deliveryd address this reader the first time a pool delivery bot has never contacted it
-- (docs/06 §3, confirmed live 2026-08-26 -- a numeric chat id fails "chat not found" on that very
-- first message, in either direction, even with Bot-to-Bot mode on; @handle does not).
INSERT INTO bindings (user_id, reader_chat_id, binding_token, epoch, reader_username)
VALUES ($1, $2, $3,
  -- Continue the chat's epoch rather than restarting it, so "newer" is always comparable.
  COALESCE((SELECT max(epoch) + 1 FROM bindings WHERE reader_chat_id = $2), 1),
  sqlc.narg(reader_username))
RETURNING id, epoch;

-- name: BindingByChat :one
SELECT b.id, b.user_id, b.epoch, b.provider_id, b.binding_token,
       u.tg_user_id, u.role, u.beta_tester
FROM bindings b
JOIN users u ON u.id = b.user_id
WHERE b.reader_chat_id = $1 AND b.superseded_at IS NULL;

-- name: ReaderUsernameForChat :one
-- deliveryd's fallback lookup: the current binding's reader_username, if this reader has one
-- (bot-login) or NULL (session-login, nothing to fall back to).
SELECT reader_username FROM bindings
WHERE reader_chat_id = $1 AND superseded_at IS NULL;

-- name: UpsertReaderBotToken :exec
-- token_enc is AES-256-GCM, sealed by pg.Store before it gets here and never readable from SQL
-- (env/017). The database has no key and is not meant to have one.
INSERT INTO reader_bot_tokens (user_id, token_enc, username, updated_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (user_id) DO UPDATE
SET token_enc = EXCLUDED.token_enc, username = EXCLUDED.username, updated_at = now();

-- name: ReaderBotTokenByUser :one
SELECT token_enc, username FROM reader_bot_tokens WHERE user_id = $1;

-- name: DeleteReaderBotToken :exec
DELETE FROM reader_bot_tokens WHERE user_id = $1;

-- name: UserByTgID :one
SELECT id, tg_user_id, role FROM users WHERE tg_user_id = $1;

-- name: DeletedTelegramIDExists :one
SELECT EXISTS(SELECT 1 FROM deleted_telegram_ids WHERE tg_user_id = $1);

-- name: InsertDeletedTelegramID :exec
INSERT INTO deleted_telegram_ids (tg_user_id) VALUES ($1)
ON CONFLICT (tg_user_id) DO NOTHING;

-- name: DeleteUserByTgID :exec
DELETE FROM users WHERE tg_user_id = $1;

-- name: LiftDeletedTelegramID :exec
DELETE FROM deleted_telegram_ids WHERE tg_user_id = $1;

-- name: RegistrationAllowed :one
-- The testing-mode gate (ADR 0039): somebody who already has an account, or whom an admin has
-- authorized. Runs before EnsureUser so a refused person never gets a users row.
SELECT (
  EXISTS (SELECT 1 FROM users WHERE users.tg_user_id = $1)
  OR EXISTS (SELECT 1 FROM user_invites WHERE user_invites.tg_user_id = $1)
)::boolean AS allowed;

-- name: GrantUserInvite :exec
-- Idempotent: authorizing the same person twice keeps the first admin and date.
INSERT INTO user_invites (tg_user_id, invited_by) VALUES ($1, $2)
ON CONFLICT (tg_user_id) DO NOTHING;

-- name: RevokeUserInvite :execrows
DELETE FROM user_invites WHERE tg_user_id = $1;

-- name: PendingUserInvites :many
-- Authorized but not registered yet. Revoking only means something for these: an account that
-- already exists is not removed by dropping its invite.
SELECT i.tg_user_id
FROM user_invites i
WHERE NOT EXISTS (SELECT 1 FROM users u WHERE u.tg_user_id = i.tg_user_id)
ORDER BY i.created_at DESC
LIMIT $1;
