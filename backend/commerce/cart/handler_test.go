package cart

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/garinesaiajay/commerceos/events"
)

// fakeEventPublisher is an in-memory events.EventBus for asserting on
// what commerce/cart.Handler publishes (item 42, P3,
// PLAN-06-ADDITIONAL-OPPORTUNITIES.md §4). Mirrors this package's own
// fakeRepository/fakeVariantReader style (service_test.go).
type fakeEventPublisher struct {
	published []events.OutboxEvent
	err       error
}

func (f *fakeEventPublisher) Publish(ctx context.Context, stream string, event events.OutboxEvent) error {
	if f.err != nil {
		return f.err
	}
	f.published = append(f.published, event)
	return nil
}

func newTestHandler() (*Handler, *fakeRepository, *fakeEventPublisher) {
	repo := newFakeRepository()
	variantReader := newFakeVariantReader()
	service := NewService(repo, variantReader)
	publisher := &fakeEventPublisher{}
	handler := NewHandler(service).WithEventPublisher(publisher, "test-stream")
	return handler, repo, publisher
}

func farFuture() time.Time {
	return time.Now().Add(24 * time.Hour)
}

func seedCart(t *testing.T, repo *fakeRepository, id string) {
	t.Helper()
	if err := repo.CreateCart(context.Background(), Cart{
		ID:         id,
		MerchantID: "merchant_001",
		Currency:   "INR",
		ExpiresAt:  farFuture(),
	}); err != nil {
		t.Fatalf("seed cart %s: %v", id, err)
	}
}

func TestAddItemPublishesCartItemAddedOnSuccess(t *testing.T) {
	handler, repo, publisher := newTestHandler()
	seedCart(t, repo, "cart_1")

	body := strings.NewReader(`{"variant_id":"airpods-pro-2-default","quantity":1}`)
	req := httptest.NewRequest(http.MethodPost, "/carts/cart_1/items", body)
	rec := httptest.NewRecorder()

	handler.AddItem(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(publisher.published) != 1 {
		t.Fatalf("expected exactly one published event, got %d", len(publisher.published))
	}

	ev := publisher.published[0]
	if ev.EventType != "cart.item_added" {
		t.Fatalf("expected event_type cart.item_added, got %q", ev.EventType)
	}

	var payload cartItemAddedEvent
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.CartID != "cart_1" {
		t.Fatalf("expected cart_id cart_1, got %q", payload.CartID)
	}
}

func TestAddItemDoesNotPublishOnFailure(t *testing.T) {
	handler, _, publisher := newTestHandler()
	// No cart seeded -- AddItem fails with ErrCartNotFound before ever
	// reaching publishCartItemAdded.
	body := strings.NewReader(`{"variant_id":"airpods-pro-2-default","quantity":1}`)
	req := httptest.NewRequest(http.MethodPost, "/carts/missing/items", body)
	rec := httptest.NewRecorder()

	handler.AddItem(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if len(publisher.published) != 0 {
		t.Fatalf("expected no published event on failure, got %d", len(publisher.published))
	}
}

func TestAddItemWithoutPublisherStillSucceeds(t *testing.T) {
	// WithEventPublisher is optional (this package's WithX convention,
	// same as payment.Handler.WithCallCounter) -- a Handler built with
	// plain NewHandler must behave exactly as it did before this item,
	// not panic on a nil h.publisher.
	repo := newFakeRepository()
	variantReader := newFakeVariantReader()
	service := NewService(repo, variantReader)
	handler := NewHandler(service)
	seedCart(t, repo, "cart_2")

	body := strings.NewReader(`{"variant_id":"airpods-pro-2-default","quantity":1}`)
	req := httptest.NewRequest(http.MethodPost, "/carts/cart_2/items", body)
	rec := httptest.NewRecorder()

	handler.AddItem(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAddItemSucceedsEvenWhenPublishFails(t *testing.T) {
	handler, repo, publisher := newTestHandler()
	publisher.err = errors.New("redis unavailable")
	seedCart(t, repo, "cart_3")

	body := strings.NewReader(`{"variant_id":"airpods-pro-2-default","quantity":1}`)
	req := httptest.NewRequest(http.MethodPost, "/carts/cart_3/items", body)
	rec := httptest.NewRecorder()

	handler.AddItem(rec, req)

	// A publish failure is logged, never surfaced to the buyer -- see
	// WithEventPublisher's doc comment on why this is the right
	// trade-off for a best-effort, at-most-once notification.
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 even though publish failed, got %d: %s", rec.Code, rec.Body.String())
	}
}
