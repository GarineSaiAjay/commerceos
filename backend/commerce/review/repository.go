package review

import "context"

// Repository persists and reads back reviews. Implemented by
// *PostgresRepository below; a test fake only needs to satisfy this,
// same convention as cart.Repository/order.Repository.
type Repository interface {
	// Create inserts a review and returns it with its generated ID,
	// CreatedAt, and computed VerifiedPurchase filled in.
	Create(ctx context.Context, r Review) (Review, error)

	// ListByProduct returns every review for a product, most recent
	// first.
	ListByProduct(ctx context.Context, productID string) ([]Review, error)
}
