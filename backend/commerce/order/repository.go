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

	// ListOrders returns every order placed with the given merchant,
	// most recent first. There is no buyer identity yet (see
	// files/JUDGE-FACING-GAPS.md P0.3), so this is merchant-scoped
	// rather than buyer-scoped -- the right scope once auth lands is a
	// further WHERE buyer_id = $2 on the same query.
	ListOrders(
		ctx context.Context,
		merchantID string,
	) ([]Order, error)

	// TransitionStatus atomically moves the order from its current
	// status to `to`, guarded by the centralized state machine. It
	// returns ErrIllegalTransition if the edge is not allowed.
	TransitionStatus(
		ctx context.Context,
		orderID string,
		to string,
	) (Order, error)
}
