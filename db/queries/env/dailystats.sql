-- Superadmin daily digest and /admin → Actions → Stats. Read-only snapshots.

-- name: DailyStatsSnapshot :one
-- Online is anyone who touched main-bot, searched, played, or received a copy in the last 24h —
-- last_seen_at alone misses app-only activity. Delivered is copies that actually landed.
-- Indexed is ready catalog files (state=1), not the retired the prototype fetcherd snapshot.
SELECT
  (
    SELECT count(*)::bigint FROM (
      SELECT id FROM users WHERE last_seen_at >= now() - interval '24 hours'
      UNION
      SELECT user_id FROM play_events WHERE at >= now() - interval '24 hours'
      UNION
      SELECT user_id FROM search_events WHERE at >= now() - interval '24 hours'
      UNION
      SELECT b.user_id
      FROM deliveries d
      JOIN bindings b ON b.reader_chat_id = d.reader_chat_id
      WHERE d.delivered_at IS NOT NULL AND d.delivered_at >= now() - interval '24 hours'
    ) active
  ) AS online_24h,
  (SELECT count(*)::bigint FROM users) AS registered,
  (
    SELECT count(*)::bigint FROM deliveries
    WHERE delivered_at IS NOT NULL AND delivered_at >= now() - interval '24 hours'
  ) AS delivered_24h,
  (SELECT count(*)::bigint FROM files WHERE state = 1) AS indexed;

-- name: ClaimDailyStatsSend :one
-- Fresh when this local YMD has not been announced yet. ON CONFLICT DO NOTHING + no row means
-- somebody (this process, or a previous boot) already sent today.
INSERT INTO daily_stats_sends (ymd)
VALUES (sqlc.arg(day)::date)
ON CONFLICT (ymd) DO NOTHING
RETURNING ymd::text AS claimed;
