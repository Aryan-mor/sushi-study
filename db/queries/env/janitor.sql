-- Retention. Every table below only ever grew, and each of them is read over a window that is far
-- shorter than for ever (docs/06 §7).
--
-- All of these are bounded by a LIMIT rather than deleting the whole backlog in one statement: a
-- sweep that locks a table for a minute is its own outage, and the janitor simply runs again.

-- name: PruneSearchEvents :execrows
-- Read only by SearchCountSince, over one hour (R-SEC-11). A day of slack is generous.
DELETE FROM search_events WHERE ctid IN (
  SELECT e.ctid FROM search_events e WHERE e.at < sqlc.arg(cutoff) LIMIT sqlc.arg(batch_size)
);

-- name: PrunePlayEvents :execrows
-- Read only by FillRailMostWatched, over rails.Policy.MostWatchedWindowDays. The cutoff the janitor
-- passes is that window plus a wide margin, so a policy change does not silently lose its history.
DELETE FROM play_events WHERE ctid IN (
  SELECT e.ctid FROM play_events e WHERE e.at < sqlc.arg(cutoff) LIMIT sqlc.arg(batch_size)
);

-- name: PruneBrowseTitles :execrows
-- A TMDB cache with no expiry is not a cache, it is a copy: a re-rated or renamed title stayed
-- wrong for ever. Dropping a stale row costs one TMDB call the next time somebody opens it.
DELETE FROM browse_titles WHERE ctid IN (
  SELECT t.ctid FROM browse_titles t WHERE t.fetched_at < sqlc.arg(cutoff) LIMIT sqlc.arg(batch_size)
);

-- name: PruneBrowsePeople :execrows
DELETE FROM browse_people WHERE ctid IN (
  SELECT p.ctid FROM browse_people p WHERE p.fetched_at < sqlc.arg(cutoff) LIMIT sqlc.arg(batch_size)
);

-- name: PruneOrphanDeliveries :execrows
-- Deliveries are keyed by reader chat and have no foreign key to users -- they cannot have one,
-- because a chat is not a user (docs/06 §3). So nothing ever removed the rows belonging to a reader
-- that has been rebound or abandoned, and they are exactly the rows that can never be useful again:
-- the message ids they hold are in a chat nothing is bound to.
--
-- Age alone is deliberately NOT enough. A delivered row is the record that this file already sits
-- in this chat at that message id; deleting a live one costs somebody a re-download.
DELETE FROM deliveries WHERE ctid IN (
  SELECT d.ctid FROM deliveries d
  WHERE d.enqueued_at < sqlc.arg(cutoff)
    AND NOT EXISTS (
      SELECT 1 FROM bindings b
      WHERE b.reader_chat_id = d.reader_chat_id AND b.superseded_at IS NULL
    )
  LIMIT sqlc.arg(batch_size)
);

-- name: PruneSettledContentRequests :execrows
-- A fulfilled or unavailable request is history once its notifications have gone out (ADR 0014).
-- Pending rows are never pruned -- somebody is still waiting on them.
DELETE FROM content_requests WHERE ctid IN (
  SELECT r.ctid FROM content_requests r
  WHERE r.status IN (2, 3) AND r.created_at < sqlc.arg(cutoff)
  LIMIT sqlc.arg(batch_size)
);

-- name: PruneOrphanFollowedSeriesState :execrows
-- The shared debounce row for a series nobody follows any more (ADR 0014 §D3). SetFollow(off) drops
-- it inline; this is the backstop. The cutoff guards against racing a follow still being created
-- (EnsureFollowedSeriesState then UpsertSeriesFollow).
DELETE FROM followed_series_state WHERE ctid IN (
  SELECT s.ctid FROM followed_series_state s
  WHERE s.changed_at < sqlc.arg(cutoff)
    AND NOT EXISTS (SELECT 1 FROM series_follows f WHERE f.tmdb_id = s.tmdb_id)
  LIMIT sqlc.arg(batch_size)
);
