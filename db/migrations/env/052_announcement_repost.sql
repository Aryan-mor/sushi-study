
-- +goose Up

DROP INDEX announcements_live_uidx;

CREATE UNIQUE INDEX announcements_live_uidx
  ON announcements (tmdb_id, kind, COALESCE(season_no, 0), COALESCE(episode_no, 0))
  WHERE status IN (1, 2);

-- +goose Down

DROP INDEX announcements_live_uidx;

CREATE UNIQUE INDEX announcements_live_uidx
  ON announcements (tmdb_id, kind, COALESCE(season_no, 0), COALESCE(episode_no, 0))
  WHERE status IN (1, 2, 3);
