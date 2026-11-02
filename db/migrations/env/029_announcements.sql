
-- +goose Up

CREATE TABLE announcements (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tmdb_id            bigint      NOT NULL,
  kind               smallint    NOT NULL,               -- 1 movie, 2 series (catalog.Kind)
  season_no          int,                                -- set for an episode debut, null for a movie
  episode_no         int,
  title              text        NOT NULL DEFAULT '',    -- the movie or series title
  subtitle           text        NOT NULL DEFAULT '',    -- episode name, when there is one
  poster             text        NOT NULL DEFAULT '',    -- TMDB path, like items.poster
  genres             text        NOT NULL DEFAULT '',    -- display string, as items.detail->>'genres'
  overview           text        NOT NULL DEFAULT '',    -- shrunk to fit at render time, stored whole
  runtime_s          int         NOT NULL DEFAULT 0,
  year               int         NOT NULL DEFAULT 0,
  rating             int         NOT NULL DEFAULT 0,     -- TMDB rating 0-100, like items.rating
  premiere_on        date,
  first_available_at timestamptz NOT NULL DEFAULT now(),  -- when the first ready file appeared
  announce_after     timestamptz NOT NULL DEFAULT now(),  -- earliest the staff preview may go out
  status             smallint    NOT NULL DEFAULT 1,      -- 1 queued, 2 awaiting approval, 3 posted, 4 rejected, 5 skipped
  admin_chat_id      bigint,
  admin_message_id   bigint,
  channel_message_id bigint,
  created_at         timestamptz NOT NULL DEFAULT now(),
  posted_at          timestamptz
);

CREATE UNIQUE INDEX announcements_live_uidx
  ON announcements (tmdb_id, kind, COALESCE(season_no, 0), COALESCE(episode_no, 0))
  WHERE status IN (1, 2, 3);

CREATE INDEX announcements_due_idx ON announcements (announce_after) WHERE status = 1;

CREATE INDEX files_created_ready_idx ON files (created_at) WHERE state = 1;

CREATE INDEX announcements_approval_idx ON announcements (created_at DESC) WHERE status = 2;

CREATE INDEX announcements_series_cursor_idx
  ON announcements (tmdb_id, season_no DESC, episode_no DESC)
  WHERE kind = 2 AND status IN (2, 3);

-- +goose Down

DROP INDEX IF EXISTS files_created_ready_idx;
DROP TABLE IF EXISTS announcements;
