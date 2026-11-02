-- Subtitle cache (doc 15 §7): apid's own read+write data, populated on demand as titles are
-- requested -- see 039_subtitles.sql's doc comment for why this lives in env, not library.

-- name: ListCachedSubtitles :many
SELECT channel_id, message_id, ext, provider, sub_type, duration_min
FROM subtitles
WHERE tmdb_id = $1 AND kind = $2 AND season_no = $3 AND episode_no = $4 AND lang = $5;

-- name: PutCachedSubtitle :exec
-- Upsert one cut (type + minutes). A different cut of the same episode is a different row.
INSERT INTO subtitles (
  tmdb_id, kind, season_no, episode_no, lang, provider, ext, channel_id, message_id, sub_type, duration_min
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (tmdb_id, kind, season_no, episode_no, lang, sub_type, duration_min) DO UPDATE SET
  provider = EXCLUDED.provider,
  ext = EXCLUDED.ext,
  channel_id = EXCLUDED.channel_id,
  message_id = EXCLUDED.message_id,
  created_at = now();
