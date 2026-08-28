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

// UpdateProduct replaces the editable fields of an existing product.
func (s *Service) UpdateProduct(ctx context.Context, product Product) error {
	return s.repo.UpdateProduct(ctx, product)
}

// DeleteProduct removes a product, refusing when it is still referenced by
// an existing cart (ErrProductInUse) or when it doesn't exist
// (ErrProductNotFound).
func (s *Service) DeleteProduct(ctx context.Context, id string) error {
	return s.repo.DeleteProduct(ctx, id)
}
