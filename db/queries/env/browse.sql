-- Browse titles, lists, user flags (ADR 0009). Env-writable; never touch replicated items.

-- name: BrowseTitleByTMDB :one
SELECT tmdb_id, kind, title, year, rating, poster, overview, released_on, detail
FROM browse_titles WHERE tmdb_id = $1 AND kind = $2;

-- name: UpsertBrowseTitle :exec
INSERT INTO browse_titles (tmdb_id, kind, title, year, rating, poster, overview, released_on, detail, fetched_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
ON CONFLICT (tmdb_id, kind) DO UPDATE SET
  title = EXCLUDED.title,
  year = EXCLUDED.year,
  rating = EXCLUDED.rating,
  poster = EXCLUDED.poster,
  overview = EXCLUDED.overview,
  released_on = EXCLUDED.released_on,
  detail = EXCLUDED.detail,
  fetched_at = now();

-- name: ListMovies :many
-- sort=2 (YEAR) is premiere date, not the year column: two 2024 titles must order by day.
-- sort=5 (TRENDING) / sort=6 (MOST_WATCHED): items already materialised into that rail (env
-- `rails`, docs/12 §2) come first in rail rank order; the rail is a capped top-N (R-META-6), not
-- a full ranking, so everything else follows newest-first rather than pretending there's more rail.
SELECT items.tmdb_id, items.kind, items.title, items.year, items.rating, items.poster
FROM items
LEFT JOIN rails trending_rail
  ON trending_rail.kind = 3 AND trending_rail.tab = 1 AND trending_rail.item_id = items.id
LEFT JOIN rails most_watched_rail
  ON most_watched_rail.kind = 2 AND most_watched_rail.tab = 1 AND most_watched_rail.item_id = items.id
WHERE items.kind = 1
  AND (sqlc.arg(genre)::text = '' OR detail->'genreList' @> to_jsonb(ARRAY[sqlc.arg(genre)::text]))
  AND (sqlc.arg(year)::int = 0 OR year = sqlc.arg(year)::int)
  AND (sqlc.arg(q)::text = '' OR title ILIKE '%' || sqlc.arg(q)::text || '%')
ORDER BY
  CASE WHEN sqlc.arg(sort)::int = 5 THEN trending_rail.rank END ASC NULLS LAST,
  CASE WHEN sqlc.arg(sort)::int = 5 THEN released_on END DESC NULLS LAST,
  CASE WHEN sqlc.arg(sort)::int = 6 THEN most_watched_rail.rank END ASC NULLS LAST,
  CASE WHEN sqlc.arg(sort)::int = 6 THEN released_on END DESC NULLS LAST,
  CASE WHEN sqlc.arg(sort)::int = 2 AND NOT sqlc.arg(sort_desc)::bool THEN released_on END ASC NULLS LAST,
  CASE WHEN sqlc.arg(sort)::int = 2 AND sqlc.arg(sort_desc)::bool THEN released_on END DESC NULLS LAST,
  CASE WHEN sqlc.arg(sort)::int = 3 AND NOT sqlc.arg(sort_desc)::bool THEN rating END ASC,
  CASE WHEN sqlc.arg(sort)::int = 3 AND sqlc.arg(sort_desc)::bool THEN rating END DESC,
  CASE WHEN sqlc.arg(sort)::int = 4 AND NOT sqlc.arg(sort_desc)::bool THEN created_at END ASC,
  CASE WHEN sqlc.arg(sort)::int = 4 AND sqlc.arg(sort_desc)::bool THEN created_at END DESC,
  CASE WHEN NOT sqlc.arg(sort_desc)::bool THEN title END ASC,
  CASE WHEN sqlc.arg(sort_desc)::bool THEN title END DESC
LIMIT sqlc.arg(lim)::int OFFSET sqlc.arg(off)::int;

-- name: ListSeries :many
-- sort=2 is TMDB last episode air date (detail.lastAirDate), falling back to first-air.
-- Ingest clock is the wrong signal: indexing Breaking Bad yesterday must not beat a 2026 premiere.
-- sort=5/6: see ListMovies above — same rails-first, newest-fallback ordering, tab=2 (series).
SELECT items.tmdb_id, items.kind, items.title, items.year, items.rating, items.poster
FROM items
LEFT JOIN rails trending_rail
  ON trending_rail.kind = 3 AND trending_rail.tab = 2 AND trending_rail.item_id = items.id
LEFT JOIN rails most_watched_rail
  ON most_watched_rail.kind = 2 AND most_watched_rail.tab = 2 AND most_watched_rail.item_id = items.id
WHERE items.kind = 2
  AND (sqlc.arg(genre)::text = '' OR detail->'genreList' @> to_jsonb(ARRAY[sqlc.arg(genre)::text]))
  AND (sqlc.arg(year)::int = 0 OR year = sqlc.arg(year)::int)
  AND (sqlc.arg(q)::text = '' OR title ILIKE '%' || sqlc.arg(q)::text || '%')
ORDER BY
  CASE WHEN sqlc.arg(sort)::int = 5 THEN trending_rail.rank END ASC NULLS LAST,
  CASE WHEN sqlc.arg(sort)::int = 5 THEN released_on END DESC NULLS LAST,
  CASE WHEN sqlc.arg(sort)::int = 6 THEN most_watched_rail.rank END ASC NULLS LAST,
  CASE WHEN sqlc.arg(sort)::int = 6 THEN released_on END DESC NULLS LAST,
  CASE WHEN sqlc.arg(sort)::int = 2 AND NOT sqlc.arg(sort_desc)::bool THEN COALESCE(
    NULLIF(detail->>'lastAirDate', '')::date, released_on
  ) END ASC NULLS LAST,
  CASE WHEN sqlc.arg(sort)::int = 2 AND sqlc.arg(sort_desc)::bool THEN COALESCE(
    NULLIF(detail->>'lastAirDate', '')::date, released_on
  ) END DESC NULLS LAST,
  CASE WHEN sqlc.arg(sort)::int = 3 AND NOT sqlc.arg(sort_desc)::bool THEN rating END ASC,
  CASE WHEN sqlc.arg(sort)::int = 3 AND sqlc.arg(sort_desc)::bool THEN rating END DESC,
  CASE WHEN sqlc.arg(sort)::int = 4 AND NOT sqlc.arg(sort_desc)::bool THEN created_at END ASC,
  CASE WHEN sqlc.arg(sort)::int = 4 AND sqlc.arg(sort_desc)::bool THEN created_at END DESC,
  CASE WHEN NOT sqlc.arg(sort_desc)::bool THEN title END ASC,
  CASE WHEN sqlc.arg(sort_desc)::bool THEN title END DESC
LIMIT sqlc.arg(lim)::int OFFSET sqlc.arg(off)::int;

-- name: ListBoxsetMetas :many
-- One card per collection: the collection's own id/name (never a member movie's), newest member's
-- poster as the stand-in art, and how many items it has.
--
-- The COALESCE on collectionId is gone on purpose. `detail->>'collectionId'` is NULL when the key
-- is absent and catalog.MarshalDetail never writes a zero, so `detail ? 'collectionId'` says the
-- same thing -- and it says it in the exact shape of items_collection_idx, which the wrapped
-- expression could never match. DISTINCT ON needs the leading ORDER BY term anyway, and the index
-- now supplies both it and the `year DESC` tiebreak, instead of the catalog being sorted from
-- scratch per page. The COUNT(*) window runs before DISTINCT ON collapses each collection to one
-- row, so it still counts every member.
SELECT DISTINCT ON ((detail->>'collectionId')::bigint)
  (detail->>'collectionId')::bigint AS collection_id,
  COALESCE(NULLIF(detail->>'collectionName', ''), title) AS name,
  poster,
  COUNT(*) OVER (PARTITION BY (detail->>'collectionId')::bigint)::int AS item_count
FROM items
WHERE detail ? 'collectionId'
  AND (sqlc.arg(q)::text = '' OR COALESCE(detail->>'collectionName', title) ILIKE '%' || sqlc.arg(q)::text || '%')
ORDER BY (detail->>'collectionId')::bigint, year DESC NULLS LAST
LIMIT sqlc.arg(lim)::int OFFSET sqlc.arg(off)::int;

-- name: ListBoxsetItems :many
-- Members of one TMDB collection (ListScope BOXSET), newest first. Same items_collection_idx as
-- ListBoxsetMetas, pointed at one collection instead of grouped across all of them.
SELECT tmdb_id, kind, title, year, rating, poster
FROM items
WHERE (detail->>'collectionId')::bigint = sqlc.arg(collection_id)::bigint
ORDER BY year DESC NULLS LAST, title ASC
LIMIT sqlc.arg(lim)::int OFFSET sqlc.arg(off)::int;

-- name: ListUserFlags :many
-- title_cards (env/018) already picked catalog over browse cache; the COALESCE against u here is
-- a different thing entirely -- a flagged title with neither source (browse_titles' TTL expired it
-- and it never made the catalog) must still show up, blank metadata and all, rather than silently
-- drop out of somebody's favourites.
SELECT COALESCE(tc.tmdb_id, u.tmdb_id) AS tmdb_id,
       COALESCE(tc.kind, u.kind) AS kind,
       COALESCE(tc.title, '') AS title,
       COALESCE(tc.year, 0) AS year,
       COALESCE(tc.rating, 0)::smallint AS rating,
       COALESCE(tc.poster, '') AS poster
FROM user_item_state u
LEFT JOIN title_cards tc ON tc.tmdb_id = u.tmdb_id AND tc.kind = u.kind
WHERE u.user_id = sqlc.arg(user_id)
  AND (
    (sqlc.arg(flag)::text = 'favorite' AND u.favorite)
    OR (sqlc.arg(flag)::text = 'later' AND u.watch_later)
  )
  AND (sqlc.arg(q)::text = '' OR COALESCE(tc.title, '') ILIKE '%' || sqlc.arg(q)::text || '%')
ORDER BY
  CASE WHEN sqlc.arg(flag)::text = 'later' THEN u.updated_at END DESC NULLS LAST,
  COALESCE(tc.title, '') ASC
LIMIT sqlc.arg(lim)::int OFFSET sqlc.arg(off)::int;

-- name: LatestWatchLater :one
-- Newest watch-later of this kind, for the home slider (news "watch in app" tap).
SELECT COALESCE(tc.tmdb_id, u.tmdb_id) AS tmdb_id,
       COALESCE(tc.kind, u.kind) AS kind,
       COALESCE(tc.title, '') AS title,
       COALESCE(tc.year, 0) AS year,
       COALESCE(tc.rating, 0)::smallint AS rating,
       COALESCE(tc.poster, '') AS poster
FROM user_item_state u
LEFT JOIN title_cards tc ON tc.tmdb_id = u.tmdb_id AND tc.kind = u.kind
WHERE u.user_id = $1
  AND u.watch_later
  AND u.kind = $2
ORDER BY u.updated_at DESC
LIMIT 1;

-- name: UserItemFlags :one
-- The favourite / watch-later pair for one title, for ItemRes (ADR 0032). A user who has never
-- touched this title has no row, so COALESCE answers false rather than "not found" -- the title page
-- wants two booleans, not an error to handle.
SELECT COALESCE(u.favorite, false)::bool    AS favorite,
       COALESCE(u.watch_later, false)::bool AS watch_later
FROM (SELECT 1) AS one
LEFT JOIN user_item_state u
  ON u.user_id = $1 AND u.tmdb_id = $2 AND u.kind = $3;

-- name: UpsertUserFavorite :exec
INSERT INTO user_item_state (user_id, tmdb_id, kind, favorite, watch_later, updated_at)
VALUES ($1, $2, $3, $4, false, now())
ON CONFLICT (user_id, tmdb_id, kind) DO UPDATE SET
  favorite = EXCLUDED.favorite,
  updated_at = now();

-- name: UpsertUserLater :exec
INSERT INTO user_item_state (user_id, tmdb_id, kind, favorite, watch_later, updated_at)
VALUES ($1, $2, $3, false, $4, now())
ON CONFLICT (user_id, tmdb_id, kind) DO UPDATE SET
  watch_later = EXCLUDED.watch_later,
  updated_at = now();

-- name: UpsertUserNotInterested :exec
-- Hide (or unhide) one title from Home. Does not touch favorite / watch_later.
INSERT INTO user_item_state (user_id, tmdb_id, kind, favorite, watch_later, not_interested, updated_at)
VALUES ($1, $2, $3, false, false, $4, now())
ON CONFLICT (user_id, tmdb_id, kind) DO UPDATE SET
  not_interested = EXCLUDED.not_interested,
  updated_at = now();

-- name: ListNotInterested :many
-- Every title this viewer hid from Home. Home sends the set even on a rail cache hit so
-- client-owned rows (continue watching) can drop them without a rail rebuild.
SELECT tmdb_id, kind
FROM user_item_state
WHERE user_id = $1 AND not_interested;

-- name: ListPlaylists :many
SELECT p.id, p.name, COUNT(pi.tmdb_id)::int AS item_count
FROM playlists p
LEFT JOIN playlist_items pi ON pi.playlist_id = p.id
WHERE p.user_id = $1
GROUP BY p.id, p.name
ORDER BY p.updated_at DESC;

-- name: ListPlaylistItems :many
-- Same shape as ListUserFlags and the same reason for the outer COALESCE: a playlist entry outlives
-- both of its possible sources going stale, and must keep its place in the list rather than vanish.
SELECT COALESCE(tc.tmdb_id, pi.tmdb_id) AS tmdb_id,
       COALESCE(tc.kind, pi.kind) AS kind,
       COALESCE(tc.title, '') AS title,
       COALESCE(tc.year, 0) AS year,
       COALESCE(tc.rating, 0)::smallint AS rating,
       COALESCE(tc.poster, '') AS poster
FROM playlist_items pi
LEFT JOIN title_cards tc ON tc.tmdb_id = pi.tmdb_id AND tc.kind = pi.kind
WHERE pi.playlist_id = $1
ORDER BY pi.rank, pi.added_at
LIMIT sqlc.arg(lim)::int OFFSET sqlc.arg(off)::int;

-- name: CompactRowsByTMDBAnyKind :many
SELECT tmdb_id, kind, title, year, rating, poster
FROM items
WHERE tmdb_id = ANY(sqlc.arg(tmdb_ids)::bigint[])
LIMIT 48;

-- name: BrowsePersonByTMDB :one
SELECT tmdb_id, name, profile, biography, movies, series
FROM browse_people WHERE tmdb_id = $1;

-- name: UpsertBrowsePerson :exec
INSERT INTO browse_people (tmdb_id, name, profile, biography, movies, series, fetched_at)
VALUES ($1, $2, $3, $4, $5, $6, now())
ON CONFLICT (tmdb_id) DO UPDATE SET
  name = EXCLUDED.name,
  profile = EXCLUDED.profile,
  biography = EXCLUDED.biography,
  movies = EXCLUDED.movies,
  series = EXCLUDED.series,
  fetched_at = now();
