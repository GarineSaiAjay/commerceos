package review

import (
	"context"
	"errors"

	"github.com/garinesaiajay/commerceos/commerce/order"
)

var (
	// ErrInvalidRating guards the same "never trust unvalidated input"
	// posture every other write path in this codebase takes.
	ErrInvalidRating = errors.New("rating must be between 1 and 5")

	// ErrProductNotInOrder keeps VerifiedPurchase honest: a review can
	// only be filed against a product that was actually part of the
	// named order, not any product_id the caller names.
	ErrProductNotInOrder = errors.New("product was not part of this order")
)

// defaultBuyerReference is used when a buyer submits a review without a
// name -- this project has no buyer identity/auth (files/AUTH.md), so
// there is nothing else to attribute a review to.
const defaultBuyerReference = "Verified Buyer"

// OrderReader is the order surface Service needs -- just enough to
// confirm the order exists and actually contains the product being
// reviewed.
type OrderReader interface {
	GetOrder(ctx context.Context, orderID string) (order.Order, error)
}

type Service struct {
	repo   Repository
	orders OrderReader
}

func NewService(repo Repository, orders OrderReader) *Service {
	return &Service{repo: repo, orders: orders}
}

// Submit handles the post-checkout review prompt (PLAN-02-CATALOG-AND-
// COMMERCE.md §2): POST /orders/{id}/review. order_id always comes from
// a real order the buyer just completed, so every review submitted
// through this path is a verified purchase by construction -- the
// nullable-order_id, unverified starter set only ever exists via
// db/seeds/003_reviews.sql, never through this API.
func (s *Service) Submit(ctx context.Context, orderID, productID, buyerReference string, rating int, comment string) (Review, error) {
	if rating < 1 || rating > 5 {
		return Review{}, ErrInvalidRating
	}

	ord, err := s.orders.GetOrder(ctx, orderID)
	if err != nil {
		return Review{}, err
	}

	found := false
	for _, item := range ord.Items {
		if item.ProductID == productID {
			found = true
			break
		}
	}
	if !found {
		return Review{}, ErrProductNotInOrder
	}

	if buyerReference == "" {
		buyerReference = defaultBuyerReference
	}

	return s.repo.Create(ctx, Review{
		ProductID:      productID,
		OrderID:        orderID,
		BuyerReference: buyerReference,
		Rating:         rating,
		Comment:        comment,
	})
}

// ListByProduct returns every review for a product, most recent first.
func (s *Service) ListByProduct(ctx context.Context, productID string) ([]Review, error) {
	return s.repo.ListByProduct(ctx, productID)
}
