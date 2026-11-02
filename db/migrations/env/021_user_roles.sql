
-- +goose Up

ALTER TABLE users ADD COLUMN role text NOT NULL DEFAULT 'user'
  CHECK (role IN ('user', 'admin', 'superadmin'));

CREATE INDEX users_staff_idx ON users (role) WHERE role <> 'user';

-- +goose Down

DROP INDEX IF EXISTS users_staff_idx;
ALTER TABLE users DROP COLUMN IF EXISTS role;
