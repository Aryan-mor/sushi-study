-- Playback progress. Keyed on (user, episode), not the file: several encodings are the same
-- film, and switching quality must not restart. last_file_id is a preference (docs/06).

-- name: UpsertUserEpisodeState :exec
-- kind 1 = catalog episode, 2 = personal video (ADR 0037): episode_id holds the personal id.
INSERT INTO user_episode_state (user_id, kind, episode_id, position_s, done, last_file_id, played_at, seq)
VALUES (sqlc.arg(user_id), sqlc.arg(kind), sqlc.arg(episode_id), sqlc.arg(position_s), sqlc.arg(done), sqlc.narg(last_file_id), now(), nextval('user_episode_state_seq'))
ON CONFLICT (user_id, kind, episode_id) DO UPDATE SET
  position_s = EXCLUDED.position_s,
  done = EXCLUDED.done,
  last_file_id = COALESCE(EXCLUDED.last_file_id, user_episode_state.last_file_id),
  played_at = now(),
  seq = nextval('user_episode_state_seq');

-- name: WatchedStateSince :many
-- Cross-device sync (docs/11 §6.1): everything this user's user_episode_state has learned since
-- watermark, ordered so the client's next watermark is just the last row's seq.
SELECT kind, episode_id, done, seq
FROM user_episode_state
WHERE user_id = sqlc.arg(user_id) AND seq > sqlc.arg(watermark)
ORDER BY seq
LIMIT sqlc.arg(row_limit);

-- name: UserEpisodeProgress :one
SELECT position_s, done, last_file_id
FROM user_episode_state
WHERE user_id = sqlc.arg(user_id) AND kind = 1 AND episode_id = sqlc.arg(episode_id);

-- name: FileDurationForEpisode :one
SELECT duration_s FROM files
WHERE id = sqlc.arg(file_id) AND episode_id = sqlc.arg(episode_id);

-- name: EpisodeItemKind :one
SELECT e.item_id, i.kind
FROM episodes e
JOIN items i ON i.id = e.item_id
WHERE e.id = sqlc.arg(episode_id);

-- name: HasRecentPlayEvent :one
SELECT EXISTS(
  SELECT 1 FROM play_events
  WHERE user_id = sqlc.arg(user_id)
    AND episode_id = sqlc.arg(episode_id)
    AND at > now() - make_interval(mins => sqlc.arg(mins)::int)
)::bool AS present;

-- name: InsertPlayEvent :exec
INSERT INTO play_events (user_id, item_id, episode_id, file_id, kind)
VALUES (sqlc.arg(user_id), sqlc.arg(item_id), sqlc.arg(episode_id), sqlc.narg(file_id), sqlc.arg(kind));
