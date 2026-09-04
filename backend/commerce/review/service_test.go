package review

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/garinesaiajay/commerceos/commerce/order"
	"github.com/garinesaiajay/commerceos/statemachine"
)

// fakeOrderReader is an in-memory order.OrderReader for tests -- no
// Postgres needed to exercise Service.Submit's validation paths.
type fakeOrderReader struct {
	orders map[string]order.Order
}

func (f fakeOrderReader) GetOrder(ctx context.Context, orderID string) (order.Order, error) {
	o, ok := f.orders[orderID]
	if !ok {
		return order.Order{}, order.ErrOrderNotFound
	}
	return o, nil
}

// fakeRepo is an in-memory review.Repository for tests.
type fakeRepo struct {
	reviews []Review
	nextID  int64
}

func (f *fakeRepo) Create(ctx context.Context, r Review) (Review, error) {
	f.nextID++
	r.ID = f.nextID
	r.CreatedAt = time.Now()
	r.VerifiedPurchase = r.OrderID != ""
	f.reviews = append(f.reviews, r)
	return r, nil
}

func (f *fakeRepo) ListByProduct(ctx context.Context, productID string) ([]Review, error) {
	var out []Review
	for _, r := range f.reviews {
		if r.ProductID == productID {
			out = append(out, r)
		}
	}
	return out, nil
}

func newTestService(orders map[string]order.Order) (*Service, *fakeRepo) {
	repo := &fakeRepo{}
	return NewService(repo, fakeOrderReader{orders: orders}), repo
}

// TestSubmitValidReview proves a review filed against a real order for
// a product that order actually contains is accepted and comes back
// verified.
func TestSubmitValidReview(t *testing.T) {
	svc, _ := newTestService(map[string]order.Order{
		"order_1": {ID: "order_1", Status: statemachine.OrderPaid, Items: []order.OrderItem{{ProductID: "airpods-pro-2"}}},
	})

	rev, err := svc.Submit(context.Background(), "order_1", "airpods-pro-2", "Test Buyer", 5, "Great!")
	if err != nil {
		t.Fatal(err)
	}
	if !rev.VerifiedPurchase {
		t.Fatal("expected verified_purchase true for a review filed through a real order")
	}
	if rev.OrderID != "order_1" || rev.ProductID != "airpods-pro-2" || rev.Rating != 5 {
		t.Fatalf("unexpected review: %+v", rev)
	}
	if rev.BuyerReference != "Test Buyer" {
		t.Fatalf("expected buyer reference to pass through, got %s", rev.BuyerReference)
	}
}

// TestSubmitDefaultsBuyerReference proves a buyer who leaves the name
// field blank (there is no buyer auth in this project) still gets a
// stored review, attributed to a generic label rather than an empty
// string.
func TestSubmitDefaultsBuyerReference(t *testing.T) {
	svc, _ := newTestService(map[string]order.Order{
		"order_1": {ID: "order_1", Status: statemachine.OrderPaid, Items: []order.OrderItem{{ProductID: "airpods-pro-2"}}},
	})

	rev, err := svc.Submit(context.Background(), "order_1", "airpods-pro-2", "", 4, "")
	if err != nil {
		t.Fatal(err)
	}
	if rev.BuyerReference != defaultBuyerReference {
		t.Fatalf("expected default buyer reference %q, got %q", defaultBuyerReference, rev.BuyerReference)
	}
}

// TestSubmitRejectsInvalidRating proves out-of-range ratings never
// reach storage.
func TestSubmitRejectsInvalidRating(t *testing.T) {
	svc, repo := newTestService(map[string]order.Order{
		"order_1": {ID: "order_1", Status: statemachine.OrderPaid, Items: []order.OrderItem{{ProductID: "airpods-pro-2"}}},
	})

	for _, rating := range []int{0, 6, -1} {
		_, err := svc.Submit(context.Background(), "order_1", "airpods-pro-2", "", rating, "")
		if !errors.Is(err, ErrInvalidRating) {
			t.Fatalf("rating %d: expected ErrInvalidRating, got %v", rating, err)
		}
	}
	if len(repo.reviews) != 0 {
		t.Fatalf("expected no reviews stored, got %d", len(repo.reviews))
	}
}

// TestSubmitRejectsProductNotInOrder proves VerifiedPurchase can't be
// gamed by naming a product_id the order never actually contained.
func TestSubmitRejectsProductNotInOrder(t *testing.T) {
	svc, _ := newTestService(map[string]order.Order{
		"order_1": {ID: "order_1", Status: statemachine.OrderPaid, Items: []order.OrderItem{{ProductID: "airpods-pro-2"}}},
	})

	_, err := svc.Submit(context.Background(), "order_1", "airpods-case", "", 5, "")
	if !errors.Is(err, ErrProductNotInOrder) {
		t.Fatalf("expected ErrProductNotInOrder, got %v", err)
	}
}

// TestSubmitRejectsUnpaidOrder proves the item-16-style gap a fresh
// audit against PLAN-02-CATALOG-AND-COMMERCE.md found: Submit used to
// check only that the order existed and contained the product, never
// that it had actually completed payment -- so a review (with
// VerifiedPurchase implicitly true) could be filed against an order
// still mid-checkout or one that never completed at all. Covers every
// non-eligible status on the order lifecycle
// (statemachine.OrderTransitionTable), not just one.
func TestSubmitRejectsUnpaidOrder(t *testing.T) {
	for _, status := range []string{
		statemachine.OrderDraft,
		statemachine.OrderAuthorized,
		statemachine.OrderPaymentPending,
		statemachine.OrderFailed,
		statemachine.OrderCancelled,
	} {
		svc, repo := newTestService(map[string]order.Order{
			"order_1": {ID: "order_1", Status: status, Items: []order.OrderItem{{ProductID: "airpods-pro-2"}}},
		})

		_, err := svc.Submit(context.Background(), "order_1", "airpods-pro-2", "", 5, "")
		if !errors.Is(err, ErrOrderNotEligibleForReview) {
			t.Errorf("status %q: expected ErrOrderNotEligibleForReview, got %v", status, err)
		}
		if len(repo.reviews) != 0 {
			t.Errorf("status %q: expected no review stored, got %d", status, len(repo.reviews))
		}
	}
}

// TestSubmitAcceptsEveryPostPaymentStatus proves the gate does not
// over-correct: any order.Status from OrderPaid onward (the buyer's
// payment succeeded; fulfillment progress afterward is orthogonal to
// whether they may leave a review) is still accepted.
func TestSubmitAcceptsEveryPostPaymentStatus(t *testing.T) {
	for _, status := range []string{
		statemachine.OrderPaid,
		statemachine.OrderFulfillmentPending,
		statemachine.OrderCompleted,
	} {
		svc, _ := newTestService(map[string]order.Order{
			"order_1": {ID: "order_1", Status: status, Items: []order.OrderItem{{ProductID: "airpods-pro-2"}}},
		})

		if _, err := svc.Submit(context.Background(), "order_1", "airpods-pro-2", "", 5, ""); err != nil {
			t.Errorf("status %q: expected review to be accepted, got %v", status, err)
		}
	}
}

// TestSubmitRejectsUnknownOrder proves order.ErrOrderNotFound
// propagates unwrapped so Handler.Submit's errors.Is switch maps it to
// a 404, same convention order.Handler.GetOrder already uses.
func TestSubmitRejectsUnknownOrder(t *testing.T) {
	svc, _ := newTestService(map[string]order.Order{})

	_, err := svc.Submit(context.Background(), "does-not-exist", "airpods-pro-2", "", 5, "")
	if !errors.Is(err, order.ErrOrderNotFound) {
		t.Fatalf("expected order.ErrOrderNotFound, got %v", err)
	}
}

// TestListByProductFiltersByProduct proves each product only sees its
// own reviews.
func TestListByProductFiltersByProduct(t *testing.T) {
	svc, _ := newTestService(map[string]order.Order{
		"order_1": {ID: "order_1", Status: statemachine.OrderPaid, Items: []order.OrderItem{{ProductID: "airpods-pro-2"}, {ProductID: "airpods-case"}}},
	})

	if _, err := svc.Submit(context.Background(), "order_1", "airpods-pro-2", "A", 5, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Submit(context.Background(), "order_1", "airpods-case", "B", 3, ""); err != nil {
		t.Fatal(err)
	}

	got, err := svc.ListByProduct(context.Background(), "airpods-pro-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ProductID != "airpods-pro-2" {
		t.Fatalf("expected exactly one airpods-pro-2 review, got %+v", got)
	}
}
