-- +goose Up

ALTER TABLE audit_events
    ADD COLUMN event_hash TEXT,
    ADD COLUMN prev_hash TEXT;

-- +goose Down

ALTER TABLE audit_events
    DROP COLUMN event_hash,
    DROP COLUMN prev_hash;