package events

import (
	"context"
	"encoding/json"
	"time"
)

// OutboxEvent is a row in the outbox_events table.
type OutboxEvent struct {
	ID        int64           `json:"id"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

// OutboxRepository persists and polls outbox events.
type OutboxRepository interface {
	// Insert writes an event into the outbox. It must be called inside
	// the same DB transaction as the state update it describes.
	Insert(
		ctx context.Context,
		eventType string,
		payload any,
	) (int64, error)

	// PollUnpublished returns up to `limit` unpublished events, oldest
	// first. It marks nothing — the worker marks published only after a
	// successful publish.
	PollUnpublished(
		ctx context.Context,
		limit int,
	) ([]OutboxEvent, error)

	// MarkPublished sets published_at for the given event IDs.
	MarkPublished(
		ctx context.Context,
		ids []int64,
	) error
}
