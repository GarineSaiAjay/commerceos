// Package tools is the shared tool layer PLAN-01-AGENTIC-CORE.md §1
// asks for: the small set of read-only and cart-building operations
// that both the external MCP surface (backend/mcp/tools.go) and the
// in-app agent's own tool-calling loop (backend/agents, item 18) call
// through -- one implementation, two callers, instead of the same
// business logic duplicated behind an HTTP/JSON-RPC boundary on one
// side and a direct Go call on the other.
//
// Deliberately excluded from this package: request_authorization,
// create_checkout, execute_authorized_checkout, get_payment_status,
// and explain_decision. Those stay exactly where they are in
// backend/mcp/tools.go, reachable only over the external MCP surface --
// PLAN-01 §2 is explicit that the in-app bounded tool-calling loop must
// never be able to reach the money-moving/authorization tools itself;
// keeping them out of this shared package is what makes that
// structurally true rather than merely policy.
package tools

import (
	"context"
	"fmt"

	"github.com/garinesaiajay/commerceos/commerce/cart"
	"github.com/garinesaiajay/commerceos/commerce/catalog"
	"github.com/garinesaiajay/commerceos/growth"
)

// Dependencies bundles the services this package's tools wrap. A
// strict subset of backend/mcp/tools.go's own Dependencies -- this
// package never touches Order, Payment, Policy, or Explain, matching
// the exclusion list in this file's doc comment.
type Dependencies struct {
	Catalog *catalog.Service
	Cart    *cart.Service
	Growth  *growth.GrowthAgent
}

// SearchProductsRequest is the search_products tool's input.
type SearchProductsRequest struct {
	Budget    int64
	Category  string
	Priority  string
	Recipient string
}

// SearchProducts is the search_products tool's logic. A non-positive
// Budget means "browsing, no filter" and returns the full catalog --
// there is no sensible hard-filter for an agent that hasn't named a
// budget yet, and defaulting Budget to 0 would incorrectly exclude
// every priced product.
func SearchProducts(ctx context.Context, deps Dependencies, req SearchProductsRequest) (any, error) {
	if req.Budget <= 0 {
		return deps.Catalog.ListProducts(ctx)
	}

	searcher := NewSearcher(deps.Catalog)
	results, err := searcher.Search(ctx, SearchFilter{
		Budget:    req.Budget,
		Category:  req.Category,
		Priority:  req.Priority,
		Recipient: req.Recipient,
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

// GetProductRequest is the get_product tool's input.
type GetProductRequest struct {
	ProductID string
}

// GetProduct is the get_product tool's logic.
func GetProduct(ctx context.Context, deps Dependencies, req GetProductRequest) (any, error) {
	if req.ProductID == "" {
		return nil, fmt.Errorf("product_id required")
	}
	product, err := deps.Catalog.GetProduct(ctx, req.ProductID)
	if err != nil {
		return nil, err
	}
	return product, nil
}

// CreateCartRequest is the create_cart tool's input.
type CreateCartRequest struct {
	ID         string
	MerchantID string
	Currency   string
}

// CreateCart is the create_cart tool's logic. No money moves.
func CreateCart(ctx context.Context, deps Dependencies, req CreateCartRequest) (any, error) {
	if req.ID == "" {
		return nil, fmt.Errorf("cart_id required")
	}
	if req.MerchantID == "" {
		req.MerchantID = "merchant_001"
	}
	if req.Currency == "" {
		req.Currency = "INR"
	}
	return deps.Cart.CreateCart(ctx, req.ID, req.MerchantID, req.Currency)
}

// AddItemRequest is the add_item tool's input.
type AddItemRequest struct {
	CartID    string
	VariantID string
	Quantity  int
}

// AddItem is the add_item tool's logic. The catalog remains the source
// of truth for product_id and price: cart.Service.AddItem looks both
// up from variant_id server-side. No money moves.
func AddItem(ctx context.Context, deps Dependencies, req AddItemRequest) (any, error) {
	if req.CartID == "" || req.VariantID == "" {
		return nil, fmt.Errorf("cart_id and variant_id required")
	}
	if req.Quantity <= 0 {
		req.Quantity = 1
	}

	if err := deps.Cart.AddItem(ctx, req.CartID, cart.CartItem{
		VariantID: req.VariantID,
		Quantity:  req.Quantity,
	}); err != nil {
		return nil, err
	}

	// Return the updated cart so the caller can see the item (and its
	// authoritative catalog price) land without a second round trip.
	return deps.Cart.GetCart(ctx, req.CartID)
}

// CalculateTotalRequest is the calculate_total tool's input.
type CalculateTotalRequest struct {
	Items []CalculateTotalItem
}

// CalculateTotalItem is one line item in a CalculateTotalRequest.
type CalculateTotalItem struct {
	UnitPrice int64
	Quantity  int
}

// CalculateTotal is the calculate_total tool's logic. Pure; no side
// effects, no dependency on Dependencies at all.
func CalculateTotal(_ context.Context, req CalculateTotalRequest) (any, error) {
	var total int64
	for _, it := range req.Items {
		total += it.UnitPrice * int64(it.Quantity)
	}
	return map[string]any{"total": total}, nil
}

// RecommendBundleRequest is the recommend_bundle tool's input.
type RecommendBundleRequest struct {
	CartID       string
	CartTotal    int64
	Budget       int64
	Tolerance    float64
	ProductID    string
	PurchaseProb float64
	Margin       int64
	Confidence   float64
	RiskCost     int64
}

// RecommendBundle is the recommend_bundle tool's logic. No money moves --
// this only scores a candidate, it never decides or authorizes anything.
func RecommendBundle(ctx context.Context, deps Dependencies, req RecommendBundleRequest) (any, error) {
	if req.ProductID == "" {
		return nil, fmt.Errorf("product_id required")
	}
	rec, err := deps.Growth.EvaluateCandidate(
		ctx, req.CartID, req.CartTotal,
		growth.BudgetCheck{CartTotal: req.CartTotal, Budget: req.Budget, Tolerance: req.Tolerance},
		req.ProductID,
		growth.EVInputs{
			PurchaseProbability: req.PurchaseProb,
			IncrementalMargin:   req.Margin,
			Confidence:          req.Confidence,
			RiskCost:            req.RiskCost,
		},
	)
	if err != nil {
		return nil, err
	}
	return rec, nil
}
