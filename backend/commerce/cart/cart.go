package cart

import "time"

type Cart struct {
	ID         string     `json:"cart_id"`
	MerchantID string     `json:"merchant_id"`
	Items      []CartItem `json:"items"`
	Subtotal   int64      `json:"subtotal"`
	Currency   string     `json:"currency"`
	ExpiresAt  time.Time  `json:"expires_at"`
}

type CartItem struct {
	ProductID string `json:"product_id"`
	VariantID string `json:"variant_id"`
	Title     string `json:"title"`
	Quantity  int    `json:"quantity"`
	UnitPrice int64  `json:"unit_price"`
	Total     int64  `json:"total"`
}
