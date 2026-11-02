
-- +goose Up

ALTER TABLE subtitles
  ADD COLUMN duration_min int  NOT NULL DEFAULT 0,
  ADD COLUMN sub_type     text NOT NULL DEFAULT '';

ALTER TABLE subtitles
  DROP CONSTRAINT subtitles_tmdb_id_kind_season_no_episode_no_lang_key;

ALTER TABLE subtitles
  ADD CONSTRAINT subtitles_variant_key
  UNIQUE (tmdb_id, kind, season_no, episode_no, lang, sub_type, duration_min);

-- +goose Down

ALTER TABLE subtitles DROP CONSTRAINT subtitles_variant_key;

ALTER TABLE subtitles
  ADD CONSTRAINT subtitles_tmdb_id_kind_season_no_episode_no_lang_key
  UNIQUE (tmdb_id, kind, season_no, episode_no, lang);

ALTER TABLE subtitles
  DROP COLUMN duration_min,
  DROP COLUMN sub_type;
