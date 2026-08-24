package events

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeOutboxRepo is an in-memory outbox repository.
type fakeOutboxRepo struct {
	mu        sync.Mutex
	events    []OutboxEvent
	nextID    int64
	published map[int64]bool
}

func (r *fakeOutboxRepo) Insert(
	ctx context.Context,
	eventType string,
	payload any,
) (int64, error) {
	raw, _ := json.Marshal(payload)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	r.events = append(r.events, OutboxEvent{
		ID:        r.nextID,
		EventType: eventType,
		Payload:   raw,
		CreatedAt: time.Now(),
	})
	return r.nextID, nil
}

func (r *fakeOutboxRepo) PollUnpublished(
	ctx context.Context,
	limit int,
) ([]OutboxEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []OutboxEvent

	for _, ev := range r.events {
		if r.published[ev.ID] {
			continue
		}
		out = append(out, ev)
		if len(out) >= limit {
			break
		}
	}

	return out, nil
}

func (r *fakeOutboxRepo) MarkPublished(
	ctx context.Context,
	ids []int64,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.published == nil {
		r.published = map[int64]bool{}
	}

	for _, id := range ids {
		r.published[id] = true
	}

	return nil
}

// fakeBus publishes and can be configured to fail on the Nth call,
// simulating a crash mid-batch.
type fakeBus struct {
	mu         sync.Mutex
	published  []int64
	failOnCall int
	callCount  int
}

func (b *fakeBus) Publish(
	ctx context.Context,
	stream string,
	event OutboxEvent,
) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.callCount++

	if b.failOnCall > 0 && b.callCount == b.failOnCall {
		return errors.New("simulated crash mid-publish")
	}

	b.published = append(b.published, event.ID)

	return nil
}

// TestOutboxWorkerCrashMidBatch proves spec §6.3: killing the worker
// between DB commit and event publish, then restarting, publishes the
// pending event with no loss and no duplicate publish.
func TestOutboxWorkerCrashMidBatch(t *testing.T) {
	repo := &fakeOutboxRepo{}
	bus := &fakeBus{}

	// Insert two events.
	_, _ = repo.Insert(context.Background(), "payment.captured", map[string]any{"order_id": "o1"})
	_, _ = repo.Insert(context.Background(), "payment.failed", map[string]any{"order_id": "o2"})

	// First run: fail on the 2nd publish (simulating a crash after the
	// 1st event was published but before the 2nd).
	bus.failOnCall = 2
	worker := NewOutboxWorker(repo, bus, "test.stream")
	_ = worker.processBatch(context.Background())

	// After the "crash": event 1 published, event 2 not published.
	if len(bus.published) != 1 {
		t.Fatalf("expected 1 published after crash, got %d", len(bus.published))
	}

	// Restart: clear the failure, run again.
	bus.failOnCall = 0
	_ = worker.processBatch(context.Background())

	// Event 2 must now be published (no loss).
	if len(bus.published) != 2 {
		t.Fatalf("expected 2 published after restart, got %d", len(bus.published))
	}

	// Event 1 must NOT be republished (no duplicate publish).
	count := 0
	for _, id := range bus.published {
		if id == 1 {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("event 1 published %d times, expected exactly 1", count)
	}

	// A third run publishes nothing new.
	_ = worker.processBatch(context.Background())
	if len(bus.published) != 2 {
		t.Fatalf("expected still 2 published after third run, got %d", len(bus.published))
	}
}
