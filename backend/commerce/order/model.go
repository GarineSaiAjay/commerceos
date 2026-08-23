package order

import "time"

type Order struct {
	ID         string      `json:"order_id"`
	MerchantID string      `json:"merchant_id"`
	CartID     string      `json:"cart_id"`
	Currency   string      `json:"currency"`
	Subtotal   int64       `json:"subtotal"`
	Status     string      `json:"status"`
	Items      []OrderItem `json:"items"`
	CreatedAt  time.Time   `json:"created_at"`
}

type OrderItem struct {
	ProductID string `json:"product_id"`
	VariantID string `json:"variant_id"`
	Title     string `json:"title"`
	Quantity  int    `json:"quantity"`
	UnitPrice int64  `json:"unit_price"`
	Total     int64  `json:"total"`
}
