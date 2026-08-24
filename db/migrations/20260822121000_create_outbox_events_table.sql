-- +goose Up

CREATE TABLE outbox_events (
    id BIGSERIAL PRIMARY KEY,

    event_type TEXT NOT NULL,
    payload JSONB NOT NULL,

    -- NULL until the outbox worker publishes it to the event bus.
    published_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_outbox_events_unpublished
    ON outbox_events(published_at)
    WHERE published_at IS NULL;


-- +goose Down

DROP TABLE outbox_events;