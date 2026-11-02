
-- +goose Up

CREATE TABLE personal_videos (
  id             bigint      NOT NULL PRIMARY KEY,
  uploaded_by_tg bigint      NOT NULL,
  tg_unique_id   text        NOT NULL,
  size_bytes     bigint      NOT NULL DEFAULT 0,
  duration_s     int         NOT NULL DEFAULT 0,
  mime           text        NOT NULL DEFAULT '',
  updated_seq    bigint      NOT NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE personal_video_locations (
  video_id   bigint      NOT NULL,
  role       text        NOT NULL,
  channel_id bigint      NOT NULL,
  message_id bigint      NOT NULL,
  copied_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (video_id, role)
);

CREATE TABLE personal_video_grants (
  video_id      bigint      NOT NULL,
  tg_user_id    bigint      NOT NULL,
  state         smallint    NOT NULL DEFAULT 1,
  title         text        NOT NULL DEFAULT '',
  granted_by_tg bigint,
  updated_seq   bigint      NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (video_id, tg_user_id)
);

CREATE INDEX personal_video_grants_user_idx ON personal_video_grants (tg_user_id, updated_seq);

CREATE TRIGGER personal_videos_readonly BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON personal_videos
  FOR EACH STATEMENT EXECUTE FUNCTION sushi_catalog_readonly();
CREATE TRIGGER personal_video_locations_readonly BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON personal_video_locations
  FOR EACH STATEMENT EXECUTE FUNCTION sushi_catalog_readonly();
CREATE TRIGGER personal_video_grants_readonly BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON personal_video_grants
  FOR EACH STATEMENT EXECUTE FUNCTION sushi_catalog_readonly();

-- +goose Down

DROP TRIGGER IF EXISTS personal_video_grants_readonly ON personal_video_grants;
DROP TRIGGER IF EXISTS personal_video_locations_readonly ON personal_video_locations;
DROP TRIGGER IF EXISTS personal_videos_readonly ON personal_videos;
DROP TABLE IF EXISTS personal_video_grants, personal_video_locations, personal_videos;
