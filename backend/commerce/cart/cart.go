package cart

import "time"

type Cart struct {
	ID         string     `json:"cart_id"`
	MerchantID string     `json:"merchant_id"`
	Items      []CartItem `json:"items"`
	Subtotal   int64      `json:"subtotal"`
	Currency   string     `json:"currency"`
	// Status is "active" or "checked_out" (set by the order checkout
	// saga -- see order/postgres_repository.go CheckoutCart). A
	// checked-out cart is single-use and must never be resumed for
	// further shopping; Service.GetCart treats one as not-found.
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
	// Version is an optimistic-concurrency counter (full-codebase
	// re-audit, P2): PostgresRepository.SaveCart conditions its UPDATE
	// on the version last read via GetCart and increments it on every
	// successful save, so two concurrent read-modify-write cycles
	// against the same cart (see Service.AddItem/UpdateItemQuantity/
	// RemoveItem) can no longer silently clobber one another -- the
	// second writer's SaveCart affects zero rows and Service retries
	// instead. Internal plumbing, not buyer-facing data -- excluded
	// from the JSON wire shape.
	Version int `json:"-"`
}

type CartItem struct {
	ProductID string `json:"product_id"`
	VariantID string `json:"variant_id"`
	Title     string `json:"title"`
	Quantity  int    `json:"quantity"`
	UnitPrice int64  `json:"unit_price"`
	Total     int64  `json:"total"`
}
