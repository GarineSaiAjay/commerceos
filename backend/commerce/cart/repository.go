package cart

import "context"

type Repository interface {
	CreateCart(ctx context.Context, cart Cart) error
	GetCart(ctx context.Context, id string) (Cart, error)
	SaveCart(ctx context.Context, cart Cart) error
}
