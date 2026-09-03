package order

import "time"

type Order struct {
	ID         string `json:"order_id"`
	MerchantID string `json:"merchant_id"`
	CartID     string `json:"cart_id"`
	Currency   string `json:"currency"`
	// Subtotal is the amount actually charged -- already net of
	// DiscountAmount, if a campaign discount was applied at checkout
	// (see PostgresRepository.CheckoutCart). It is never the pre-discount
	// cart total; callers that want that can add DiscountAmount back.
	Subtotal int64 `json:"subtotal"`
	// DiscountAmount and CampaignID are zero/empty unless an ACTIVE
	// campaign matched an item in this cart at checkout time (paise,
	// like every other amount in this codebase).
	DiscountAmount int64       `json:"discount_amount"`
	CampaignID     string      `json:"campaign_id,omitempty"`
	Status         string      `json:"status"`
	Items          []OrderItem `json:"items"`
	CreatedAt      time.Time   `json:"created_at"`
	// PaymentStatus is the linked payment record's status
	// (payments.status: created/attempted/paid/failed/refunded), empty
	// if no payment has been initiated for this order yet. Populated via
	// a LEFT JOIN in GetOrder/ListOrders (postgres_repository.go) --
	// added for the merchant dashboard's Orders page (item 15,
	// PLAN-05-SELLER-DASHBOARD.md §2's "status, payment status" list
	// column). The existing buyer-facing ListOrders/GetOrder callers
	// (checkout.tsx's own order history) simply get an extra field they
	// don't render.
	PaymentStatus string `json:"payment_status,omitempty"`
	// RunID is the agent_actions.id (the run_id GET /runs/{id} takes)
	// whose proposal was authorized and consumed to pay for this order
	// -- empty until CreatePaymentOrder tags it (see
	// payment.Service.CreatePaymentOrder's doc comment), and permanently
	// empty for a draft order that never reached a successful payment.
	// PLAN-05-SELLER-DASHBOARD.md §2's "Orders -> Runs audit-trail
	// link": the dashboard Orders page uses this to deep-link to
	// /dashboard/runs?run_id=... instead of the generic "browse Agent
	// Runs" fallback it showed before.
	RunID string `json:"run_id,omitempty"`
}

type OrderItem struct {
	ProductID string `json:"product_id"`
	VariantID string `json:"variant_id"`
	Title     string `json:"title"`
	Quantity  int    `json:"quantity"`
	UnitPrice int64  `json:"unit_price"`
	Total     int64  `json:"total"`
}
