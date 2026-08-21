package catalog

import (
	"context"
	"errors"
)

var ErrVariantNotFound = errors.New("variant not found")

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{
		repo: repo,
	}
}

func (s *Service) CreateProduct(ctx context.Context, product Product) error {
	return s.repo.CreateProduct(ctx, product)
}

func (s *Service) GetProduct(ctx context.Context, id string) (Product, error) {
	return s.repo.GetProduct(ctx, id)
}

func (s *Service) ListProducts(ctx context.Context) ([]Product, error) {
	return s.repo.ListProducts(ctx)
}

func (s *Service) GetVariant(ctx context.Context, id string) (ProductVariant, error) {
	return s.repo.GetVariant(ctx, id)
}
