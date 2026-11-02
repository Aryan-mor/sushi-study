-- The deploy-announce claim (docs/02 §7). main-bot is the only caller today.

-- name: ClaimDeployNotice :one
INSERT INTO deploy_notices (service, tag, notified_at)
VALUES ($1, $2, now())
ON CONFLICT (service) DO UPDATE
SET tag         = EXCLUDED.tag,
    notified_at = now()
WHERE deploy_notices.tag <> EXCLUDED.tag
RETURNING tag;
