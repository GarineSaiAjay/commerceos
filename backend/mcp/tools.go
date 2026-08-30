package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/garinesaiajay/commerceos/commerce/cart"
	"github.com/garinesaiajay/commerceos/commerce/catalog"
	"github.com/garinesaiajay/commerceos/commerce/order"
	"github.com/garinesaiajay/commerceos/commerce/payment"
	"github.com/garinesaiajay/commerceos/growth"
	"github.com/garinesaiajay/commerceos/policy"
	"github.com/garinesaiajay/commerceos/tools"
)

// Dependencies bundles the services the MCP tools wrap. Tools are thin
// wrappers — they never duplicate business logic.
type Dependencies struct {
	Catalog *catalog.Service
	Cart    *cart.Service
	Order   *order.Service
	Payment *payment.Service
	Policy  *policy.Service
	Growth  *growth.GrowthAgent
	Explain func(policy.ProposedAction, policy.Mandate, string) string
}

// toolDeps narrows Dependencies to the shared tools package's own
// Dependencies shape (PLAN-01-AGENTIC-CORE.md §1, ROADMAP-PRIORITIZED.md
// P1 item 17) -- backend/tools deliberately never sees Order, Payment,
// Policy, or Explain, so this is a strict subset, not a full adapter.
func (d Dependencies) toolDeps() tools.Dependencies {
	return tools.Dependencies{Catalog: d.Catalog, Cart: d.Cart, Growth: d.Growth}
}

// schema builds a real JSON Schema object (type/properties/required) for
// a tool's InputSchema, instead of the bare `{"type":"object"}` stub
// every tool used to advertise. A generic MCP client (Claude Desktop,
// the MCP Inspector, any ACP/AP2-style external agent) discovers a
// tool's arguments ONLY from this schema — it never reads this repo's
// Go source — so an empty schema silently meant "no external client can
// use this tool correctly without out-of-band knowledge." required may
// be nil/empty for a tool with no mandatory fields.
func schema(properties map[string]any, required ...string) map[string]any {
	s := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func prop(kind, description string) map[string]any {
	return map[string]any{"type": kind, "description": description}
}

// RegisterTools wires the 11 narrow tools into the server.
func RegisterTools(s *Server, deps Dependencies) {
	s.Register(&Tool{
		Name: "search_products",
		Description: "Search the catalog by budget (rupees) and priority/category. " +
			"Deterministic; never bypasses budget/availability. Omit budget to browse " +
			"the full catalog unfiltered.",
		InputSchema: schema(map[string]any{
			"budget":    prop("integer", "Maximum price in rupees (not paise). Omit or 0 to browse the full catalog unfiltered."),
			"category":  prop("string", "Product category to prefer, e.g. \"earbuds\"."),
			"priority":  prop("string", "A feature tag to prioritize, e.g. \"active_noise_cancellation\", \"battery_life\"."),
			"recipient": prop("string", "Who the product is for, e.g. \"myself\", \"my brother\" (informational only)."),
		}),
		Handler: func(ctx context.Context, p json.RawMessage) (any, error) {
			return searchProducts(ctx, deps, p)
		},
	})

	s.Register(&Tool{
		Name:        "get_product",
		Description: "Get one product by ID.",
		InputSchema: schema(map[string]any{
			"product_id": prop("string", "Catalog product ID."),
		}, "product_id"),
		Handler: func(ctx context.Context, p json.RawMessage) (any, error) {
			return getProduct(ctx, deps, p)
		},
	})

	s.Register(&Tool{
		Name:        "create_cart",
		Description: "Create a cart. No money moves.",
		InputSchema: schema(map[string]any{
			"cart_id":     prop("string", "A new, caller-chosen unique cart ID."),
			"merchant_id": prop("string", "Merchant ID. Defaults to \"merchant_001\" if omitted."),
			"currency":    prop("string", "ISO 4217 currency code. Defaults to \"INR\" if omitted."),
		}, "cart_id"),
		Handler: func(ctx context.Context, p json.RawMessage) (any, error) {
			return createCart(ctx, deps, p)
		},
	})

	s.Register(&Tool{
		Name: "add_item",
		Description: "Add a product variant to an existing cart by variant_id and quantity. " +
			"No money moves; the catalog is the authoritative source for price, not the caller. " +
			"This is the only way to put an item into a cart before create_checkout — a cart with " +
			"nothing added will fail checkout with an empty-cart error.",
		InputSchema: schema(map[string]any{
			"cart_id":    prop("string", "ID of an existing cart, from create_cart."),
			"variant_id": prop("string", "Catalog variant ID to add. See search_products/get_product for a product's variants."),
			"quantity":   prop("integer", "Number of units to add. Defaults to 1 if omitted or non-positive."),
		}, "cart_id", "variant_id"),
		Handler: func(ctx context.Context, p json.RawMessage) (any, error) {
			return addItem(ctx, deps, p)
		},
	})

	s.Register(&Tool{
		Name:        "recommend_bundle",
		Description: "Recommend a cross-sell bundle with an expected-value score. No money moves.",
		InputSchema: schema(map[string]any{
			"cart_id":              prop("string", "Cart this recommendation is evaluated for."),
			"cart_total":           prop("integer", "Current cart subtotal, in paise."),
			"budget":               prop("integer", "Buyer's spending ceiling, in paise."),
			"tolerance":            prop("number", "Fractional budget tolerance allowed above the ceiling, e.g. 0.10 for 10%."),
			"product_id":           prop("string", "Candidate product to evaluate as a cross-sell."),
			"purchase_probability": prop("number", "Estimated probability (0..1) the buyer accepts this candidate."),
			"incremental_margin":   prop("integer", "Expected incremental gross margin if accepted, in paise."),
			"confidence":           prop("number", "Confidence (0..1) in the probability/margin estimates."),
			"risk_cost":            prop("integer", "Expected downside cost (e.g. return risk), in paise."),
		}, "product_id"),
		Handler: func(ctx context.Context, p json.RawMessage) (any, error) {
			return recommendBundle(ctx, deps, p)
		},
	})

	s.Register(&Tool{
		Name:        "calculate_total",
		Description: "Compute a cart total from a list of line items. Pure read; no side effects.",
		InputSchema: schema(map[string]any{
			"items": map[string]any{
				"type":        "array",
				"description": "Line items to total.",
				"items": schema(map[string]any{
					"unit_price": prop("integer", "Unit price in paise."),
					"quantity":   prop("integer", "Quantity."),
				}),
			},
		}, "items"),
		Handler: func(ctx context.Context, p json.RawMessage) (any, error) {
			return calculateTotal(ctx, deps, p)
		},
	})

	s.Register(&Tool{
		Name:        "request_authorization",
		Description: "Request a policy authorization for a proposed action. Returns APPROVED/REJECTED + authorization_id.",
		InputSchema: schema(map[string]any{
			"action":     prop("string", "Action being proposed, e.g. \"CREATE_ORDER\"."),
			"amount":     prop("integer", "Proposed amount, in paise."),
			"currency":   prop("string", "ISO 4217 currency code."),
			"merchant":   prop("string", "Merchant ID the action is against."),
			"items":      map[string]any{"type": "array", "description": "Product IDs in the proposed action.", "items": map[string]any{"type": "string"}},
			"mandate_id": prop("string", "ID of the buyer's mandate this action is authorized against."),
			"cart_id":    prop("string", "Cart this action is bound to."),
		}, "mandate_id"),
		Handler: func(ctx context.Context, p json.RawMessage) (any, error) {
			return requestAuthorization(ctx, deps, p)
		},
	})

	s.Register(&Tool{
		Name:        "create_checkout",
		Description: "Create a checkout order from a cart. Produces an order only — NEVER moves money. The cart must already have items in it (see add_item).",
		InputSchema: schema(map[string]any{
			"cart_id": prop("string", "Cart to check out."),
		}, "cart_id"),
		Handler: func(ctx context.Context, p json.RawMessage) (any, error) {
			return createCheckout(ctx, deps, p)
		},
	})

	s.Register(&Tool{
		Name:        "execute_authorized_checkout",
		Description: "Execute an authorized payment for an order using a valid authorization_id from request_authorization. The backend re-verifies the authorization before any money movement.",
		InputSchema: schema(map[string]any{
			"order_id":         prop("string", "Order to pay for, from create_checkout."),
			"authorization_id": prop("string", "authorization_id returned by request_authorization."),
		}, "order_id", "authorization_id"),
		Handler: func(ctx context.Context, p json.RawMessage) (any, error) {
			return executeAuthorizedCheckout(ctx, deps, p)
		},
	})

	s.Register(&Tool{
		Name:        "get_payment_status",
		Description: "Read payment status for an order. Read-only.",
		InputSchema: schema(map[string]any{
			"order_id": prop("string", "Order to check payment status for."),
		}, "order_id"),
		Handler: func(ctx context.Context, p json.RawMessage) (any, error) {
			return getPaymentStatus(ctx, deps, p)
		},
	})

	s.Register(&Tool{
		Name:        "explain_decision",
		Description: "Explain a policy decision in plain language.",
		InputSchema: schema(map[string]any{
			"failed_check": prop("string", "The policy check name that failed, e.g. \"amount_ceiling\", \"budget_tolerance\"."),
			"amount":       prop("integer", "Proposed amount, in paise."),
			"currency":     prop("string", "ISO 4217 currency code."),
			"merchant":     prop("string", "Merchant ID the action was against."),
			"items":        map[string]any{"type": "array", "description": "Product IDs in the proposed action.", "items": map[string]any{"type": "string"}},
			"max_amount":   prop("integer", "The mandate's maximum amount, in paise."),
		}, "failed_check"),
		Handler: func(ctx context.Context, p json.RawMessage) (any, error) {
			return explainDecision(ctx, deps, p)
		},
	})
}

// searchProducts is the search_products tool. It delegates to the
// shared tools.SearchProducts (backend/tools/tools.go), which every
// other product-search path in this codebase now goes through too --
// the in-app agent's BuyerAgent and its own tool-calling loop both rank
// products via the same tools.Searcher underneath (PLAN-01-AGENTIC-CORE.md
// §1, ROADMAP-PRIORITIZED.md P1 item 17) -- rather than re-implementing
// scoring here, per this file's own "tools are thin wrappers" rule. A
// caller that omits budget (or sends a non-positive one) still gets the
// full catalog: there is no sensible "budget" hard-filter for an agent
// that's still just browsing, and defaulting budget to 0 would
// incorrectly exclude every priced product.
func searchProducts(ctx context.Context, deps Dependencies, p json.RawMessage) (any, error) {
	var req struct {
		Budget    int64  `json:"budget"`
		Category  string `json:"category"`
		Priority  string `json:"priority"`
		Recipient string `json:"recipient"`
	}
	// Malformed or absent arguments are treated as "no filter", not an
	// error -- an empty {} call is exactly how a browsing agent starts.
	_ = json.Unmarshal(p, &req)

	return tools.SearchProducts(ctx, deps.toolDeps(), tools.SearchProductsRequest{
		Budget:    req.Budget,
		Category:  req.Category,
		Priority:  req.Priority,
		Recipient: req.Recipient,
	})
}

func getProduct(ctx context.Context, deps Dependencies, p json.RawMessage) (any, error) {
	var req struct {
		ProductID string `json:"product_id"`
	}
	_ = json.Unmarshal(p, &req)

	return tools.GetProduct(ctx, deps.toolDeps(), tools.GetProductRequest{ProductID: req.ProductID})
}

func createCart(ctx context.Context, deps Dependencies, p json.RawMessage) (any, error) {
	var req struct {
		ID         string `json:"cart_id"`
		MerchantID string `json:"merchant_id"`
		Currency   string `json:"currency"`
	}
	_ = json.Unmarshal(p, &req)

	return tools.CreateCart(ctx, deps.toolDeps(), tools.CreateCartRequest{
		ID:         req.ID,
		MerchantID: req.MerchantID,
		Currency:   req.Currency,
	})
}

// addItem is the add_item tool. Without this tool, an MCP-only caller
// could search_products and create_cart but never put anything INTO the
// cart before create_checkout -- there was no MCP path from "found a
// product" to "it's in the cart", which meant an agent using only the
// standardized MCP surface could never actually complete a purchase.
// The catalog remains the source of truth for product_id and price:
// cart.Service.AddItem looks both up from variant_id server-side, the
// same as the bespoke commerce HTTP API's POST /carts/{id}/items does.
func addItem(ctx context.Context, deps Dependencies, p json.RawMessage) (any, error) {
	var req struct {
		CartID    string `json:"cart_id"`
		VariantID string `json:"variant_id"`
		Quantity  int    `json:"quantity"`
	}
	_ = json.Unmarshal(p, &req)

	return tools.AddItem(ctx, deps.toolDeps(), tools.AddItemRequest{
		CartID:    req.CartID,
		VariantID: req.VariantID,
		Quantity:  req.Quantity,
	})
}

func recommendBundle(ctx context.Context, deps Dependencies, p json.RawMessage) (any, error) {
	var req struct {
		CartID       string  `json:"cart_id"`
		CartTotal    int64   `json:"cart_total"`
		Budget       int64   `json:"budget"`
		Tolerance    float64 `json:"tolerance"`
		ProductID    string  `json:"product_id"`
		PurchaseProb float64 `json:"purchase_probability"`
		Margin       int64   `json:"incremental_margin"`
		Confidence   float64 `json:"confidence"`
		RiskCost     int64   `json:"risk_cost"`
	}
	_ = json.Unmarshal(p, &req)

	return tools.RecommendBundle(ctx, deps.toolDeps(), tools.RecommendBundleRequest{
		CartID:       req.CartID,
		CartTotal:    req.CartTotal,
		Budget:       req.Budget,
		Tolerance:    req.Tolerance,
		ProductID:    req.ProductID,
		PurchaseProb: req.PurchaseProb,
		Margin:       req.Margin,
		Confidence:   req.Confidence,
		RiskCost:     req.RiskCost,
	})
}

func calculateTotal(ctx context.Context, deps Dependencies, p json.RawMessage) (any, error) {
	var req struct {
		Items []struct {
			UnitPrice int64 `json:"unit_price"`
			Quantity  int   `json:"quantity"`
		} `json:"items"`
	}
	if err := json.Unmarshal(p, &req); err != nil {
		return nil, fmt.Errorf("items required")
	}

	items := make([]tools.CalculateTotalItem, len(req.Items))
	for i, it := range req.Items {
		items[i] = tools.CalculateTotalItem{UnitPrice: it.UnitPrice, Quantity: it.Quantity}
	}

	return tools.CalculateTotal(ctx, tools.CalculateTotalRequest{Items: items})
}

func requestAuthorization(ctx context.Context, deps Dependencies, p json.RawMessage) (any, error) {
	var req struct {
		Action    string   `json:"action"`
		Amount    int64    `json:"amount"`
		Currency  string   `json:"currency"`
		Merchant  string   `json:"merchant"`
		Items     []string `json:"items"`
		MandateID string   `json:"mandate_id"`
		CartID    string   `json:"cart_id"`
	}
	if err := json.Unmarshal(p, &req); err != nil || req.MandateID == "" {
		return nil, fmt.Errorf("mandate_id required")
	}
	decision, err := deps.Policy.Propose(ctx, policy.ProposedAction{
		Action:   req.Action,
		Amount:   req.Amount,
		Currency: req.Currency,
		Merchant: req.Merchant,
		Items:    req.Items,
		CartID:   req.CartID,
	}, req.MandateID)
	if err != nil {
		return nil, err
	}
	return decision, nil
}

func createCheckout(ctx context.Context, deps Dependencies, p json.RawMessage) (any, error) {
	var req struct {
		CartID string `json:"cart_id"`
	}
	if err := json.Unmarshal(p, &req); err != nil || req.CartID == "" {
		return nil, fmt.Errorf("cart_id required")
	}
	// Produces an order only — no payment, no Razorpay call.
	return deps.Order.Checkout(ctx, req.CartID, order.NewOrderID(req.CartID))
}

// executeAuthorizedCheckout creates the payment for an order using an
// authorization_id obtained from request_authorization. The payment service
// re-verifies the authorization against the authorizations table before any
// money movement — a forged id is rejected by the policy layer.
func executeAuthorizedCheckout(ctx context.Context, deps Dependencies, p json.RawMessage) (any, error) {
	var req struct {
		OrderID         string `json:"order_id"`
		AuthorizationID string `json:"authorization_id"`
	}
	if err := json.Unmarshal(p, &req); err != nil || req.OrderID == "" || req.AuthorizationID == "" {
		return nil, fmt.Errorf("order_id and authorization_id required")
	}
	ord, err := deps.Order.GetOrder(ctx, req.OrderID)
	if err != nil {
		return nil, fmt.Errorf("order not found: %w", err)
	}
	payment, err := deps.Payment.CreatePaymentOrder(
		ctx, ord, "mcp-"+ord.ID+":"+req.AuthorizationID, req.AuthorizationID,
	)
	if err != nil {
		return nil, err
	}
	return payment, nil
}

func getPaymentStatus(ctx context.Context, deps Dependencies, p json.RawMessage) (any, error) {
	var req struct {
		OrderID string `json:"order_id"`
	}
	if err := json.Unmarshal(p, &req); err != nil || req.OrderID == "" {
		return nil, fmt.Errorf("order_id required")
	}
	pay, err := deps.Payment.GetPayment(ctx, req.OrderID)
	if err != nil {
		return nil, err
	}
	return pay, nil
}

func explainDecision(ctx context.Context, deps Dependencies, p json.RawMessage) (any, error) {
	var req struct {
		FailedCheck string   `json:"failed_check"`
		Amount      int64    `json:"amount"`
		Currency    string   `json:"currency"`
		Merchant    string   `json:"merchant"`
		Items       []string `json:"items"`
		MaxAmount   int64    `json:"max_amount"`
	}
	if err := json.Unmarshal(p, &req); err != nil {
		return nil, fmt.Errorf("failed_check required")
	}
	action := policy.ProposedAction{
		Action: "CREATE_ORDER", Amount: req.Amount, Currency: req.Currency,
		Merchant: req.Merchant, Items: req.Items,
	}
	mandate := policy.Mandate{MaximumAmount: req.MaxAmount, Currency: req.Currency}
	return map[string]any{
		"explanation": deps.Explain(action, mandate, req.FailedCheck),
	}, nil
}
