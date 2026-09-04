-- +goose Up

-- backend/events/stream_consumer.go previously only logged every event
-- off the shared Redis Stream (events.DefaultStream) to stdout and
-- discarded it -- its own doc comment described this as a placeholder
-- "proving the event bus is wired end to end" with real Analytics/
-- Notification/Audit consumers promised to arrive "in later phases."
-- That promise never happened and, on inspection, doesn't need to:
-- audit_events already captures every consequential action directly
-- and synchronously (backend/commerce/payment/webhook_applier.go's own
-- audit.Write calls), and backend/analytics/service.go computes every
-- dashboard metric with direct SQL against the real tables, not from
-- stream-derived state -- so a stream-driven "Analytics consumer" or
-- "Audit consumer" here would duplicate logic that already exists and
-- already works. There is also no notification channel (email/SMS/
-- push) anywhere in this project for a "Notification consumer" to call
-- into.
--
-- What was genuinely still missing: a durable, queryable record of
-- every event that ever crossed the bus at all (cart.item_added,
-- payment.captured, payment.failed today; whatever is added next
-- tomorrow) -- previously visible only in container stdout and lost
-- the moment the process restarted. event_log is that record.
--
-- (stream, stream_message_id) is UNIQUE rather than stream_message_id
-- alone so this can never collide across two different streams sharing
-- the same Redis-generated ID format, and so the consumer's INSERT ...
-- ON CONFLICT DO NOTHING can be keyed on it directly for idempotent
-- persistence under Redis Streams' at-least-once delivery (a crash
-- between XReadGroup and XAck redelivers the same message; without
-- this constraint that would silently double-count activity here).
CREATE TABLE event_log (
    id BIGSERIAL PRIMARY KEY,
    stream TEXT NOT NULL,
    stream_message_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (stream, stream_message_id)
);

CREATE INDEX idx_event_log_event_type ON event_log (event_type);
CREATE INDEX idx_event_log_received_at ON event_log (received_at DESC);

-- +goose Down

DROP TABLE event_log;
