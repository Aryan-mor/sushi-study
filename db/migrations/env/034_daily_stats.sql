
-- +goose Up

CREATE TABLE daily_stats_sends (
  ymd     date        PRIMARY KEY,
  sent_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down

DROP TABLE IF EXISTS daily_stats_sends;
