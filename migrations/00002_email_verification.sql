-- +goose Up
-- +goose StatementBegin

CREATE TABLE email_tokens (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL CHECK (kind IN ('verify', 'reset')),
    token_hash BYTEA UNIQUE NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX email_tokens_user_kind_idx ON email_tokens (user_id, kind);

ALTER TABLE users ADD COLUMN email_verified_at TIMESTAMPTZ;

-- Mark all existing users as already verified (no disruption).
UPDATE users SET email_verified_at = now();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN IF EXISTS email_verified_at;
DROP TABLE IF EXISTS email_tokens;
-- +goose StatementEnd
