package order

import "context"

type Repository interface {
	CheckoutCart(
		ctx context.Context,
		cartID string,
		orderID string,
	) (Order, error)

	GetOrder(
		ctx context.Context,
		orderID string,
	) (Order, error)

	// TransitionStatus atomically moves the order from its current
	// status to `to`, guarded by the centralized state machine. It
	// returns ErrIllegalTransition if the edge is not allowed.
	TransitionStatus(
		ctx context.Context,
		orderID string,
		to string,
	) (Order, error)
}
