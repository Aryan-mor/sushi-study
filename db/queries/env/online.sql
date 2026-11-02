-- /admin → Actions → Online. Playback progress lands about every 30s (docs/11), so a few
-- minutes of user_episode_state.played_at or search_events is "in the app right now".

-- name: ListOnlineViewers :many
SELECT u.id, u.tg_name, u.tg_username, max(r.seen_at)::timestamptz AS seen_at,
  COALESCE((
    SELECT string_agg(d.platform, ',' ORDER BY d.seen_at DESC)
    FROM user_devices d
    WHERE d.user_id = u.id
      AND d.seen_at >= now() - make_interval(mins => sqlc.arg(mins)::int)
  ), '')::text AS platforms
FROM (
  SELECT user_id, played_at AS seen_at
  FROM user_episode_state
  WHERE played_at >= now() - make_interval(mins => sqlc.arg(mins)::int)
  UNION ALL
  SELECT user_id, at AS seen_at
  FROM search_events
  WHERE at >= now() - make_interval(mins => sqlc.arg(mins)::int)
  UNION ALL
  SELECT user_id, seen_at
  FROM user_devices
  WHERE seen_at >= now() - make_interval(mins => sqlc.arg(mins)::int)
) r
JOIN users u ON u.id = r.user_id
GROUP BY u.id, u.tg_name, u.tg_username
ORDER BY seen_at DESC
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- name: CountOnlineViewers :one
SELECT count(*)::bigint FROM (
  SELECT user_id FROM user_episode_state
  WHERE played_at >= now() - make_interval(mins => sqlc.arg(mins)::int)
  UNION
  SELECT user_id FROM search_events
  WHERE at >= now() - make_interval(mins => sqlc.arg(mins)::int)
  UNION
  SELECT user_id FROM user_devices
  WHERE seen_at >= now() - make_interval(mins => sqlc.arg(mins)::int)
) r;

-- name: OnlineUserLabel :one
SELECT id, tg_name, tg_username FROM users WHERE id = sqlc.arg(user_id);

-- name: UserWatchLog :many
-- One page, newest first. Page 0 is item 1. Next goes further back.
SELECT
  ues.played_at,
  ues.position_s,
  ues.done,
  i.tmdb_id,
  i.title AS item_title,
  i.kind,
  e.season_no,
  e.episode_no,
  e.title AS episode_title
FROM user_episode_state ues
-- kind = 1: a personal video's id is not an episode id (ADR 0037), and staff never see personal titles.
JOIN episodes e ON ues.kind = 1 AND e.id = ues.episode_id
JOIN items i ON i.id = e.item_id
WHERE ues.user_id = sqlc.arg(user_id)
ORDER BY ues.played_at DESC, ues.episode_id DESC
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- name: UserLatestWatch :one
SELECT
  ues.played_at,
  ues.position_s,
  ues.done,
  i.tmdb_id,
  i.title AS item_title,
  i.kind,
  e.season_no,
  e.episode_no,
  e.title AS episode_title
FROM user_episode_state ues
-- kind = 1: a personal video's id is not an episode id (ADR 0037), and staff never see personal titles.
JOIN episodes e ON ues.kind = 1 AND e.id = ues.episode_id
JOIN items i ON i.id = e.item_id
WHERE ues.user_id = sqlc.arg(user_id)
ORDER BY ues.played_at DESC, ues.episode_id DESC
LIMIT 1;

-- name: TouchUserDevice :exec
INSERT INTO user_devices (user_id, platform, seen_at)
VALUES (sqlc.arg(user_id), sqlc.arg(platform), now())
ON CONFLICT (user_id, platform) DO UPDATE SET seen_at = now();

-- name: BumpLatestUserDevice :exec
-- Playback and search do not carry a platform. Refresh whichever device checked in last.
UPDATE user_devices AS d
SET seen_at = now()
WHERE d.user_id = sqlc.arg(user_id)
  AND d.platform = (
    SELECT platform FROM user_devices
    WHERE user_id = sqlc.arg(user_id)
    ORDER BY seen_at DESC
    LIMIT 1
  );

-- name: ListUserDevices :many
SELECT platform, seen_at
FROM user_devices
WHERE user_id = sqlc.arg(user_id)
  AND seen_at >= now() - make_interval(mins => sqlc.arg(mins)::int)
ORDER BY seen_at DESC;

-- name: UserWatchCount :one
SELECT count(*)::bigint FROM user_episode_state WHERE user_id = sqlc.arg(user_id);
