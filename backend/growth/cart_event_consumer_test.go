package growth

import (
	"context"
	"testing"

	"github.com/garinesaiajay/commerceos/commerce/cart"
	"github.com/garinesaiajay/commerceos/commerce/catalog"
)

// fakeCapturingStore records every Save call so precomputeSuggestion's
// actual persistence side effect -- the whole point of this consumer
// existing -- can be asserted on directly, unlike suggest_test.go's
// own fakeRecommendationStore (same package, reused nowhere here)
// which deliberately discards every save because those tests only
// care about the wire response.
type fakeCapturingStore struct {
	saved []Recommendation
}

func (f *fakeCapturingStore) Save(ctx context.Context, r Recommendation) error {
	f.saved = append(f.saved, r)
	return nil
}

func newTestCartEventConsumer(
	products map[string]catalog.Product,
	carts map[string]cart.Cart,
	dismissed map[string][]string,
) (*CartEventConsumer, *fakeCapturingStore) {
	fc := &fakeCatalog{products: products}
	store := &fakeCapturingStore{}
	agent := NewGrowthAgent(fc, store)
	consumer := NewCartEventConsumer(
		nil, // client -- unused by precomputeSuggestion/handleCartItemAdded directly
		"test-stream",
		"test-group",
		fc,
		&fakeCartReader{carts: carts},
		agent,
		&fakeDismissals{dismissed: dismissed},
	)
	return consumer, store
}

func TestPrecomputeSuggestionSavesRecommendationForComplementaryProduct(t *testing.T) {
	products := map[string]catalog.Product{
		"airpods": testProduct("airpods", "m1", 200_000, 5, "earbuds"),
		"case":    testProduct("case", "m1", 50_000, 5, "earbuds"),
	}
	carts := map[string]cart.Cart{
		"cart_1": {
			ID:         "cart_1",
			MerchantID: "m1",
			Subtotal:   200_000,
			Items:      []cart.CartItem{{ProductID: "airpods", VariantID: "airpods-default", Quantity: 1}},
		},
	}
	consumer, store := newTestCartEventConsumer(products, carts, nil)

	if err := consumer.precomputeSuggestion(context.Background(), "cart_1"); err != nil {
		t.Fatalf("precomputeSuggestion: %v", err)
	}

	if len(store.saved) != 1 {
		t.Fatalf("expected exactly one recommendation saved, got %d", len(store.saved))
	}
	rec := store.saved[0]
	if rec.CartID != "cart_1" || rec.ProductID != "case" {
		t.Fatalf("expected a recommendation for cart_1/case, got cart_id=%s product_id=%s", rec.CartID, rec.ProductID)
	}
	if rec.Decision != "RECOMMEND" {
		t.Fatalf("expected RECOMMEND, got %s (%s)", rec.Decision, rec.Reason)
	}
}

func TestPrecomputeSuggestionNoopWhenCartEmpty(t *testing.T) {
	carts := map[string]cart.Cart{
		"cart_empty": {ID: "cart_empty", MerchantID: "m1", Items: []cart.CartItem{}},
	}
	consumer, store := newTestCartEventConsumer(nil, carts, nil)

	if err := consumer.precomputeSuggestion(context.Background(), "cart_empty"); err != nil {
		t.Fatalf("precomputeSuggestion: %v", err)
	}
	if len(store.saved) != 0 {
		t.Fatalf("expected no recommendation for an empty cart, got %d", len(store.saved))
	}
}

func TestPrecomputeSuggestionExcludesDismissedProduct(t *testing.T) {
	products := map[string]catalog.Product{
		"airpods": testProduct("airpods", "m1", 200_000, 5, "earbuds"),
		"case":    testProduct("case", "m1", 50_000, 5, "earbuds"),
	}
	carts := map[string]cart.Cart{
		"cart_1": {
			ID:         "cart_1",
			MerchantID: "m1",
			Subtotal:   200_000,
			Items:      []cart.CartItem{{ProductID: "airpods", VariantID: "airpods-default", Quantity: 1}},
		},
	}
	// The only complementary product has already been dismissed for
	// this cart -- precomputing a recommendation for it anyway would
	// resurrect a "no thanks" the buyer already gave.
	dismissed := map[string][]string{"cart_1": {"case"}}
	consumer, store := newTestCartEventConsumer(products, carts, dismissed)

	if err := consumer.precomputeSuggestion(context.Background(), "cart_1"); err != nil {
		t.Fatalf("precomputeSuggestion: %v", err)
	}
	if len(store.saved) != 0 {
		t.Fatalf("expected no recommendation for a dismissed product, got %d", len(store.saved))
	}
}

func TestPrecomputeSuggestionReturnsErrorWhenCartNotFound(t *testing.T) {
	consumer, store := newTestCartEventConsumer(nil, map[string]cart.Cart{}, nil)

	err := consumer.precomputeSuggestion(context.Background(), "does-not-exist")
	if err == nil {
		t.Fatal("expected an error for an unknown cart")
	}
	if len(store.saved) != 0 {
		t.Fatalf("expected no recommendation saved on error, got %d", len(store.saved))
	}
}

func TestHandleCartItemAddedIgnoresMalformedPayload(t *testing.T) {
	consumer, store := newTestCartEventConsumer(nil, map[string]cart.Cart{}, nil)

	// Deliberately not JSON, and separately JSON with no cart_id --
	// neither should reach precomputeSuggestion (which would otherwise
	// error against the empty carts map above, but the point of this
	// test is that it never gets that far).
	consumer.handleCartItemAdded(context.Background(), `not json`)
	consumer.handleCartItemAdded(context.Background(), `{}`)

	if len(store.saved) != 0 {
		t.Fatalf("expected no recommendation saved for malformed/empty payloads, got %d", len(store.saved))
	}
}
