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
}

type OrderItem struct {
	ProductID string `json:"product_id"`
	VariantID string `json:"variant_id"`
	Title     string `json:"title"`
	Quantity  int    `json:"quantity"`
	UnitPrice int64  `json:"unit_price"`
	Total     int64  `json:"total"`
}
