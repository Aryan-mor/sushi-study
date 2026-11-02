-- Personal videos (ADR 0037). main-bot writes videos, grants and the personal_clone location;
-- backupd writes the personal_backup location; deliveryd reads where to copy from.

-- name: PersonalVideoByUID :one
-- Telegram gives one file the same file_unique_id whoever sends it: this is how a second sender
-- becomes a grant instead of a second copy.
SELECT id FROM personal_videos WHERE tg_unique_id = $1;

-- name: InsertPersonalVideo :one
-- No row back means somebody else stored the same file first (UNIQUE tg_unique_id); the caller
-- reads that row with PersonalVideoByUID and drops its own channel copy.
INSERT INTO personal_videos (uploaded_by_tg, tg_unique_id, size_bytes, duration_s, mime, updated_seq)
VALUES (sqlc.arg(uploaded_by_tg), sqlc.arg(tg_unique_id), sqlc.arg(size_bytes), sqlc.arg(duration_s),
        sqlc.arg(mime), nextval('catalog_seq'))
ON CONFLICT (tg_unique_id) DO NOTHING
RETURNING id;

-- name: SetPersonalLocation :exec
-- Keyed by role, like file_locations: a re-run updates the row for the role it just wrote.
INSERT INTO personal_video_locations (video_id, role, channel_id, message_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (video_id, role) DO UPDATE SET
  channel_id = EXCLUDED.channel_id, message_id = EXCLUDED.message_id, copied_at = now();

-- name: PersonalGrant :one
SELECT state, title FROM personal_video_grants WHERE video_id = $1 AND tg_user_id = $2;

-- name: UpsertPendingGrant :exec
-- A person who sends the file themselves names it: state 1 until they do. title holds the
-- suggestion (caption or file name) the prompt offers. A ready grant is never sent back to 1 --
-- the caller checks PersonalGrant first, and this guard makes a race harmless.
INSERT INTO personal_video_grants (video_id, tg_user_id, state, title, updated_seq)
VALUES (sqlc.arg(video_id), sqlc.arg(tg_user_id), 1, sqlc.arg(suggestion), nextval('catalog_seq'))
ON CONFLICT (video_id, tg_user_id) DO UPDATE SET
  title = EXCLUDED.title, updated_seq = EXCLUDED.updated_seq, updated_at = now()
WHERE personal_video_grants.state = 1;

-- name: ConfirmGrantTitle :execrows
UPDATE personal_video_grants
SET title = sqlc.arg(title), state = 2, updated_seq = nextval('catalog_seq'), updated_at = now()
WHERE video_id = sqlc.arg(video_id) AND tg_user_id = sqlc.arg(tg_user_id);

-- name: LatestPendingGrant :one
-- The title prompt a free-text reply answers when main-bot no longer remembers which prompt it
-- sent (a restart): the newest grant still waiting for this person's title.
SELECT video_id, title FROM personal_video_grants
WHERE tg_user_id = $1 AND state = 1
ORDER BY created_at DESC
LIMIT 1;

-- name: CountPersonalUploadsSince :one
-- Quota: new files this person put into storage since a moment. Grants on files someone else
-- stored first cost nothing and are not counted.
SELECT count(*)::bigint FROM personal_videos
WHERE uploaded_by_tg = sqlc.arg(tg_user_id) AND created_at >= sqlc.arg(since);

-- name: PersonalLocationForVideo :one
-- Where deliveryd copies from: personal_clone first, then personal_backup -- the same fallback
-- order FileLocations uses for the catalog.
SELECT channel_id, message_id FROM personal_video_locations
WHERE video_id = $1
ORDER BY CASE role WHEN 'personal_clone' THEN 0 ELSE 1 END
LIMIT 1;

-- name: PersonalCloneMissingBackup :many
-- personal_clone copies backupd has not yet copied into personal_backup. Keyset by video id, for
-- the same reason as CloneLocationsMissingRole: a row that can never be copied must not head every
-- page.
SELECT pl.video_id, pl.channel_id, pl.message_id
FROM personal_video_locations pl
WHERE pl.role = 'personal_clone'
  AND pl.video_id > sqlc.arg(after_video_id)
  AND NOT EXISTS (
    SELECT 1 FROM personal_video_locations b
    WHERE b.video_id = pl.video_id AND b.role = 'personal_backup'
  )
ORDER BY pl.video_id
LIMIT sqlc.arg(page_size);

-- name: DeletePersonalGrant :execrows
-- ADR 0038: apid ends one person's access. The video, its channel copies and everyone else's
-- grants stay.
DELETE FROM personal_video_grants WHERE video_id = $1 AND tg_user_id = $2;

-- name: EnsurePersonalShare :exec
-- One link per video: the first share makes it, every later one reuses it.
INSERT INTO personal_video_shares (token, video_id, title, created_by_tg)
VALUES (sqlc.arg(token), sqlc.arg(video_id), sqlc.arg(title), sqlc.arg(created_by_tg))
ON CONFLICT (video_id) DO NOTHING;

-- name: PersonalShareByVideo :one
SELECT token, title FROM personal_video_shares WHERE video_id = $1;

-- name: PersonalShareByToken :one
SELECT video_id, title, created_by_tg FROM personal_video_shares WHERE token = $1;

-- name: InsertSharedGrant :execrows
-- Claiming a link: a ready grant with the link's title. Zero rows means they already had access.
INSERT INTO personal_video_grants (video_id, tg_user_id, state, title, granted_by_tg, updated_seq)
VALUES (sqlc.arg(video_id), sqlc.arg(tg_user_id), 2, sqlc.arg(title), sqlc.arg(granted_by_tg), nextval('catalog_seq'))
ON CONFLICT (video_id, tg_user_id) DO NOTHING;
