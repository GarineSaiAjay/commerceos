package review

import (
	"context"
	"errors"

	"github.com/garinesaiajay/commerceos/commerce/order"
	"github.com/garinesaiajay/commerceos/statemachine"
)

var (
	// ErrInvalidRating guards the same "never trust unvalidated input"
	// posture every other write path in this codebase takes.
	ErrInvalidRating = errors.New("rating must be between 1 and 5")

	// ErrProductNotInOrder keeps VerifiedPurchase honest: a review can
	// only be filed against a product that was actually part of the
	// named order, not any product_id the caller names.
	ErrProductNotInOrder = errors.New("product was not part of this order")

	// ErrOrderNotEligibleForReview closes a gap a fresh audit against
	// PLAN-02-CATALOG-AND-COMMERCE.md found: Submit checked that the
	// order existed and contained the product, but never checked the
	// order actually reached a paid state -- so an order still in
	// DRAFT/AUTHORIZED/PAYMENT_PENDING, or one that ended in FAILED/
	// CANCELLED, could still have a review filed against it, complete
	// with VerifiedPurchase implicitly true just because order_id was
	// non-empty. Not reachable through the normal UI (checkout.tsx only
	// shows the review form after payment/verify succeeds -- see
	// usePaymentFlow.ts's verifyPayment), but unenforced server-side,
	// which is what actually matters for "verified purchase" to mean
	// what this codebase's own doc comments say it means.
	ErrOrderNotEligibleForReview = errors.New("order has not completed payment")

	// ErrDuplicateReview guards against a retried or duplicated
	// POST /orders/{id}/review call inflating a product's
	// average_rating/review_count with more than one review from the
	// same real purchase -- see db/migrations/*_add_reviews_order_
	// product_unique.sql, the reviews_order_product_unique constraint
	// this maps from (PostgresRepository.Create translates the
	// resulting unique-violation into this error).
	ErrDuplicateReview = errors.New("a review already exists for this order and product")
)

// reviewEligibleOrderStatuses are the order.Status values a review may
// be filed against -- everything from the point payment succeeds
// onward (statemachine.go's OrderPaid -> OrderFulfillmentPending ->
// OrderCompleted), deliberately excluding OrderDraft/OrderAuthorized/
// OrderPaymentPending (payment not yet confirmed) and OrderFailed/
// OrderCancelled (payment never completed).
var reviewEligibleOrderStatuses = map[string]bool{
	statemachine.OrderPaid:               true,
	statemachine.OrderFulfillmentPending: true,
	statemachine.OrderCompleted:          true,
}

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

	if !reviewEligibleOrderStatuses[ord.Status] {
		return Review{}, ErrOrderNotEligibleForReview
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
