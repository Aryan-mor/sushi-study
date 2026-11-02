
-- +goose Up

ALTER TABLE reader_bot_tokens DROP COLUMN token;
ALTER TABLE reader_bot_tokens ADD COLUMN token_enc bytea NOT NULL DEFAULT ''::bytea;
ALTER TABLE reader_bot_tokens ALTER COLUMN token_enc DROP DEFAULT;


-- +goose Down

ALTER TABLE reader_bot_tokens DROP COLUMN token_enc;
ALTER TABLE reader_bot_tokens ADD COLUMN token text NOT NULL DEFAULT '';
ALTER TABLE reader_bot_tokens ALTER COLUMN token DROP DEFAULT;
