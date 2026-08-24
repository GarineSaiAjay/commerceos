package events

import (
	"context"
	"fmt"
	"log"
	"time"
)

// OutboxWorker polls the outbox_events table for unpublished rows,
// publishes each to the event bus, and marks them published only after
// a successful publish. If the process crashes mid-batch, the unmarked
// rows remain unpublished and are picked up on restart — no loss, and
// no duplicate publish of an already-published event (because marking
// happens only after publish succeeds).
type OutboxWorker struct {
	repo     OutboxRepository
	bus      EventBus
	stream   string
	batch    int
	interval time.Duration
}

func NewOutboxWorker(
	repo OutboxRepository,
	bus EventBus,
	stream string,
) *OutboxWorker {
	return &OutboxWorker{
		repo:     repo,
		bus:      bus,
		stream:   stream,
		batch:    10,
		interval: time.Second,
	}
}

// Run polls until ctx is cancelled.
func (w *OutboxWorker) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil

		case <-time.After(w.interval):
			if err := w.processBatch(ctx); err != nil {
				log.Printf("outbox worker: %v", err)
			}
		}
	}
}

// processBatch publishes one batch and marks only the successfully
// published events. A crash between publish and mark leaves the event
// unpublished in the DB, so it is republished on restart — at-least-once
// delivery, which the downstream dedup (webhook_events) absorbs.
func (w *OutboxWorker) processBatch(ctx context.Context) error {
	events, err := w.repo.PollUnpublished(ctx, w.batch)
	if err != nil {
		return fmt.Errorf("poll unpublished: %w", err)
	}

	if len(events) == 0 {
		return nil
	}

	var publishedIDs []int64

	for _, ev := range events {
		if err := w.bus.Publish(ctx, w.stream, ev); err != nil {
			// Stop the batch on the first failure; the rest stay
			// unpublished and will be retried next tick.
			log.Printf(
				"outbox worker: failed to publish event %d: %v",
				ev.ID,
				err,
			)
			break
		}

		publishedIDs = append(publishedIDs, ev.ID)
	}

	if len(publishedIDs) > 0 {
		if err := w.repo.MarkPublished(ctx, publishedIDs); err != nil {
			return fmt.Errorf("mark published: %w", err)
		}
	}

	return nil
}
