-- The announce channel (ADR 0017). Discovery reads the replicated catalog (read-only here,
-- R-LIB-3); everything else reads and writes the env-local announcements queue.

-- name: DebutMovies :many
-- Movies whose first ready file appeared inside the window and that nothing has announced yet.
--
-- The window is applied to `files` first, not to an aggregate over the whole table: this runs every
-- 45 seconds, and `files_created_ready_idx` is what keeps it off a sequential scan (R-DATA-5). That
-- also gives `first_at` the meaning we actually want -- when this became playable, as far as this
-- deployment ever saw.
--
-- Freshness matches the prototype isFreshChannelNewsRelease: a known premiere must itself be inside
-- the 90-day window (year must not rescue an old release date). Only a title with no premiere
-- falls back to production year. An empty poster is skipped -- a card with no artwork is not
-- worth a channel post (ADR 0017 §4).
WITH ready AS (
  SELECT e.item_id, min(f.created_at) AS first_at
  FROM files f
  JOIN episodes e ON e.id = f.episode_id
  WHERE f.state = 1 AND f.created_at >= sqlc.arg(since)
  GROUP BY e.item_id
)
SELECT i.tmdb_id,
       i.title,
       i.year,
       i.rating::int                        AS rating,
       i.poster,
       COALESCE(i.detail->>'genres', '')::text AS genres,
       i.overview,
       COALESCE((i.detail->>'runtimeS')::int, 0)::int AS runtime_s,
       COALESCE(i.detail->>'backdrop', '')::text AS backdrop,
       i.released_on,
       COALESCE(r.first_at, now())::timestamptz AS first_available_at
FROM items i
JOIN ready r ON r.item_id = i.id
WHERE i.kind = 1
  AND i.poster <> ''
  AND i.released_on IS NOT NULL
  AND i.released_on >= sqlc.arg(premiered_after)::date
  AND i.released_on <= CURRENT_DATE
  AND NOT EXISTS (
    SELECT 1 FROM announcements a
    WHERE a.tmdb_id = i.tmdb_id AND a.kind = 1
      AND a.season_no IS NULL AND a.episode_no IS NULL
  )
ORDER BY r.first_at DESC
LIMIT sqlc.arg(lim);

-- name: DebutEpisodes :many
-- Episodes that became playable inside the window, for series we have a poster for.
--
-- Freshness is the series first-air date inside 90 days. Year alone is not enough after a
-- catalog resync: files.created_at is "now" for the whole library, so year >= 2025 queued
-- every 2025/2026 episode. We do not store per-episode air dates.
--
-- The second NOT EXISTS is the cursor rule: once we have told the channel about S02E05, a
-- back-fill that later lands S01E03 is not news. Row comparison rather than two clauses, so the
-- ordering is lexicographic on (season, episode) -- which is the order people watch in --
-- and `announcements_series_cursor_idx` serves it.
--
-- Every series debut collapses to one queued row. The reconciler inserts each episode, then
-- DropOlderQueuedSeries leaves only the latest still-queued episode and restarts its hold.
-- Indexing S01E01–S01E10 together therefore previews E10 once. This query still returns every
-- episode; the collapse is the write. A posted episode is left alone, so the next one that
-- lands later is news on its own.
WITH ready AS (
  SELECT f.episode_id, min(f.created_at) AS first_at
  FROM files f
  WHERE f.state = 1 AND f.created_at >= sqlc.arg(since)
  GROUP BY f.episode_id
)
SELECT i.tmdb_id,
       i.title                              AS series_title,
       i.year,
       i.rating::int                        AS rating,
       i.poster,
       COALESCE(i.detail->>'genres', '')::text AS genres,
       -- The episode's own synopsis when it has one, else the series'. An episode overview is what
       -- someone actually wants to read about a new episode.
       COALESCE(NULLIF(e.overview, ''), i.overview)::text AS overview,
       COALESCE((i.detail->>'runtimeS')::int, 0)::int AS runtime_s,
       i.released_on,
       e.season_no,
       e.episode_no,
       e.title                              AS episode_title,
       COALESCE(i.detail->>'backdrop', '')::text AS backdrop,
       COALESCE(r.first_at, now())::timestamptz AS first_available_at
FROM episodes e
JOIN ready r ON r.episode_id = e.id
JOIN items i ON i.id = e.item_id
WHERE i.kind = 2
  AND i.poster <> ''
  AND i.released_on IS NOT NULL
  AND i.released_on >= sqlc.arg(premiered_after)::date
  AND i.released_on <= CURRENT_DATE
  AND NOT EXISTS (
    SELECT 1 FROM announcements a
    WHERE a.tmdb_id = i.tmdb_id AND a.kind = 2
      AND a.season_no = e.season_no AND a.episode_no = e.episode_no
  )
  AND NOT EXISTS (
    SELECT 1 FROM announcements a2
    WHERE a2.tmdb_id = i.tmdb_id AND a2.kind = 2
      AND a2.status IN (2, 3)
      AND (a2.season_no, a2.episode_no) > (e.season_no, e.episode_no)
  )
ORDER BY r.first_at DESC
LIMIT sqlc.arg(lim);

-- name: InsertAnnouncement :one
-- Queues one debut. The conflict target is announcements_live_uidx: a row that is already queued
-- or awaiting approval returns nothing, which the reconciler reads as "already known". A posted
-- row does not conflict — staff may draft the same title again (migration 052). Discovery still
-- skips anything with a row, so the ticker will not re-queue a posted title by itself.
INSERT INTO announcements (
  tmdb_id, kind, season_no, episode_no, title, subtitle, poster, genres, overview, runtime_s,
  year, rating, premiere_on, first_available_at, announce_after, manual
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
ON CONFLICT (tmdb_id, kind, COALESCE(season_no, 0), COALESCE(episode_no, 0))
  WHERE status IN (1, 2)
  DO NOTHING
RETURNING id;

-- name: DueAnnouncements :many
-- Queued rows whose delay has elapsed, oldest first so the backlog drains in the order it arrived.
-- Backdrop, country and title logo are joined from the replica so a slideshow / hero card can
-- use stored extras without a migration on announcements itself (ADR 0017 §6).
SELECT a.id, a.tmdb_id, a.kind, a.season_no, a.episode_no, a.title, a.subtitle, a.poster, a.genres,
       a.overview, a.runtime_s, a.year, a.rating, a.premiere_on, a.manual,
       COALESCE(i.detail->>'backdrop', '')::text AS backdrop,
       COALESCE(i.detail->>'backdrops', '')::text AS backdrops,
       COALESCE(i.detail->>'country', '')::text AS country,
       COALESCE(i.detail->>'logo', '')::text AS logo,
       COALESCE(i.detail->>'network', '')::text AS network,
       COALESCE(i.detail->>'trailerKey', '')::text AS trailer_key,
       -- The stored Persian synopsis (library/022), but only a translation of the very text this
       -- row captions: the episode's when that is what we queued, else the title's.
       COALESCE(
         NULLIF(CASE WHEN e.overview = a.overview THEN e.overview_fa END, ''),
         NULLIF(CASE WHEN i.overview = a.overview THEN i.overview_fa END, ''),
         ''
       )::text AS overview_fa
FROM announcements a
LEFT JOIN items i ON i.tmdb_id = a.tmdb_id AND i.kind = a.kind
LEFT JOIN episodes e ON e.item_id = i.id
  AND e.season_no = COALESCE(a.season_no, 0) AND e.episode_no = COALESCE(a.episode_no, 0)
WHERE a.status = 1 AND a.announce_after <= now()
ORDER BY a.announce_after
LIMIT $1;

-- name: MarkAnnouncementAwaitingApproval :exec
-- Records which preview message staff are looking at, so approve/reject can edit it in place.
UPDATE announcements
SET status = 2, admin_chat_id = $2, admin_message_id = $3
WHERE id = $1 AND status = 1;

-- name: MarkAnnouncementSkipped :exec
-- A due row we could not even show staff (no recipients configured). Parked rather than retried
-- forever, so the due scan does not stall behind it every 45 seconds.
UPDATE announcements SET status = 5 WHERE id = $1 AND status = 1;

-- name: AnnouncementForDecision :one
-- Everything the caption needs, for the approve/reject press. Status comes along so a second press
-- on a stale keyboard can be answered "already handled" instead of posting twice.
SELECT a.id, a.tmdb_id, a.kind, a.season_no, a.episode_no, a.title, a.subtitle, a.poster, a.genres,
       a.overview, a.runtime_s, a.year, a.rating, a.premiere_on, a.status, a.manual,
       COALESCE(i.detail->>'backdrop', '')::text AS backdrop,
       COALESCE(i.detail->>'backdrops', '')::text AS backdrops,
       COALESCE(i.detail->>'country', '')::text AS country,
       COALESCE(i.detail->>'logo', '')::text AS logo,
       COALESCE(i.detail->>'network', '')::text AS network,
       COALESCE(i.detail->>'trailerKey', '')::text AS trailer_key,
       -- The stored Persian synopsis (library/022), but only a translation of the very text this
       -- row captions: the episode's when that is what we queued, else the title's.
       COALESCE(
         NULLIF(CASE WHEN e.overview = a.overview THEN e.overview_fa END, ''),
         NULLIF(CASE WHEN i.overview = a.overview THEN i.overview_fa END, ''),
         ''
       )::text AS overview_fa
FROM announcements a
LEFT JOIN items i ON i.tmdb_id = a.tmdb_id AND i.kind = a.kind
LEFT JOIN episodes e ON e.item_id = i.id
  AND e.season_no = COALESCE(a.season_no, 0) AND e.episode_no = COALESCE(a.episode_no, 0)
WHERE a.id = $1;

-- name: MarkAnnouncementPosted :execrows
-- Approve. Guarded on status = 2 so two admins pressing at once post once.
UPDATE announcements
SET status = 3, channel_message_id = $2, posted_at = now()
WHERE id = $1 AND status = 2;

-- name: RejectAnnouncement :execrows
-- Decline. Nobody is told; the title simply never reaches the channel.
UPDATE announcements SET status = 4 WHERE id = $1 AND status = 2;

-- name: DropOlderQueuedSeries :exec
-- One queued post per series inside its hold (ADR 0017).
-- Rank is watch order: highest (season, episode) stays at status 1, every other queued
-- episode of that series is skipped. The survivor keeps the announce_after it was inserted
-- with, so a later episode restarts the hold and an older one does not move it.
WITH ranked AS (
  SELECT id,
         row_number() OVER (
           ORDER BY season_no DESC NULLS LAST, episode_no DESC NULLS LAST, id DESC
         ) AS n
  FROM announcements
  WHERE announcements.tmdb_id = sqlc.arg(series_tmdb) AND announcements.kind = 2 AND announcements.status = 1
)
UPDATE announcements a
SET status = 5
FROM ranked r
WHERE a.id = r.id AND r.n > 1 AND a.status = 1;

-- name: AnnouncementQueued :one
-- True when the row we just inserted is the one the hold is waiting on.
SELECT id FROM announcements WHERE id = $1 AND status = 1;

-- name: LiveAnnouncement :one
-- The row that still blocks a second draft: queued or awaiting approval. A posted row does not.
SELECT id, status
FROM announcements
WHERE tmdb_id = $1
  AND kind = $2
  AND COALESCE(season_no, 0) = $3
  AND COALESCE(episode_no, 0) = $4
  AND status IN (1, 2);

-- name: AnnouncementWasPosted :one
-- True when this title already has a channel post, so a manual draft can warn and still proceed.
SELECT EXISTS (
  SELECT 1 FROM announcements
  WHERE tmdb_id = $1
    AND kind = $2
    AND COALESCE(season_no, 0) = $3
    AND COALESCE(episode_no, 0) = $4
    AND status = 3
) AS posted;
