package events

import (
	"context"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// StreamConsumer reads events from a Redis Stream and dispatches them to
// registered placeholder handlers (Analytics/Audit/Notification arrive
// in later phases). This proves the event bus is wired end to end.
type StreamConsumer struct {
	client *redis.Client
	stream string
	group  string
}

func NewStreamConsumer(
	client *redis.Client,
	stream string,
	group string,
) *StreamConsumer {
	return &StreamConsumer{
		client: client,
		stream: stream,
		group:  group,
	}
}

// Run consumes events from the stream's consumer group until ctx is
// cancelled. It logs each event as a placeholder consumer.
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
		Consumer: "placeholder-consumer",
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
			eventType := msg.Values["event_type"]
			payload := msg.Values["payload"]

			// Placeholder consumer: log the event. Analytics,
			// Notification, and Audit consumers arrive in later phases.
			log.Printf(
				"[consumer:placeholder] event=%v payload=%v",
				eventType,
				payload,
			)

			// Acknowledge the message.
			_, _ = c.client.XAck(
				ctx,
				c.stream,
				c.group,
				msg.ID,
			).Result()
		}
	}
}
