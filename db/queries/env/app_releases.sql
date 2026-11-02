-- Latest client binaries on RELEASE_CHANNEL. One row per platform per track (stable | nightly).
-- main-bot Download reads stable only. apid offers nightly to beta testers unless stable is a higher semver.

-- UpsertAppRelease returns the message this platform+track pointed at *before* the new post, so
-- main-bot can delete it. A nightly must not retire the stable message (and the reverse). Zero
-- means this track had no build yet.
-- name: UpsertAppRelease :one
WITH prev AS (
  SELECT message_id FROM app_releases WHERE platform = $1 AND channel = $2
)
INSERT INTO app_releases (platform, channel, channel_id, message_id, version, file_name, posted_at)
VALUES ($1, $2, $3, $4, $5, $6, now())
ON CONFLICT (platform, channel) DO UPDATE
SET channel_id = EXCLUDED.channel_id,
    message_id = EXCLUDED.message_id,
    version    = EXCLUDED.version,
    file_name  = EXCLUDED.file_name,
    posted_at  = now()
RETURNING COALESCE((SELECT message_id FROM prev), 0)::bigint AS prev_message_id;

-- name: AppReleaseByPlatform :one
SELECT platform, channel, channel_id, message_id, version, file_name, posted_at
FROM app_releases
WHERE platform = $1 AND channel = $2;

-- AppReleasePlatforms is what the Download screen offers. Stable only: nightly is in-app, and
-- only for beta testers. A platform with no stable row is a build that was never posted.
-- name: AppReleasePlatforms :many
SELECT platform, version
FROM app_releases
WHERE message_id <> 0 AND channel = 'stable'
ORDER BY platform;
