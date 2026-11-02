-- Search (docs/12 §6): pg_trgm over the replicated items.title, plus the R-SEC-11 hourly cap.

-- name: SearchItems :many
-- English `title` plus optional `title_fa` (library/029). Rank by the stronger trigram match
-- (R-META-10). Ties broken by id so paging stays stable.
SELECT id, tmdb_id, kind, title, year, rating, poster
FROM items
WHERE title % sqlc.arg(query)::text
   OR (title_fa <> '' AND title_fa % sqlc.arg(query)::text)
ORDER BY GREATEST(
         similarity(title, sqlc.arg(query)::text),
         CASE WHEN title_fa = '' THEN 0 ELSE similarity(title_fa, sqlc.arg(query)::text) END
       ) DESC, id
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: RecordSearch :exec
INSERT INTO search_events (user_id) VALUES ($1);

-- name: SearchCountSince :one
SELECT count(*) FROM search_events WHERE user_id = $1 AND at > $2;

-- name: CatalogKeysByTMDB :many
-- Presence check for SearchRes.missing (ADR 0010): drop TMDB hits already in catalog items.
SELECT tmdb_id, kind
FROM items
WHERE tmdb_id = ANY(sqlc.arg(tmdb_ids)::bigint[]);

-- name: CatalogRowsByTMDBAnyKind :many
-- Compact rows for TMDB search hints already in catalog (Persian alias path).
SELECT tmdb_id, kind, title, year, rating, poster
FROM items
WHERE tmdb_id = ANY(sqlc.arg(tmdb_ids)::bigint[]);
