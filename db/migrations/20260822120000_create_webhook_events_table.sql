-- +goose Up

CREATE TABLE webhook_events (
    id BIGSERIAL PRIMARY KEY,

    -- Razorpay's x-razorpay-event-id header. Unique for dedup.
    event_id TEXT NOT NULL UNIQUE,

    event_type TEXT NOT NULL,
    payload JSONB NOT NULL,

    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_webhook_events_event_type
    ON webhook_events(event_type);


-- +goose Down

DROP TABLE webhook_events;