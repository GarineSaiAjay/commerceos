// Package review is PLAN-02-CATALOG-AND-COMMERCE.md §2's real (not
// synthetic) review/rating system: buyers rate a product after
// checkout, sellers get real feedback with zero manual entry, and the
// data grows for as long as a demo/judging session runs -- unlike a
// static seeded set. catalog.Product.AverageRating/ReviewCount are the
// aggregate view of this package's data (computed at read time by
// catalog.PostgresRepository.GetProduct's join); this package owns the
// individual review rows themselves.
package review

import "time"

// Review is one buyer's rating of one product. VerifiedPurchase is
// always `OrderID != ""` -- computed, never a separately-set flag that
// could drift from the truth.
type Review struct {
	ID               int64     `json:"id"`
	ProductID        string    `json:"product_id"`
	OrderID          string    `json:"order_id,omitempty"`
	BuyerReference   string    `json:"buyer_reference"`
	Rating           int       `json:"rating"`
	Comment          string    `json:"comment,omitempty"`
	VerifiedPurchase bool      `json:"verified_purchase"`
	CreatedAt        time.Time `json:"created_at"`
}
