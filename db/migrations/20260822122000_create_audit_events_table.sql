-- +goose Up

CREATE TABLE audit_events (
    id BIGSERIAL PRIMARY KEY,

    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    detail JSONB NOT NULL DEFAULT '{}'::jsonb,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_events_entity
    ON audit_events(entity_type, entity_id, created_at);


-- +goose Down

DROP TABLE audit_events;