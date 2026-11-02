
-- +goose Up

ALTER TABLE users ADD COLUMN beta_tester boolean NOT NULL DEFAULT false;

ALTER TABLE app_releases ADD COLUMN channel text NOT NULL DEFAULT 'stable'
  CHECK (channel IN ('stable', 'nightly'));

UPDATE app_releases SET channel = 'nightly' WHERE version LIKE '%-nightly%';

ALTER TABLE app_releases DROP CONSTRAINT app_releases_pkey;
ALTER TABLE app_releases ADD PRIMARY KEY (platform, channel);

-- +goose Down

DELETE FROM app_releases WHERE channel = 'nightly';

ALTER TABLE app_releases DROP CONSTRAINT app_releases_pkey;
ALTER TABLE app_releases DROP CONSTRAINT IF EXISTS app_releases_channel_check;
ALTER TABLE app_releases DROP COLUMN IF EXISTS channel;
ALTER TABLE app_releases ADD PRIMARY KEY (platform);

ALTER TABLE users DROP COLUMN IF EXISTS beta_tester;
