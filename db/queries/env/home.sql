-- Home. Every query reads Sushi's own tables: no filters undoing another product's semantics,
-- because normalisation happened once, on the way in (ADR 0003).

-- name: RailRows :many
SELECT i.id, i.tmdb_id, i.kind, i.title, i.year, i.rating, i.poster
FROM rails r
JOIN items i ON i.id = r.item_id
WHERE r.kind = $1 AND r.tab = $2
ORDER BY r.rank;

-- name: RailSeq :one
SELECT seq FROM rail_state WHERE only_row;

-- name: BumpRailSeq :one
UPDATE rail_state SET seq = seq + 1, built_at = now() WHERE only_row RETURNING seq;

-- name: ClearTab :exec
DELETE FROM rails WHERE tab = $1;

-- name: FillRailNewest :execrows
-- The slider: our catalog ranked by release date. Filtering TMDB's newest list instead would be
-- empty most days, because a young library does not have this week's releases (docs/12 §2.1).
INSERT INTO rails (kind, tab, rank, item_id)
SELECT
  sqlc.arg(kind)::smallint,
  sqlc.arg(tab)::smallint,
  (sqlc.arg(rank_from)::int + row_number() OVER () - 1)::smallint,
  s.id
FROM (
  SELECT i.id
  FROM items i
  WHERE i.kind = sqlc.arg(item_kind)::smallint
    AND EXISTS (SELECT 1 FROM episodes e JOIN files f ON f.episode_id = e.id
                WHERE e.item_id = i.id AND f.state = 1)
    AND NOT EXISTS (SELECT 1 FROM rails r WHERE r.tab = sqlc.arg(tab)::smallint AND r.item_id = i.id)
  ORDER BY i.released_on DESC NULLS LAST, i.id DESC
  LIMIT sqlc.arg(row_limit)
) s;

-- name: FillRailMostWatched :execrows
-- Sushi's own play counts now (ADR 0003). Thinner than the shared numbers were, and honest.
INSERT INTO rails (kind, tab, rank, item_id)
SELECT
  sqlc.arg(kind)::smallint,
  sqlc.arg(tab)::smallint,
  (sqlc.arg(rank_from)::int + row_number() OVER () - 1)::smallint,
  s.id
FROM (
  SELECT i.id
  FROM play_events e
  JOIN items i ON i.id = e.item_id
  WHERE i.kind = sqlc.arg(item_kind)::smallint
    AND e.at > now() - make_interval(days => sqlc.arg(window_days)::int)
    AND NOT EXISTS (SELECT 1 FROM rails r WHERE r.tab = sqlc.arg(tab)::smallint AND r.item_id = i.id)
  GROUP BY i.id
  HAVING count(*) >= sqlc.arg(min_plays)::int
  ORDER BY count(*) DESC, i.id
  LIMIT sqlc.arg(row_limit)
) s;

-- name: FillRailByTMDBIDs :execrows
-- Trending: TMDB's weekly list, intersected with our catalog and kept in TMDB's own rank order
-- (docs/12 §2.3). Same "playable file, not already on this screen" gate as every other fill.
INSERT INTO rails (kind, tab, rank, item_id)
SELECT
  sqlc.arg(kind)::smallint,
  sqlc.arg(tab)::smallint,
  (sqlc.arg(rank_from)::int + row_number() OVER () - 1)::smallint,
  s.id
FROM (
  SELECT i.id
  FROM unnest(sqlc.arg(tmdb_ids)::bigint[]) WITH ORDINALITY AS t(tmdb_id, ord)
  JOIN items i ON i.tmdb_id = t.tmdb_id
  WHERE i.kind = sqlc.arg(item_kind)::smallint
    AND EXISTS (SELECT 1 FROM episodes e JOIN files f ON f.episode_id = e.id
                WHERE e.item_id = i.id AND f.state = 1)
    AND NOT EXISTS (SELECT 1 FROM rails r WHERE r.tab = sqlc.arg(tab)::smallint AND r.item_id = i.id)
  ORDER BY t.ord
  LIMIT sqlc.arg(row_limit)
) s;

-- name: FillRailRandom :execrows
-- The floor for a rail that came back short. Random is rolled once, here, and then pinned - never
-- per request, which would reshuffle cards under the user's focus (R-CACHE-2).
INSERT INTO rails (kind, tab, rank, item_id)
SELECT
  sqlc.arg(kind)::smallint,
  sqlc.arg(tab)::smallint,
  (sqlc.arg(rank_from)::int + row_number() OVER () - 1)::smallint,
  s.id
FROM (
  SELECT i.id
  FROM items i
  WHERE i.kind = sqlc.arg(item_kind)::smallint
    AND EXISTS (SELECT 1 FROM episodes e JOIN files f ON f.episode_id = e.id
                WHERE e.item_id = i.id AND f.state = 1)
    AND NOT EXISTS (SELECT 1 FROM rails r WHERE r.tab = sqlc.arg(tab)::smallint AND r.item_id = i.id)
  ORDER BY random()
  LIMIT sqlc.arg(row_limit)
) s;
