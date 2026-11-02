-- Content requests (ADR 0014 §D2, §D4). The env-local table, plus the catalog read that decides
-- whether a request is even needed. items / episodes / files are replicated and read-only here
-- (R-LIB-3).

-- name: CatalogServeable :one
-- True when this (tmdb_id, kind) already has at least one ready file somewhere in the title. The
-- Request op short-circuits to ALREADY_AVAILABLE on this; the reconciler flips a pending row to
-- fulfilled on it.
SELECT EXISTS (
  SELECT 1
  FROM items i
  JOIN episodes e ON e.item_id = i.id
  JOIN files f ON f.episode_id = e.id
  WHERE i.tmdb_id = $1 AND i.kind = $2 AND f.state = 1
) AS serveable;

-- name: RequestTitleSnapshot :one
-- A denormalised title + poster stored on the request row so later DMs need no TMDB call.
-- title_cards already encodes "catalog row outranks the TMDB cache" (env/018); the client opened
-- /item just before requesting, so browse_titles is nearly always populated. No row -> empty, and
-- the DM formatting falls back to the tmdb id.
SELECT title, poster FROM title_cards WHERE tmdb_id = $1 AND kind = $2;

-- name: CountRequestsSince :one
-- The rolling-window quota check. Every row counts, whatever its status -- a fulfilled request
-- still used a slot that day.
SELECT count(*) FROM content_requests
WHERE user_id = $1 AND created_at >= $2;

-- name: InsertContentRequest :one
-- ON CONFLICT targets the partial unique index (status = 1): a re-submit of an already-open
-- request returns no row, which the caller reads as DUPLICATE and does not charge to the quota.
INSERT INTO content_requests (user_id, tmdb_id, kind, title, poster, imdb_id)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (user_id, tmdb_id, kind) WHERE status = 1 DO NOTHING
RETURNING id;

-- name: PendingContentRequestsForAdmins :many
-- Rows the admin alert has not gone out for yet (ADR 0014 §D4). Joined to the requester's
-- Telegram id so the DM can name who asked; poster so it can carry the artwork.
--
-- title_cards is joined on top of the request's own snapshot because the snapshot is taken at
-- request time, when browse_titles may not have been hydrated yet -- an empty snapshot poster
-- would mean a card with no artwork forever. The snapshot still wins when it has a value: it is
-- what the requester saw when they asked.
SELECT r.id, r.tmdb_id, r.kind, r.imdb_id,
       COALESCE(NULLIF(r.title, ''), tc.title, '')::text   AS title,
       COALESCE(NULLIF(r.poster, ''), tc.poster, '')::text AS poster,
       COALESCE(tc.year, 0)::int                           AS year,
       COALESCE(tc.rating, 0)::int                         AS rating,
       u.tg_user_id AS requester_tg_id, u.tg_name AS requester_name, u.tg_username AS requester_username,
       COALESCE(p.subtitle_lang, '')::text AS subtitle_lang
FROM content_requests r
JOIN users u ON u.id = r.user_id
LEFT JOIN user_prefs p ON p.user_id = r.user_id
LEFT JOIN title_cards tc ON tc.tmdb_id = r.tmdb_id AND tc.kind = r.kind
WHERE r.status = 1 AND r.notified_admins_at IS NULL
ORDER BY r.created_at
LIMIT $1;

-- name: MarkContentRequestAdminsNotified :exec
UPDATE content_requests SET notified_admins_at = now() WHERE id = $1;

-- name: MarkContentRequestChannelPosted :exec
-- Staff-inbox post (ADR 0023): one channel message, then stop. channel_message_id lets a later
-- tick edit the post when the request is fulfilled or rejected.
UPDATE content_requests
SET notified_admins_at = now(), channel_message_id = $2
WHERE id = $1;

-- name: SettledRequestChannelPosts :many
-- Channel posts whose request is no longer pending, still waiting to have the keyboard stripped.
SELECT id, channel_message_id, status, title, tmdb_id, kind
FROM content_requests
WHERE channel_message_id IS NOT NULL AND status IN (2, 3)
ORDER BY id
LIMIT $1;

-- name: ClearRequestChannelMessage :exec
UPDATE content_requests SET channel_message_id = NULL WHERE id = $1;

-- name: AdminPendingRequestsPage :many
-- One page for the main-bot admin request panel (ADR 0014 §D5), oldest first so the backlog is
-- worked in order. A fulfilled/rejected request drops out (status = 1 only).
SELECT r.id, r.tmdb_id, r.imdb_id, r.kind, r.title,
       u.tg_user_id AS requester_tg_id, u.tg_name AS requester_name, u.tg_username AS requester_username
FROM content_requests r
JOIN users u ON u.id = r.user_id
WHERE r.status = 1
ORDER BY r.created_at
LIMIT $1 OFFSET $2;

-- name: AdminPendingRequestsCount :one
SELECT count(*) FROM content_requests WHERE status = 1;

-- name: RequestTarget :one
SELECT tmdb_id, kind FROM content_requests WHERE id = $1;

-- name: RejectContentRequest :execrows
-- Admin decline: 3 = unavailable. Silent — the requester is not told (ADR 0014 §D5).
UPDATE content_requests SET status = 3 WHERE id = $1 AND status = 1;

-- name: ReadyFileLabels :many
-- Ready files for one title, so a non-Persian subtitle preference can ignore hardsub and dub.
SELECT f.quality_label, COALESCE(f.sub_langs, '')::text AS sub_langs
FROM items i
JOIN episodes e ON e.item_id = i.id
JOIN files f ON f.episode_id = e.id
WHERE i.tmdb_id = $1 AND i.kind = $2 AND f.state = 1;

-- name: PendingRequestsForFulfilment :many
-- One row per pending request, with the requester's subtitle language. Fulfilment is per user:
-- a hardsub-only title is ready for Persian and still open for anyone else.
SELECT r.id, r.tmdb_id, r.kind, COALESCE(p.subtitle_lang, '')::text AS subtitle_lang
FROM content_requests r
LEFT JOIN user_prefs p ON p.user_id = r.user_id
WHERE r.status = 1
ORDER BY r.created_at
LIMIT $1;

-- name: MarkRequestFulfilled :execrows
UPDATE content_requests
SET status = 2, fulfilled_at = now()
WHERE id = $1 AND status = 1;

-- name: PendingContentRequests :many
-- The fulfilment scan reads these and checks each (tmdb_id, kind) against CatalogServeable
-- (ADR 0014 §D4). Distinct so N users requesting the same title cost one serveable check.
SELECT DISTINCT tmdb_id, kind
FROM content_requests
WHERE status = 1
LIMIT $1;

-- name: MarkTargetFulfilled :execrows
-- One serveable (tmdb_id, kind) fulfils every pending request for it at once — 20 people who asked
-- for the same series each get one "it's ready" DM from the requester-notify pass, not 20.
UPDATE content_requests
SET status = 2, fulfilled_at = now()
WHERE status = 1 AND tmdb_id = $1 AND kind = $2;

-- name: FulfilledContentRequestsToNotify :many
-- Fulfilled rows whose requester has not been told yet, joined to the Telegram id the DM goes to.
--
-- locale comes along so the DM is written in the requester's language rather than in both at once
-- (R-I18N-10). year and rating come from title_cards, which by definition has a catalog row by the
-- time a request is fulfilled -- that is what fulfilled means.
SELECT r.id, r.user_id, u.tg_user_id, r.tmdb_id, r.kind,
       COALESCE(NULLIF(r.title, ''), tc.title, '')::text   AS title,
       COALESCE(NULLIF(r.poster, ''), tc.poster, '')::text AS poster,
       COALESCE(p.locale, '')::text                        AS locale,
       COALESCE(tc.year, 0)::int                           AS year,
       COALESCE(tc.rating, 0)::int                         AS rating
FROM content_requests r
JOIN users u ON u.id = r.user_id
LEFT JOIN user_prefs p ON p.user_id = r.user_id
LEFT JOIN title_cards tc ON tc.tmdb_id = r.tmdb_id AND tc.kind = r.kind
WHERE r.status = 2 AND r.notified_requester_at IS NULL
ORDER BY r.fulfilled_at
LIMIT $1;

-- name: MarkContentRequestRequesterNotified :exec
UPDATE content_requests SET notified_requester_at = now() WHERE id = $1;

-- name: AdminAlertRecipients :many
-- Recipients of the new-request alert (ADR 0014 §D4), each with the locale their copy is written
-- in. An admin who never finished onboarding has no prefs row, so locale falls back to '' and the
-- formatter picks English.
SELECT u.tg_user_id, COALESCE(p.locale, '')::text AS locale
FROM users u
LEFT JOIN user_prefs p ON p.user_id = u.id
WHERE u.role IN ('admin', 'superadmin')
ORDER BY u.tg_user_id;
