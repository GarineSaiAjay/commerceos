package growth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/garinesaiajay/commerceos/commerce/cart"
	"github.com/garinesaiajay/commerceos/commerce/catalog"
	"github.com/garinesaiajay/commerceos/commerce/order"
)

// SuggestHandler's dependencies (CatalogSearcher/CartReader/OrderReader/
// DismissalStore) are all small, already-defined interfaces, so these
// tests exercise it directly against in-memory fakes instead of a real
// Postgres, unlike the DB-integration style used elsewhere in this
// package (repository_test.go's siblings in commerce/catalog) -- faster
// and it's the only coverage SuggestHandler has ever had.

type fakeCatalog struct {
	products map[string]catalog.Product
}

func (f *fakeCatalog) GetProduct(ctx context.Context, id string) (catalog.Product, error) {
	p, ok := f.products[id]
	if !ok {
		return catalog.Product{}, fmt.Errorf("product not found: %s", id)
	}
	return p, nil
}

func (f *fakeCatalog) ListProducts(ctx context.Context) ([]catalog.Product, error) {
	var out []catalog.Product
	for _, p := range f.products {
		out = append(out, p)
	}
	return out, nil
}

type fakeCartReader struct {
	carts map[string]cart.Cart
}

func (f *fakeCartReader) GetCart(ctx context.Context, id string) (cart.Cart, error) {
	c, ok := f.carts[id]
	if !ok {
		return cart.Cart{}, fmt.Errorf("cart not found: %s", id)
	}
	return c, nil
}

type fakeOrderReader struct {
	orders map[string]order.Order
}

func (f *fakeOrderReader) GetOrder(ctx context.Context, id string) (order.Order, error) {
	o, ok := f.orders[id]
	if !ok {
		return order.Order{}, fmt.Errorf("order not found: %s", id)
	}
	return o, nil
}

type fakeDismissals struct {
	dismissed map[string][]string // cart_id -> product_ids
}

func (f *fakeDismissals) SaveDismissal(ctx context.Context, cartID, productID string) error {
	f.dismissed[cartID] = append(f.dismissed[cartID], productID)
	return nil
}

func (f *fakeDismissals) ListDismissedProductIDs(ctx context.Context, cartID string) ([]string, error) {
	return f.dismissed[cartID], nil
}

// fakeRecommendationStore discards every save -- these tests care about
// the wire response, not persistence (PostgresStore.Save is exercised
// separately at the DB-integration layer).
type fakeRecommendationStore struct{}

func (fakeRecommendationStore) Save(ctx context.Context, r Recommendation) error { return nil }

func testProduct(id, merchantID string, priceAmount int64, availability int, useCases ...string) catalog.Product {
	return catalog.Product{
		ID:           id,
		Title:        id,
		Price:        catalog.Money{Amount: priceAmount, Currency: "INR"},
		Availability: availability,
		UseCases:     useCases,
		Merchant:     catalog.MerchantRef{ID: merchantID},
	}
}

// --- bestCandidate (pure function) ---

func TestBestCandidateScoresOverlapAndTieBreaksCheaper(t *testing.T) {
	signals := map[string]bool{"earbuds": true, "travel": true}
	products := []catalog.Product{
		testProduct("a", "m1", 500, 5, "earbuds"),
		testProduct("b", "m1", 300, 5, "earbuds", "travel"),
		testProduct("c", "m1", 100, 5, "travel"),
	}

	best, ok := bestCandidate(products, "m1", signals, map[string]bool{})
	if !ok {
		t.Fatal("expected a candidate")
	}
	if best.product.ID != "b" {
		t.Fatalf("expected b (2 overlapping tags beats a's and c's 1), got %s", best.product.ID)
	}
	if best.score != 2 {
		t.Fatalf("expected score 2, got %d", best.score)
	}
}

func TestBestCandidateTieBreaksTowardCheaperPrice(t *testing.T) {
	signals := map[string]bool{"earbuds": true}
	products := []catalog.Product{
		testProduct("expensive", "m1", 900, 5, "earbuds"),
		testProduct("cheap", "m1", 100, 5, "earbuds"),
	}

	best, ok := bestCandidate(products, "m1", signals, map[string]bool{})
	if !ok {
		t.Fatal("expected a candidate")
	}
	if best.product.ID != "cheap" {
		t.Fatalf("expected the cheaper of two equally-scored candidates, got %s", best.product.ID)
	}
}

func TestBestCandidateExcludesOutOfStockWrongMerchantAndExplicitlyExcluded(t *testing.T) {
	signals := map[string]bool{"earbuds": true}
	products := []catalog.Product{
		testProduct("excluded", "m1", 100, 5, "earbuds"),
		testProduct("oos", "m1", 100, 0, "earbuds"),
		testProduct("other-merchant", "m2", 100, 5, "earbuds"),
		testProduct("winner", "m1", 100, 3, "earbuds"),
	}

	best, ok := bestCandidate(products, "m1", signals, map[string]bool{"excluded": true})
	if !ok {
		t.Fatal("expected a candidate")
	}
	if best.product.ID != "winner" {
		t.Fatalf("expected winner (only real match), got %s", best.product.ID)
	}
}

func TestBestCandidateNoOverlapReturnsFalse(t *testing.T) {
	products := []catalog.Product{testProduct("a", "m1", 100, 5, "unrelated")}
	_, ok := bestCandidate(products, "m1", map[string]bool{"earbuds": true}, map[string]bool{})
	if ok {
		t.Fatal("expected no candidate when nothing overlaps the signal set")
	}
}

// --- SuggestForProduct (item 19, PLAN-03 §3) ---

func newTestSuggestHandler(products map[string]catalog.Product, carts map[string]cart.Cart, orders map[string]order.Order, dismissed map[string][]string) *SuggestHandler {
	fc := &fakeCatalog{products: products}
	agent := NewGrowthAgent(fc, fakeRecommendationStore{})
	return NewSuggestHandler(
		fc,
		&fakeCartReader{carts: carts},
		&fakeOrderReader{orders: orders},
		agent,
		&fakeDismissals{dismissed: dismissed},
	)
}

func decodeSuggestResponse(t *testing.T, rec *httptest.ResponseRecorder) SuggestResponse {
	t.Helper()
	var resp SuggestResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v (body: %s)", err, rec.Body.String())
	}
	return resp
}

func TestSuggestForProductFindsComplementaryItemWithNoCartYet(t *testing.T) {
	products := map[string]catalog.Product{
		"viewed": testProduct("viewed", "m1", 200_000, 5, "earbuds"),
		"case":   testProduct("case", "m1", 50_000, 5, "earbuds"),
	}
	h := newTestSuggestHandler(products, map[string]cart.Cart{}, nil, nil)

	body := strings.NewReader(`{"product_id":"viewed","cart_id":"cart_fresh"}`)
	req := httptest.NewRequest(http.MethodPost, "/growth/suggest/product", body)
	rec := httptest.NewRecorder()
	h.SuggestForProduct(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeSuggestResponse(t, rec)
	if !resp.Available || resp.Product == nil || resp.Product.ProductID != "case" {
		t.Fatalf("expected an available suggestion for 'case', got %+v", resp)
	}
	// cart_fresh doesn't exist yet (nothing added) -- must not error, and
	// the recommendation should still be keyed to that cart_id so it
	// converges with whatever this cart's live-cart suggestion computes
	// once shopping actually starts.
	if resp.Recommendation == nil || resp.Recommendation.CartID != "cart_fresh" {
		t.Fatalf("expected recommendation keyed to cart_fresh, got %+v", resp.Recommendation)
	}
}

func TestSuggestForProductRequiresProductIDAndCartID(t *testing.T) {
	h := newTestSuggestHandler(nil, nil, nil, nil)

	for _, body := range []string{`{"cart_id":"c1"}`, `{"product_id":"p1"}`, `{}`} {
		req := httptest.NewRequest(http.MethodPost, "/growth/suggest/product", strings.NewReader(body))
		rec := httptest.NewRecorder()
		h.SuggestForProduct(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %q: expected 400, got %d", body, rec.Code)
		}
	}
}

func TestSuggestForProductExcludesAlreadyDismissed(t *testing.T) {
	products := map[string]catalog.Product{
		"viewed": testProduct("viewed", "m1", 200_000, 5, "earbuds"),
		"case":   testProduct("case", "m1", 50_000, 5, "earbuds"),
	}
	// The only real candidate ("case") was already dismissed on this
	// cart -- the product-detail surface must honor a "No thanks" given
	// anywhere else in the same shopping session, not just its own
	// surface (PLAN-03-PROACTIVE-GROWTH-AGENT.md §5).
	h := newTestSuggestHandler(products, map[string]cart.Cart{}, nil, map[string][]string{"cart_1": {"case"}})

	req := httptest.NewRequest(http.MethodPost, "/growth/suggest/product", strings.NewReader(`{"product_id":"viewed","cart_id":"cart_1"}`))
	rec := httptest.NewRecorder()
	h.SuggestForProduct(rec, req)

	resp := decodeSuggestResponse(t, rec)
	if resp.Available {
		t.Fatalf("expected no suggestion once the only candidate is dismissed, got %+v", resp)
	}
}

func TestSuggestForProductUsesLiveCartToExcludeAndBudget(t *testing.T) {
	products := map[string]catalog.Product{
		"viewed":     testProduct("viewed", "m1", 200_000, 5, "earbuds"),
		"in-cart":    testProduct("in-cart", "m1", 50_000, 5, "earbuds"), // already in the cart -- must be excluded even though it overlaps
		"real-match": testProduct("real-match", "m1", 60_000, 5, "earbuds"),
	}
	carts := map[string]cart.Cart{
		"cart_1": {
			ID:         "cart_1",
			MerchantID: "m1",
			Items:      []cart.CartItem{{ProductID: "in-cart", VariantID: "in-cart-default", Quantity: 1, Total: 50_000}},
			Subtotal:   50_000,
		},
	}
	h := newTestSuggestHandler(products, carts, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/growth/suggest/product", strings.NewReader(`{"product_id":"viewed","cart_id":"cart_1"}`))
	rec := httptest.NewRecorder()
	h.SuggestForProduct(rec, req)

	resp := decodeSuggestResponse(t, rec)
	if !resp.Available || resp.Product == nil || resp.Product.ProductID != "real-match" {
		t.Fatalf("expected real-match (in-cart correctly excluded), got %+v", resp)
	}
}

// --- SuggestForOrder (item 19, PLAN-03 §4) ---

func TestSuggestForOrderScoresAgainstOrderItems(t *testing.T) {
	products := map[string]catalog.Product{
		"purchased": testProduct("purchased", "m1", 2_000_000, 5, "wireless"),
		"case":      testProduct("case", "m1", 50_000, 5, "wireless"),
	}
	orders := map[string]order.Order{
		"order_1": {
			ID:         "order_1",
			MerchantID: "m1",
			CartID:     "cart_done",
			Subtotal:   2_000_000,
			Items:      []order.OrderItem{{ProductID: "purchased", VariantID: "purchased-default", Quantity: 1, Total: 2_000_000}},
		},
	}
	h := newTestSuggestHandler(products, nil, orders, nil)

	req := httptest.NewRequest(http.MethodPost, "/growth/suggest/order", strings.NewReader(`{"order_id":"order_1"}`))
	rec := httptest.NewRecorder()
	h.SuggestForOrder(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeSuggestResponse(t, rec)
	if !resp.Available || resp.Product == nil || resp.Product.ProductID != "case" {
		t.Fatalf("expected 'case' suggested to complete the set, got %+v", resp)
	}
	// Keyed to the order's own cart_id, not the order_id, so this
	// converges with whatever else that shopping session already
	// recommended (see the evaluate doc comment in suggest.go).
	if resp.Recommendation == nil || resp.Recommendation.CartID != "cart_done" {
		t.Fatalf("expected recommendation keyed to the order's cart_id, got %+v", resp.Recommendation)
	}
}

func TestSuggestForOrderExcludesAlreadyPurchasedItems(t *testing.T) {
	products := map[string]catalog.Product{
		"purchased":  testProduct("purchased", "m1", 200_000, 5, "wireless"),
		"also-owned": testProduct("also-owned", "m1", 50_000, 5, "wireless"), // bought in the same order -- must not be re-suggested
	}
	orders := map[string]order.Order{
		"order_1": {
			ID:         "order_1",
			MerchantID: "m1",
			CartID:     "cart_done",
			Subtotal:   250_000,
			Items: []order.OrderItem{
				{ProductID: "purchased", VariantID: "purchased-default", Quantity: 1, Total: 200_000},
				{ProductID: "also-owned", VariantID: "also-owned-default", Quantity: 1, Total: 50_000},
			},
		},
	}
	h := newTestSuggestHandler(products, nil, orders, nil)

	req := httptest.NewRequest(http.MethodPost, "/growth/suggest/order", strings.NewReader(`{"order_id":"order_1"}`))
	rec := httptest.NewRecorder()
	h.SuggestForOrder(rec, req)

	resp := decodeSuggestResponse(t, rec)
	if resp.Available {
		t.Fatalf("expected no suggestion -- the only overlapping product was already bought in this order, got %+v", resp)
	}
}

func TestSuggestForOrderRequiresOrderReaderWired(t *testing.T) {
	fc := &fakeCatalog{products: map[string]catalog.Product{}}
	agent := NewGrowthAgent(fc, fakeRecommendationStore{})
	// orders deliberately nil -- proves the handler fails closed with a
	// clear 500 instead of a nil-pointer panic if ever wired without an
	// OrderReader (shouldn't happen in production -- main.go always
	// passes orderService).
	h := NewSuggestHandler(fc, &fakeCartReader{carts: map[string]cart.Cart{}}, nil, agent, &fakeDismissals{dismissed: map[string][]string{}})

	req := httptest.NewRequest(http.MethodPost, "/growth/suggest/order", strings.NewReader(`{"order_id":"order_1"}`))
	rec := httptest.NewRecorder()
	h.SuggestForOrder(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when OrderReader is nil, got %d", rec.Code)
	}
}

func TestSuggestForOrderNotFoundReturns404(t *testing.T) {
	h := newTestSuggestHandler(nil, nil, map[string]order.Order{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/growth/suggest/order", strings.NewReader(`{"order_id":"missing"}`))
	rec := httptest.NewRecorder()
	h.SuggestForOrder(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown order_id, got %d", rec.Code)
	}
}
