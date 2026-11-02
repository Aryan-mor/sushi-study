-- Opening a title (docs/12 §4) and its pick-list (docs/12 §5). Reads only -- items, episodes and
-- files are replicated from the library and never written here (R-LIB-3).

-- name: ItemByTMDB :one
SELECT id, tmdb_id, kind, title, year, rating, poster, overview, overview_fa, released_on, detail
FROM items WHERE tmdb_id = $1 AND kind = $2;

-- name: EpisodesForItem :many
-- item_id is the library's internal items.id (from ItemByTMDB), never the wire's tmdb_id -- the
-- client never sees this join key, only the episode ids that come back.
SELECT id, season_no, episode_no, title FROM episodes
WHERE item_id = $1 ORDER BY season_no, episode_no;

-- name: EpisodesForItemSeason :many
SELECT id, season_no, episode_no, title FROM episodes
WHERE item_id = sqlc.arg(item_id) AND season_no = sqlc.arg(season_no)
ORDER BY episode_no
LIMIT sqlc.arg(lim)::int OFFSET sqlc.arg(off)::int;

-- name: CountEpisodesForItemSeason :one
SELECT count(*)::int FROM episodes
WHERE item_id = sqlc.arg(item_id) AND season_no = sqlc.arg(season_no);

-- name: FirstPlayableEpisodeForItem :one
-- Header Play needs an episode that actually has a ready file, not S01E01 of a 1100-ep show
-- that we have not ingested yet (ADR 0028).
SELECT e.id, e.season_no, e.episode_no, e.title
FROM items i
JOIN episodes e ON e.item_id = i.id
WHERE i.tmdb_id = sqlc.arg(tmdb_id) AND i.kind = sqlc.arg(kind)
  AND e.season_no > 0
  AND EXISTS (SELECT 1 FROM files f WHERE f.episode_id = e.id AND f.state = 1)
ORDER BY e.season_no, e.episode_no
LIMIT 1;

-- name: FilesForEpisode :many
-- Ready (1) sorts before pending (2) before unavailable (3). Within a state, a file with
-- a custom display_name sorts ahead of height so the pinned file is first.
SELECT id, quality_label, display_name, height, audio_langs, sub_langs, size_bytes, duration_s, state
FROM files WHERE episode_id = $1
ORDER BY state, (display_name <> '') DESC, height DESC;

-- name: FileLocations :many
-- catalog-bot copies from whichever copy it can see: clone first, then backup1… so a missing
-- clone membership still delivers from a backup the bot is in (the prototype provider-user-bot).
SELECT channel_id, message_id, role
FROM file_locations
WHERE file_id = $1
ORDER BY CASE role
  WHEN 'clone' THEN 0
  WHEN 'backup1' THEN 1
  WHEN 'backup2' THEN 2
  WHEN 'backup3' THEN 3
  ELSE 9
END;

-- name: FileByID :one
SELECT id, quality_label, display_name, height, audio_langs, sub_langs, size_bytes, duration_s, state
FROM files WHERE id = $1;

-- name: FileOrigin :one
-- Title + season/episode for a file the catalog-bot is about to copy. Without this join the
-- delivery caption only had the files row, so TMDB printed as 0 and the episode was unnamed.
SELECT
  f.id, f.quality_label, f.display_name, f.height, f.audio_langs, f.sub_langs, f.size_bytes, f.duration_s, f.state,
  i.tmdb_id, i.kind, i.title, i.year,
  e.season_no, e.episode_no, e.title AS episode_title
FROM files f
JOIN episodes e ON e.id = f.episode_id
JOIN items i ON i.id = e.item_id
WHERE f.id = $1;

-- name: CompactRowsByTMDB :many
SELECT tmdb_id, kind, title, year, rating, poster
FROM items
WHERE kind = sqlc.arg(kind) AND tmdb_id = ANY(sqlc.arg(tmdb_ids)::bigint[])
LIMIT 24;

-- name: CompactRowsByCollection :many
-- `detail ? 'collectionId'` first, and the bare cast second, so this matches items_collection_idx
-- exactly (env/011). Wrapped in COALESCE it matched nothing, and every title page paid a full scan
-- of the catalog for its "more from this collection" strip.
SELECT tmdb_id, kind, title, year, rating, poster
FROM items
WHERE detail ? 'collectionId'
  AND (detail->>'collectionId')::bigint = sqlc.arg(collection_id)::bigint
  AND NOT (tmdb_id = sqlc.arg(exclude_tmdb) AND kind = sqlc.arg(kind))
LIMIT 16;
