
-- +goose Up

CREATE TABLE content_requests (
  id                    bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id               bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  tmdb_id               bigint      NOT NULL,
  kind                  smallint    NOT NULL,               -- 1 movie, 2 series (catalog.Kind)
  title                 text        NOT NULL DEFAULT '',     -- snapshot for later DMs, no TMDB re-fetch
  poster                text        NOT NULL DEFAULT '',     -- TMDB path, like items.poster
  status                smallint    NOT NULL DEFAULT 1,      -- 1 pending, 2 fulfilled, 3 unavailable
  created_at            timestamptz NOT NULL DEFAULT now(),
  fulfilled_at          timestamptz,
  notified_admins_at    timestamptz,
  notified_requester_at timestamptz
);

CREATE UNIQUE INDEX content_requests_open_uidx
  ON content_requests (user_id, tmdb_id, kind) WHERE status = 1;

CREATE INDEX content_requests_pending_idx ON content_requests (status, created_at) WHERE status = 1;

CREATE INDEX content_requests_quota_idx ON content_requests (user_id, created_at);

-- +goose Down

DROP TABLE IF EXISTS content_requests;
