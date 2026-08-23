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
}
