-- Personal videos, read from the replica (ADR 0037). Two access questions and nothing else: who may
-- see a video, and what one person's "My videos" holds. Sharing later changes these and only these.
--
-- The caller is users.id; grants are keyed by Telegram id, the identity dev and prod share.

-- name: CanViewPersonal :one
SELECT EXISTS (
  SELECT 1
  FROM personal_video_grants g
  JOIN users u ON u.tg_user_id = g.tg_user_id
  WHERE u.id = sqlc.arg(user_id) AND g.video_id = sqlc.arg(video_id) AND g.state = 2
)::bool AS ok;

-- name: ListPersonalVideos :many
-- Newest first. Progress is this environment's user_episode_state, kind = 2.
SELECT
  v.id,
  g.title,
  v.duration_s,
  v.size_bytes,
  COALESCE(ues.position_s, 0)::int AS position_s,
  COALESCE(ues.done, false)::bool AS done
FROM personal_video_grants g
JOIN users u ON u.tg_user_id = g.tg_user_id
JOIN personal_videos v ON v.id = g.video_id
LEFT JOIN user_episode_state ues
  ON ues.user_id = u.id AND ues.kind = 2 AND ues.episode_id = v.id
WHERE u.id = sqlc.arg(user_id) AND g.state = 2
ORDER BY g.created_at DESC, v.id DESC
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- name: PersonalDuration :one
SELECT duration_s FROM personal_videos WHERE id = $1;

-- name: UserTelegramID :one
SELECT tg_user_id FROM users WHERE id = $1;

-- name: EnqueuePersonalShare :exec
-- ADR 0038. A second tap while one is pending is a no-op.
INSERT INTO personal_share_requests (user_id, video_id) VALUES ($1, $2)
ON CONFLICT (user_id, video_id) WHERE sent_at IS NULL DO NOTHING;

-- name: PendingPersonalShares :many
SELECT r.id, r.video_id, r.attempts, u.tg_user_id, COALESCE(p.locale, '')::text AS locale
FROM personal_share_requests r
JOIN users u ON u.id = r.user_id
LEFT JOIN user_prefs p ON p.user_id = r.user_id
WHERE r.sent_at IS NULL
ORDER BY r.id
LIMIT $1;

-- name: MarkPersonalShareSent :exec
UPDATE personal_share_requests SET sent_at = now() WHERE id = $1;

-- name: NotePersonalShareFailure :exec
-- After three tries the row is closed with its error: a person who blocked main-bot is not
-- retried forever.
UPDATE personal_share_requests
SET attempts = attempts + 1, last_error = sqlc.arg(error)::text,
    sent_at = CASE WHEN attempts + 1 >= 3 THEN now() ELSE NULL END
WHERE id = sqlc.arg(id);
