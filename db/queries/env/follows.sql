-- Series follow (ADR 0014 §D3). Toggling a follow, the watermark scan, and the coalesced flush.
-- items / episodes / files are replicated and read-only here (R-LIB-3); kind = 2 is a series.

-- name: SeriesMaxServeableEpisode :one
-- The highest (season_no, episode_no) of this series that has at least one ready file — the
-- "arrived" marker the scan compares against and a new follow seeds from. (0, 0) when nothing is
-- serveable yet.
SELECT
  COALESCE(top.season_no, 0)::int  AS season_no,
  COALESCE(top.episode_no, 0)::int AS episode_no
FROM (SELECT 1) AS _
LEFT JOIN LATERAL (
  SELECT e.season_no, e.episode_no
  FROM episodes e
  JOIN items i ON i.id = e.item_id
  WHERE i.tmdb_id = $1 AND i.kind = 2
    AND EXISTS (SELECT 1 FROM files f WHERE f.episode_id = e.id AND f.state = 1)
  ORDER BY e.season_no DESC, e.episode_no DESC
  LIMIT 1
) AS top ON true;

-- name: UpsertSeriesFollow :exec
-- Toggle on. seen_* is seeded by the caller to the series' current max so a brand-new follow never
-- fires for episodes that already exist (ADR 0014 §D3). Re-following is a no-op.
INSERT INTO series_follows (user_id, tmdb_id, seen_season_no, seen_episode_no)
VALUES ($1, $2, sqlc.arg(seen_season)::int, sqlc.arg(seen_episode)::int)
ON CONFLICT (user_id, tmdb_id) DO NOTHING;

-- name: DeleteSeriesFollow :exec
DELETE FROM series_follows WHERE user_id = $1 AND tmdb_id = $2;

-- name: EnsureFollowedSeriesState :exec
-- The shared debounce row, created on first follow, seeded to the current catalog max so the flush
-- pass has a baseline that will not immediately look "new".
INSERT INTO followed_series_state (tmdb_id, max_season_no, max_episode_no)
VALUES ($1, sqlc.arg(max_season)::int, sqlc.arg(max_episode)::int)
ON CONFLICT (tmdb_id) DO NOTHING;

-- name: GCFollowedSeriesState :exec
-- Drop the shared row once the last follower is gone (called after DeleteSeriesFollow).
DELETE FROM followed_series_state s
WHERE s.tmdb_id = $1 AND NOT EXISTS (SELECT 1 FROM series_follows f WHERE f.tmdb_id = s.tmdb_id);

-- name: FollowedSeriesForScan :many
-- Every series with at least one follower, plus its recorded max, for the watermark scan.
SELECT s.tmdb_id, s.max_season_no, s.max_episode_no
FROM followed_series_state s
WHERE EXISTS (SELECT 1 FROM series_follows f WHERE f.tmdb_id = s.tmdb_id)
LIMIT $1;

-- name: BumpFollowedSeriesMax :execrows
-- The scan found a higher serveable episode: move the max and reset the shared debounce clock.
UPDATE followed_series_state
SET max_season_no = $2, max_episode_no = $3, changed_at = now()
WHERE tmdb_id = $1 AND ($2, $3) > (max_season_no, max_episode_no);

-- name: FollowNoticesDue :many
-- (follower, series) pairs whose series has a newer max than they have seen AND whose series has
-- been quiet past the debounce cutoff. new_count is how many newly-serveable episodes they missed,
-- so the DM can say "+N more". poster and year carry the card's artwork and meta line; locale is
-- the language the DM is written in (R-I18N-10).
SELECT
  f.user_id,
  u.tg_user_id,
  f.tmdb_id,
  s.max_season_no,
  s.max_episode_no,
  COALESCE(tc.title, '')::text AS title,
  COALESCE(tc.poster, '')::text AS poster,
  COALESCE(tc.year, 0)::int AS year,
  COALESCE(p.locale, '')::text AS locale,
  (SELECT count(*) FROM episodes e
   JOIN items i ON i.id = e.item_id
   WHERE i.tmdb_id = f.tmdb_id AND i.kind = 2
     AND EXISTS (SELECT 1 FROM files ff WHERE ff.episode_id = e.id AND ff.state = 1)
     AND (e.season_no, e.episode_no) > (f.seen_season_no, f.seen_episode_no))::int AS new_count
FROM series_follows f
JOIN followed_series_state s ON s.tmdb_id = f.tmdb_id
JOIN users u ON u.id = f.user_id
LEFT JOIN user_prefs p ON p.user_id = f.user_id
LEFT JOIN title_cards tc ON tc.tmdb_id = f.tmdb_id AND tc.kind = 2
WHERE (s.max_season_no, s.max_episode_no) > (f.seen_season_no, f.seen_episode_no)
  AND s.changed_at < sqlc.arg(cutoff)
ORDER BY f.tmdb_id
LIMIT $1;

-- name: AdvanceFollowSeen :exec
UPDATE series_follows
SET seen_season_no = $3, seen_episode_no = $4
WHERE user_id = $1 AND tmdb_id = $2;

-- name: SyncFollowSeenToSeriesMax :exec
-- After a "your requested title is ready" DM (ADR 0014 §D2), catch that user's follow up to the
-- series max so the follow reconciler does not send a near-duplicate "new episode" DM.
UPDATE series_follows f
SET seen_season_no = s.max_season_no, seen_episode_no = s.max_episode_no
FROM followed_series_state s
WHERE f.tmdb_id = s.tmdb_id AND f.user_id = $1 AND f.tmdb_id = $2;
