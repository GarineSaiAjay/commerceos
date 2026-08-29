-- +goose Up

-- Operator accounts: the merchant's own back-office users, distinct from
-- buyers (who check out as guests). Gates the merchant dashboard and the
-- merchant-review path for Level 2/3 approval requests -- see
-- files/AUTH.md.
CREATE TABLE operators (
    id TEXT PRIMARY KEY,
    merchant_id TEXT NOT NULL REFERENCES merchants(id),
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Bearer session tokens. Only the SHA-256 hash of the token is stored --
-- the raw token exists only in the login response and the caller's
-- Authorization header, never at rest.
CREATE TABLE operator_sessions (
    token_hash TEXT PRIMARY KEY,
    operator_id TEXT NOT NULL REFERENCES operators(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_operator_sessions_operator_id ON operator_sessions(operator_id);

-- +goose Down

DROP TABLE operator_sessions;
DROP TABLE operators;
