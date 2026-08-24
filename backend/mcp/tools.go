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

// RegisterTools wires the 9 narrow tools into the server.
func RegisterTools(s *Server, deps Dependencies) {
	s.Register(&Tool{
		Name:        "search_products",
		Description: "Search the catalog by budget and priority. Deterministic; never bypasses budget/availability.",
		InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, p json.RawMessage) (any, error) {
			return listProducts(ctx, deps)
		},
	})

	s.Register(&Tool{
		Name:        "get_product",
		Description: "Get one product by ID.",
		InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, p json.RawMessage) (any, error) {
			return getProduct(ctx, deps, p)
		},
	})

	s.Register(&Tool{
		Name:        "create_cart",
		Description: "Create a cart. No money moves.",
		InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, p json.RawMessage) (any, error) {
			return createCart(ctx, deps, p)
		},
	})

	s.Register(&Tool{
		Name:        "recommend_bundle",
		Description: "Recommend a cross-sell bundle with an expected-value score. No money moves.",
		InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, p json.RawMessage) (any, error) {
			return recommendBundle(ctx, deps, p)
		},
	})

	s.Register(&Tool{
		Name:        "calculate_total",
		Description: "Compute a cart total. Pure read.",
		InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, p json.RawMessage) (any, error) {
			return calculateTotal(ctx, deps, p)
		},
	})

	s.Register(&Tool{
		Name:        "request_authorization",
		Description: "Request a policy authorization for a proposed action. Returns APPROVED/REJECTED + authorization_id.",
		InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, p json.RawMessage) (any, error) {
			return requestAuthorization(ctx, deps, p)
		},
	})

	s.Register(&Tool{
		Name:        "create_checkout",
		Description: "Create a checkout order from a cart. Produces an order only — NEVER moves money.",
		InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, p json.RawMessage) (any, error) {
			return createCheckout(ctx, deps, p)
		},
	})

	s.Register(&Tool{
		Name:        "execute_authorized_checkout",
		Description: "Execute an authorized payment for an order using a valid authorization_id from request_authorization. The backend re-verifies the authorization before any money movement.",
		InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, p json.RawMessage) (any, error) {
			return executeAuthorizedCheckout(ctx, deps, p)
		},
	})

	s.Register(&Tool{
		Name:        "get_payment_status",
		Description: "Read payment status for an order. Read-only.",
		InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, p json.RawMessage) (any, error) {
			return getPaymentStatus(ctx, deps, p)
		},
	})

	s.Register(&Tool{
		Name:        "explain_decision",
		Description: "Explain a policy decision in plain language.",
		InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, p json.RawMessage) (any, error) {
			return explainDecision(ctx, deps, p)
		},
	})
}

func listProducts(ctx context.Context, deps Dependencies) (any, error) {
	products, err := deps.Catalog.ListProducts(ctx)
	if err != nil {
		return nil, err
	}
	return products, nil
}

func getProduct(ctx context.Context, deps Dependencies, p json.RawMessage) (any, error) {
	var req struct {
		ProductID string `json:"product_id"`
	}
	if err := json.Unmarshal(p, &req); err != nil || req.ProductID == "" {
		return nil, fmt.Errorf("product_id required")
	}
	product, err := deps.Catalog.GetProduct(ctx, req.ProductID)
	if err != nil {
		return nil, err
	}
	return product, nil
}

func createCart(ctx context.Context, deps Dependencies, p json.RawMessage) (any, error) {
	var req struct {
		ID         string `json:"cart_id"`
		MerchantID string `json:"merchant_id"`
		Currency   string `json:"currency"`
	}
	if err := json.Unmarshal(p, &req); err != nil || req.ID == "" {
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
	if err := json.Unmarshal(p, &req); err != nil || req.ProductID == "" {
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
	var total int64
	for _, it := range req.Items {
		total += it.UnitPrice * int64(it.Quantity)
	}
	return map[string]any{"total": total}, nil
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
