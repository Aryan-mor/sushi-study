
-- +goose Up

CREATE TABLE personal_videos (
  id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  uploaded_by_tg bigint      NOT NULL,
  tg_unique_id   text        NOT NULL,
  size_bytes     bigint      NOT NULL DEFAULT 0,
  duration_s     int         NOT NULL DEFAULT 0,
  mime           text        NOT NULL DEFAULT '',
  updated_seq    bigint      NOT NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now(),

  UNIQUE (tg_unique_id)
);

CREATE INDEX personal_videos_uploader_idx ON personal_videos (uploaded_by_tg, created_at);

CREATE TABLE personal_video_locations (
  video_id   bigint      NOT NULL REFERENCES personal_videos (id) ON DELETE CASCADE,
  role       text        NOT NULL,
  channel_id bigint      NOT NULL,
  message_id bigint      NOT NULL,
  copied_at  timestamptz NOT NULL DEFAULT now(),

  PRIMARY KEY (video_id, role),
  UNIQUE (channel_id, message_id),
  CONSTRAINT personal_video_locations_role_check CHECK (role IN ('personal_clone', 'personal_backup'))
);

CREATE TABLE personal_video_grants (
  video_id      bigint      NOT NULL REFERENCES personal_videos (id) ON DELETE CASCADE,
  tg_user_id    bigint      NOT NULL,
  state         smallint    NOT NULL DEFAULT 1,
  title         text        NOT NULL DEFAULT '',
  granted_by_tg bigint,
  updated_seq   bigint      NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),

  PRIMARY KEY (video_id, tg_user_id),
  CONSTRAINT personal_video_grants_state_check CHECK (state IN (1, 2))
);

CREATE INDEX personal_video_grants_user_idx ON personal_video_grants (tg_user_id, updated_seq);

ALTER PUBLICATION sushi_catalog ADD TABLE personal_videos, personal_video_locations, personal_video_grants;

-- +goose Down

ALTER PUBLICATION sushi_catalog DROP TABLE personal_video_grants, personal_video_locations, personal_videos;
DROP TABLE IF EXISTS personal_video_grants, personal_video_locations, personal_videos;
