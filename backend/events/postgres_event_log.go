package events

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// EventLogWriter persists one raw event read off a Redis Stream,
// exactly once per (stream, messageID) pair. See PostgresEventLogWriter's
// own doc comment for what event_log is for and why it exists alongside
// the outbox (backend/events/postgres_outbox_repository.go, publish-side
// durability before an event ever reaches Redis) and audit_events
// (backend/audit, a curated, hash-chained trail of consequential actions
// only) rather than duplicating either.
type EventLogWriter interface {
	Write(ctx context.Context, stream, messageID, eventType string, payload []byte) error
}

// PostgresEventLogWriter is StreamConsumer's real, durable sink: every
// event that crosses events.DefaultStream (cart.item_added,
// payment.captured, payment.failed today -- whatever is published next
// tomorrow) lands in the event_log table, queryable long after the
// process that logged it to stdout has restarted and lost that line.
//
// Write is idempotent: Redis Streams' consumer groups deliver
// at-least-once, so a crash between XReadGroup and XAck in
// StreamConsumer.consumeBatch can redeliver the same message on
// restart. INSERT ... ON CONFLICT DO NOTHING, keyed on the
// (stream, stream_message_id) UNIQUE constraint
// (db/migrations/20260904200000_create_event_log.sql), turns that
// at-least-once delivery into exactly-once storage -- a duplicate
// delivery becomes a silent no-op here, not a double-counted row.
type PostgresEventLogWriter struct {
	db *pgxpool.Pool
}

func NewPostgresEventLogWriter(db *pgxpool.Pool) *PostgresEventLogWriter {
	return &PostgresEventLogWriter{db: db}
}

func (w *PostgresEventLogWriter) Write(
	ctx context.Context,
	stream string,
	messageID string,
	eventType string,
	payload []byte,
) error {
	_, err := w.db.Exec(ctx, `
		INSERT INTO event_log (stream, stream_message_id, event_type, payload)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (stream, stream_message_id) DO NOTHING
	`, stream, messageID, eventType, payload)
	if err != nil {
		return fmt.Errorf("insert event_log row (stream=%s id=%s type=%s): %w", stream, messageID, eventType, err)
	}

	return nil
}
