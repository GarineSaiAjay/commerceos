-- +goose Up

-- authorizations lacked an updated_at column while the repository's
-- MarkAuthorizationUsed updates it. Add it to match the other tables.
ALTER TABLE authorizations
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- +goose Down

ALTER TABLE authorizations
    DROP COLUMN updated_at;