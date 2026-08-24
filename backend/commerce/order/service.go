package order

import (
	"context"
	"fmt"
)

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{
		repo: repo,
	}
}

func (s *Service) Checkout(
	ctx context.Context,
	cartID string,
	orderID string,
) (Order, error) {
	saga := NewCheckoutSaga(s.repo)

	result, err := saga.Run(ctx, cartID, orderID)
	if err != nil {
		return Order{}, err
	}

	return result.Order, nil
}

// GetOrder reads an order by ID.
func (s *Service) GetOrder(ctx context.Context, orderID string) (Order, error) {
	return s.repo.GetOrder(ctx, orderID)
}

func NewOrderID(cartID string) string {
	return fmt.Sprintf("order_%s", cartID)
}
