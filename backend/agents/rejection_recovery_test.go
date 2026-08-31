package agents

import (
	"context"
	"testing"

	"github.com/garinesaiajay/commerceos/commerce/catalog"
	"github.com/garinesaiajay/commerceos/commerce/order"
)

// fakeRecoveryCatalog is an in-memory RecoveryCatalogReader -- same
// fake-over-real-Postgres posture as catalog/service_test.go's
// fakeRepository and growth/suggest_test.go's fakes: suggestSubstitute
// is pure orchestration over ListProducts/GetProduct, both easily
// fakeable, so no real DB is needed to test its decision logic.
type fakeRecoveryCatalog struct {
	products []catalog.Product
}

func (f *fakeRecoveryCatalog) ListProducts(ctx context.Context) ([]catalog.Product, error) {
	return f.products, nil
}

func (f *fakeRecoveryCatalog) GetProduct(ctx context.Context, id string) (catalog.Product, error) {
	for _, p := range f.products {
		if p.ID == id {
			return p, nil
		}
	}
	return catalog.Product{}, catalog.ErrProductNotFound
}

// testProduct builds a minimal but realistic catalog.Product -- price
// is given in rupees for readability at call sites (converted to paise
// here, matching how every amount elsewhere in this codebase is
// stored), always with its own "<id>-default" variant so
// defaultVariantID's primary path is exercised, not its fallback.
func testProduct(id string, priceRupees int64, useCases ...string) catalog.Product {
	price := catalog.Money{Amount: priceRupees * 100, Currency: "INR"}
	return catalog.Product{
		ID:           id,
		Title:        id + " title",
		Price:        price,
		Availability: 5,
		UseCases:     useCases,
		Variants: []catalog.ProductVariant{
			{ID: id + "-default", ProductID: id, Price: price, Availability: 5},
		},
	}
}

func testOrderItem(productID string, totalRupees int64) order.OrderItem {
	return order.OrderItem{
		ProductID: productID,
		VariantID: productID + "-default",
		Title:     productID + " title",
		Quantity:  1,
		UnitPrice: totalRupees * 100,
		Total:     totalRupees * 100,
	}
}

const testCeiling = int64(30_000_00) // ₹30,000, matches policy.DefaultConfig()'s real ceiling

func TestSuggestSubstituteWithinCeilingNotAvailable(t *testing.T) {
	h := NewRejectionRecoveryHandler(nil, &fakeRecoveryCatalog{}, nil, nil, testCeiling)
	ord := order.Order{
		Subtotal: testCeiling, // exactly at the ceiling -- not over it
		Items:    []order.OrderItem{testOrderItem("p1", 30_000)},
	}
	got, err := h.suggestSubstitute(context.Background(), ord)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Available {
		t.Fatalf("expected Available=false for an order at/under the ceiling, got %+v", got)
	}
}

func TestSuggestSubstituteNoItems(t *testing.T) {
	h := NewRejectionRecoveryHandler(nil, &fakeRecoveryCatalog{}, nil, nil, testCeiling)
	ord := order.Order{Subtotal: testCeiling + 100_00, Items: nil}
	got, err := h.suggestSubstitute(context.Background(), ord)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Available {
		t.Fatalf("expected Available=false for an order with no items, got %+v", got)
	}
}

func TestSuggestSubstitutePicksMostExpensiveItem(t *testing.T) {
	catalogReader := &fakeRecoveryCatalog{products: []catalog.Product{
		testProduct("p1", 20_000, "audio"),
		testProduct("p2", 12_000, "audio"),
		testProduct("cheap-sub", 6_000, "audio"), // within budget once p2 (the pricier item) is swapped out
	}}
	h := NewRejectionRecoveryHandler(nil, catalogReader, nil, nil, testCeiling)
	// Subtotal 32,000 > ceiling 30,000 by 2,000. Items: p1 (20,000) and
	// p2 (12,000). p1 is more expensive, so it should be the replace
	// candidate -- budget for its substitute is 20,000 - 2,000 = 18,000.
	ord := order.Order{
		Subtotal: 32_000_00,
		Items:    []order.OrderItem{testOrderItem("p2", 12_000), testOrderItem("p1", 20_000)},
	}
	got, err := h.suggestSubstitute(context.Background(), ord)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Available {
		t.Fatalf("expected a substitute to be found, got %+v", got)
	}
	if got.ReplacedItem == nil || got.ReplacedItem.ProductID != "p1" {
		t.Fatalf("expected the more expensive item (p1) to be replaced, got %+v", got.ReplacedItem)
	}
}

func TestSuggestSubstituteBudgetTooSmall(t *testing.T) {
	h := NewRejectionRecoveryHandler(nil, &fakeRecoveryCatalog{}, nil, nil, testCeiling)
	// Subtotal 60,000 > ceiling 30,000 by 30,000 -- but the only item
	// only costs 20,000, less than the overage itself. Even removing
	// it entirely wouldn't be enough, let alone swapping it for
	// something cheaper.
	ord := order.Order{
		Subtotal: 60_000_00,
		Items:    []order.OrderItem{testOrderItem("p1", 20_000)},
	}
	got, err := h.suggestSubstitute(context.Background(), ord)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Available {
		t.Fatalf("expected Available=false when the overage exceeds the candidate item's own price, got %+v", got)
	}
}

func TestSuggestSubstituteHappyPath(t *testing.T) {
	catalogReader := &fakeRecoveryCatalog{products: []catalog.Product{
		testProduct("expensive", 25_000, "audio"),
		testProduct("mid", 8_000, "audio"),        // within the 18,000 budget (20,000 - 2,000 overage)
		testProduct("pricey-alt", 19_000, "audio"), // over the 18,000 budget, must be excluded by the hard constraint
	}}
	h := NewRejectionRecoveryHandler(nil, catalogReader, nil, nil, testCeiling)
	// Subtotal 32,000 > ceiling 30,000 by 2,000. Single item "expensive"
	// at 20,000 -> budget for its substitute is 20,000 - 2,000 = 18,000.
	ord := order.Order{
		Subtotal: 32_000_00,
		Items:    []order.OrderItem{testOrderItem("expensive", 20_000)},
	}
	got, err := h.suggestSubstitute(context.Background(), ord)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Available {
		t.Fatalf("expected a substitute to be found, got %+v", got)
	}
	if got.Substitute == nil {
		t.Fatalf("expected a non-nil substitute")
	}
	if got.Substitute.ProductID == "expensive" {
		t.Fatalf("substitute must not be the replaced product itself")
	}
	wantNewSubtotal := ord.Subtotal - 20_000_00 + got.Substitute.Price
	if got.NewSubtotal != wantNewSubtotal {
		t.Fatalf("NewSubtotal = %d, want %d", got.NewSubtotal, wantNewSubtotal)
	}
	if got.NewSubtotal > testCeiling {
		t.Fatalf("proposed substitute leaves the order over the ceiling: new subtotal %d > ceiling %d", got.NewSubtotal, testCeiling)
	}
}

func TestSuggestSubstituteExcludesItemsAlreadyInOrder(t *testing.T) {
	// The only in-budget candidate is the item ALREADY on the order
	// (e.g. re-scored at a lower price) -- must be excluded, leaving no
	// substitute available, rather than "swapping" an item for itself.
	catalogReader := &fakeRecoveryCatalog{products: []catalog.Product{
		testProduct("expensive", 20_000, "audio"),
		testProduct("already-in-cart", 5_000, "audio"),
	}}
	h := NewRejectionRecoveryHandler(nil, catalogReader, nil, nil, testCeiling)
	ord := order.Order{
		Subtotal: 32_000_00,
		Items: []order.OrderItem{
			testOrderItem("expensive", 20_000),
			testOrderItem("already-in-cart", 12_000),
		},
	}
	got, err := h.suggestSubstitute(context.Background(), ord)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Available {
		t.Fatalf("expected no substitute once the only in-budget candidate is excluded as already-in-order, got %+v", got)
	}
}

func TestSuggestSubstituteNoInBudgetProduct(t *testing.T) {
	catalogReader := &fakeRecoveryCatalog{products: []catalog.Product{
		testProduct("still-too-expensive", 19_000, "audio"),
	}}
	h := NewRejectionRecoveryHandler(nil, catalogReader, nil, nil, testCeiling)
	ord := order.Order{
		Subtotal: 32_000_00,
		Items:    []order.OrderItem{testOrderItem("expensive", 20_000)},
	}
	got, err := h.suggestSubstitute(context.Background(), ord)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Available {
		t.Fatalf("expected Available=false when nothing in the catalog fits the budget, got %+v", got)
	}
}

func TestDefaultVariantIDPrefersConventionalDefault(t *testing.T) {
	p := testProduct("p1", 100, "audio")
	if got := defaultVariantID(p); got != "p1-default" {
		t.Fatalf("defaultVariantID = %q, want %q", got, "p1-default")
	}
}

func TestDefaultVariantIDFallsBackToFirstVariant(t *testing.T) {
	p := catalog.Product{
		ID: "p2",
		Variants: []catalog.ProductVariant{
			{ID: "p2-red", ProductID: "p2"},
			{ID: "p2-blue", ProductID: "p2"},
		},
	}
	if got := defaultVariantID(p); got != "p2-red" {
		t.Fatalf("defaultVariantID = %q, want %q (first variant)", got, "p2-red")
	}
}

func TestDefaultVariantIDFallsBackToSyntheticWhenNoVariants(t *testing.T) {
	p := catalog.Product{ID: "p3"}
	if got := defaultVariantID(p); got != "p3-default" {
		t.Fatalf("defaultVariantID = %q, want %q", got, "p3-default")
	}
}
