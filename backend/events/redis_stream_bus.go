package events

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// EventBus publishes domain events to Redis Streams.
type EventBus interface {
	Publish(ctx context.Context, stream string, event OutboxEvent) error
}

// RedisStreamBus publishes each event as a Redis Stream entry.
type RedisStreamBus struct {
	client *redis.Client
}

func NewRedisStreamBus(client *redis.Client) *RedisStreamBus {
	return &RedisStreamBus{client: client}
}

func (b *RedisStreamBus) Publish(
	ctx context.Context,
	stream string,
	event OutboxEvent,
) error {
	values := map[string]any{
		"event_type": event.EventType,
		"payload":    string(event.Payload),
		"created_at": event.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	if err := b.client.XAdd(ctx, &redis.XAddArgs{
		Stream: stream,
		Values: values,
	}).Err(); err != nil {
		return fmt.Errorf("publish to redis stream %s: %w", stream, err)
	}

	return nil
}
