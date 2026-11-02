
-- +goose Up

DROP INDEX IF EXISTS episodes_item_idx;

DROP INDEX IF EXISTS file_locations_role_idx;
CREATE INDEX file_locations_clone_idx ON file_locations (file_id) WHERE role = 'clone';

-- +goose Down

DROP INDEX IF EXISTS file_locations_clone_idx;
CREATE INDEX file_locations_role_idx ON file_locations (role);
CREATE INDEX episodes_item_idx ON episodes (item_id, season_no, episode_no);
