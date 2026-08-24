package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrWebhookEventDuplicate is returned when an event with the same
// x-razorpay-event-id has already been stored. The caller must treat a
// duplicate as a no-op — no state transition may be invoked.
var ErrWebhookEventDuplicate = errors.New("duplicate webhook event")

// WebhookEventStore persists inbound webhook events (post-verification,
// post-dedup) as an immutable record.
type WebhookEventStore interface {
	// Store inserts the event. Returns ErrWebhookEventDuplicate if the
	// event_id already exists.
	Store(
		ctx context.Context,
		eventID string,
		eventType string,
		payload json.RawMessage,
	) error
}

type PostgresWebhookEventStore struct {
	db *pgxpool.Pool
}

func NewPostgresWebhookEventStore(db *pgxpool.Pool) *PostgresWebhookEventStore {
	return &PostgresWebhookEventStore{db: db}
}

func (s *PostgresWebhookEventStore) Store(
	ctx context.Context,
	eventID string,
	eventType string,
	payload json.RawMessage,
) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO webhook_events (event_id, event_type, payload)
		VALUES ($1, $2, $3)
	`, eventID, eventType, payload)

	if err != nil {
		// Unique violation on event_id => duplicate delivery.
		if isUniqueViolation(err) {
			return ErrWebhookEventDuplicate
		}

		return fmt.Errorf("store webhook event: %w", err)
	}

	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}

	return false
}

var _ = pgx.ErrNoRows
