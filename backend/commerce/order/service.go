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
	order, err := s.repo.CheckoutCart(ctx, cartID, orderID)
	if err != nil {
		return Order{}, err
	}

	return order, nil
}

func NewOrderID(cartID string) string {
	return fmt.Sprintf("order_%s", cartID)
}
