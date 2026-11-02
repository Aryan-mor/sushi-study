
-- +goose Up

UPDATE source_refs sr
SET ext_key = o.src_channel_id::text || ':' || o.src_msg_id::text
FROM (
  SELECT DISTINCT ON (sr.file_id)
         sr.file_id,
         iq.src_channel_id,
         iq.src_msg_id
  FROM source_refs sr
  JOIN ingest_queue iq
    ON iq.file_id = sr.file_id
   AND iq.src_msg_id::text = sr.ext_key
  JOIN files f ON f.id = sr.file_id
  JOIN episodes e ON e.id = f.episode_id
  JOIN items i ON i.id = e.item_id
  WHERE sr.source = 'tgforward'
    AND sr.ext_key ~ '^[0-9]+$'
  ORDER BY sr.file_id,
           CASE
             WHEN NULLIF(iq.verdict->>'tmdb_id', '')::bigint = i.tmdb_id
              AND COALESCE(NULLIF(iq.verdict->>'season', '')::int, 0) = e.season_no
              AND COALESCE(NULLIF(iq.verdict->>'episode', '')::int, 0) = e.episode_no
             THEN 0
             ELSE 1
           END,
           iq.id ASC
) o
WHERE sr.source = 'tgforward'
  AND sr.file_id = o.file_id
  AND sr.ext_key ~ '^[0-9]+$';

-- +goose Down

UPDATE source_refs sr
SET ext_key = split_part(sr.ext_key, ':', 2)
WHERE sr.source = 'tgforward'
  AND sr.ext_key LIKE '%:%'
  AND split_part(sr.ext_key, ':', 1) LIKE '-%'
  AND NOT EXISTS (
    SELECT 1 FROM source_refs other
    WHERE other.source = 'tgforward'
      AND other.ext_key = split_part(sr.ext_key, ':', 2)
      AND other.ctid <> sr.ctid
  );
