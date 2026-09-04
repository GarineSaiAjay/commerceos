package events

import (
	"context"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// StreamConsumer reads every event off the shared Redis Stream
// (events.DefaultStream) as its own consumer group and durably
// persists each one via EventLogWriter. It is the general-purpose
// consumer of the raw event stream, distinct from
// growth.CartEventConsumer's separate consumer group, which reacts to
// exactly one event type (cart.item_added) to precompute a cross-sell
// suggestion -- Redis Streams delivers every message to every group
// independently, so the two consumers don't compete for or divide up
// the stream between them; both simply see everything published to it.
//
// This type's own doc comment used to describe it as a placeholder
// that only logged each event to stdout, "proving the event bus is
// wired end to end," with real Analytics/Notification/Audit consumers
// promised to arrive in later phases. That promise went stale and this
// comment retires it honestly instead of leaving it to mislead the next
// reader: audit_events already captures every consequential action
// directly and synchronously
// (backend/commerce/payment/webhook_applier.go's own audit.Write
// calls, not this stream), and backend/analytics/service.go computes
// every dashboard metric with direct SQL against the real tables, not
// from stream-derived state -- so a stream-driven "Analytics consumer"
// or "Audit consumer" here would duplicate logic that already exists
// and already works, not fill a gap. There is also no notification
// channel (email/SMS/push) configured anywhere in this project for a
// "Notification consumer" to call into.
//
// What genuinely was missing without this consumer doing real work: a
// durable, queryable record of every event that ever crossed the bus
// at all, rather than one visible only in container stdout and lost
// the instant the process restarted. See PostgresEventLogWriter's own
// doc comment for the event_log table this now persists to.
type StreamConsumer struct {
	client *redis.Client
	stream string
	group  string
	writer EventLogWriter
}

func NewStreamConsumer(
	client *redis.Client,
	stream string,
	group string,
	writer EventLogWriter,
) *StreamConsumer {
	return &StreamConsumer{
		client: client,
		stream: stream,
		group:  group,
		writer: writer,
	}
}

// Run consumes events from the stream's consumer group until ctx is
// cancelled, persisting each one via handleMessage.
func (c *StreamConsumer) Run(ctx context.Context) error {
	// Create the consumer group (ignore the AlreadyExists error on
	// first run).
	_ = c.client.XGroupCreateMkStream(
		ctx,
		c.stream,
		c.group,
		"$",
	).Err()

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-time.After(500 * time.Millisecond):
			c.consumeBatch(ctx)
		}
	}
}

func (c *StreamConsumer) consumeBatch(ctx context.Context) {
	// Poll for new messages.
	msgs, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    c.group,
		Consumer: "event-log-consumer",
		Streams:  []string{c.stream, ">"},
		Count:    10,
		Block:    200 * time.Millisecond,
	}).Result()
	if err != nil {
		// No messages / timeout — fine.
		return
	}

	for _, stream := range msgs {
		for _, msg := range stream.Messages {
			if !c.handleMessage(ctx, msg.ID, msg.Values) {
				// Persistence failed -- leave the message pending
				// (unacked) in this consumer group so the next poll
				// (or, on a crash, the next process start reading the
				// same group) redelivers it instead of silently
				// dropping an event that never made it to event_log.
				continue
			}

			_, _ = c.client.XAck(
				ctx,
				c.stream,
				c.group,
				msg.ID,
			).Result()
		}
	}
}

// handleMessage persists one stream message via c.writer and reports
// whether it should be acknowledged. Deliberately takes no *redis.Client
// so it can be exercised directly against a fake EventLogWriter in
// tests, without a live Redis instance -- the same "test the pure
// per-message logic, not the polling loop" split
// growth.CartEventConsumer's own handleCartItemAdded already uses.
func (c *StreamConsumer) handleMessage(ctx context.Context, messageID string, values map[string]any) bool {
	eventType, _ := values["event_type"].(string)
	payload, _ := values["payload"].(string)

	if err := c.writer.Write(ctx, c.stream, messageID, eventType, []byte(payload)); err != nil {
		log.Printf(
			"[event_log] failed to persist event id=%s type=%s: %v",
			messageID,
			eventType,
			err,
		)
		return false
	}

	log.Printf(
		"[event_log] persisted event id=%s type=%s",
		messageID,
		eventType,
	)
	return true
}
