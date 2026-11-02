
-- +goose Up

CREATE TABLE login_nonces (
    user_id bigint PRIMARY KEY REFERENCES users (id),
    nonce bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE login_channel_outbox (
    id bigserial PRIMARY KEY,
    blob text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    posted_at timestamptz,
    channel_message_id bigint,
    delete_after timestamptz NOT NULL
);

CREATE INDEX login_channel_outbox_pending_idx
    ON login_channel_outbox (id) WHERE posted_at IS NULL;

CREATE INDEX login_channel_outbox_delete_idx
    ON login_channel_outbox (delete_after)
    WHERE channel_message_id IS NOT NULL;

-- +goose Down

DROP TABLE login_channel_outbox;
DROP TABLE login_nonces;
