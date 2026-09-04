package events

import (
	"context"
	"errors"
	"testing"
)

// fakeEventLogWriter records every successful Write call and can be
// told to fail on one specific message ID, so tests can assert both
// the happy path (persist + ack) and the retry path (no ack on a
// failed write) without a live Postgres or Redis instance.
type fakeEventLogWriter struct {
	writes  []fakeEventLogWrite
	failOn  string
	failErr error
}

type fakeEventLogWrite struct {
	stream, messageID, eventType string
	payload                      []byte
}

func (f *fakeEventLogWriter) Write(_ context.Context, stream, messageID, eventType string, payload []byte) error {
	if f.failOn != "" && messageID == f.failOn {
		if f.failErr != nil {
			return f.failErr
		}
		return errors.New("simulated write failure")
	}
	f.writes = append(f.writes, fakeEventLogWrite{
		stream:    stream,
		messageID: messageID,
		eventType: eventType,
		payload:   payload,
	})
	return nil
}

func TestHandleMessagePersistsAndAcksOnSuccess(t *testing.T) {
	writer := &fakeEventLogWriter{}
	c := NewStreamConsumer(nil, "test-stream", "test-group", writer)

	ok := c.handleMessage(context.Background(), "1-0", map[string]any{
		"event_type": "payment.captured",
		"payload":    `{"order_id":"o1"}`,
	})

	if !ok {
		t.Fatal("expected handleMessage to report success so the caller acks")
	}
	if len(writer.writes) != 1 {
		t.Fatalf("expected 1 persisted write, got %d", len(writer.writes))
	}
	got := writer.writes[0]
	if got.stream != "test-stream" {
		t.Errorf("stream = %q, want test-stream", got.stream)
	}
	if got.messageID != "1-0" {
		t.Errorf("messageID = %q, want 1-0", got.messageID)
	}
	if got.eventType != "payment.captured" {
		t.Errorf("eventType = %q, want payment.captured", got.eventType)
	}
	if string(got.payload) != `{"order_id":"o1"}` {
		t.Errorf("payload = %q, want the raw JSON payload unchanged", got.payload)
	}
}

func TestHandleMessageDoesNotAckOnWriteFailure(t *testing.T) {
	writer := &fakeEventLogWriter{failOn: "2-0"}
	c := NewStreamConsumer(nil, "test-stream", "test-group", writer)

	ok := c.handleMessage(context.Background(), "2-0", map[string]any{
		"event_type": "payment.failed",
		"payload":    `{"order_id":"o2"}`,
	})

	if ok {
		t.Fatal("expected handleMessage to report failure so the caller leaves the message unacked for redelivery")
	}
	if len(writer.writes) != 0 {
		t.Fatalf("expected no successful writes recorded, got %d", len(writer.writes))
	}
}

func TestHandleMessageToleratesMissingFields(t *testing.T) {
	// A malformed or partially-written stream entry (e.g. a future
	// producer forgets a field) should still persist what's there
	// rather than panicking on a failed type assertion -- both fields
	// are read via the two-value "comma ok" form specifically so a
	// missing or wrong-typed value degrades to an empty string instead
	// of crashing the consumer loop.
	writer := &fakeEventLogWriter{}
	c := NewStreamConsumer(nil, "test-stream", "test-group", writer)

	ok := c.handleMessage(context.Background(), "3-0", map[string]any{
		"event_type": "cart.item_added",
		// "payload" deliberately omitted.
	})

	if !ok {
		t.Fatal("expected handleMessage to still succeed and persist an empty payload")
	}
	if len(writer.writes) != 1 {
		t.Fatalf("expected 1 persisted write, got %d", len(writer.writes))
	}
	if string(writer.writes[0].payload) != "" {
		t.Errorf("payload = %q, want empty string for a missing field", writer.writes[0].payload)
	}
}
