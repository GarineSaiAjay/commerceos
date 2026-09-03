package catalog

import (
	"context"
	"errors"
)

var ErrVariantNotFound = errors.New("variant not found")

type Service struct {
	repo  Repository
	cache ProductsCache
}

func NewService(repo Repository) *Service {
	return &Service{
		repo: repo,
	}
}

// WithCache attaches a short-TTL cache for ListProducts (item 23,
// PLAN-04-UI-UX-AND-LATENCY.md §B2), returning the same Service for
// chaining at construction time -- the same fluent-setter convention
// as payment.Handler.WithCallCounter / growth.SuggestHandler.
// WithImpressions. Optional and additive: a Service that never calls
// WithCache (cache stays nil) behaves identically to before this
// item -- ListProducts always reads through to the repository.
func (s *Service) WithCache(cache ProductsCache) *Service {
	s.cache = cache
	return s
}

func (s *Service) CreateProduct(ctx context.Context, product Product) error {
	if err := s.repo.CreateProduct(ctx, product); err != nil {
		return err
	}
	s.invalidateCache(ctx)
	return nil
}

func (s *Service) GetProduct(ctx context.Context, id string) (Product, error) {
	return s.repo.GetProduct(ctx, id)
}

// ListProducts reads through s.cache when one is configured: a cache
// hit skips the repository (and the N+1 query pattern
// PostgresRepository.ListProducts runs -- see cache.go's doc comment)
// entirely; a miss falls through to the repository and populates the
// cache for the next call.
func (s *Service) ListProducts(ctx context.Context) ([]Product, error) {
	if s.cache != nil {
		if products, ok := s.cache.Get(ctx); ok {
			return products, nil
		}
	}

	products, err := s.repo.ListProducts(ctx)
	if err != nil {
		return nil, err
	}

	if s.cache != nil {
		s.cache.Set(ctx, products)
	}

	return products, nil
}

// invalidateCache is a no-op when no cache is configured -- called
// after every successful mutation so a merchant's own edit is never
// masked by a stale cached list (see ProductsCache.Invalidate's doc
// comment).
func (s *Service) invalidateCache(ctx context.Context) {
	if s.cache != nil {
		s.cache.Invalidate(ctx)
	}
}

func (s *Service) GetVariant(ctx context.Context, id string) (ProductVariant, error) {
	return s.repo.GetVariant(ctx, id)
}

// ListVariantsByProduct returns every variant of one product. Exposed
// mainly for a future dashboard variant sub-editor (PLAN-05-SELLER-
// DASHBOARD.md §1's "once PLAN-02 §1 ships real variants" line, not
// built in item 14) -- GetProduct already returns Variants inline for
// the buyer catalog picker, so most callers never need this directly.
func (s *Service) ListVariantsByProduct(ctx context.Context, productID string) ([]ProductVariant, error) {
	return s.repo.ListVariantsByProduct(ctx, productID)
}

// CreateVariant adds a new variant to an existing product, then
// invalidates the products cache -- GetProduct's Variants field (which
// the cached ListProducts response embeds per product) would otherwise
// miss a newly-added variant until the cache's own short TTL naturally
// expires, same correctness requirement CreateProduct/UpdateProduct/
// DeleteProduct already enforce for their own edits.
func (s *Service) CreateVariant(ctx context.Context, variant ProductVariant) error {
	if err := s.repo.CreateVariant(ctx, variant); err != nil {
		return err
	}
	s.invalidateCache(ctx)
	return nil
}

// UpdateVariant replaces an existing variant's editable fields, then
// invalidates the products cache for the same reason CreateVariant does.
func (s *Service) UpdateVariant(ctx context.Context, variant ProductVariant) error {
	if err := s.repo.UpdateVariant(ctx, variant); err != nil {
		return err
	}
	s.invalidateCache(ctx)
	return nil
}

// DeleteVariant removes a variant, then invalidates the products cache
// for the same reason CreateVariant does.
func (s *Service) DeleteVariant(ctx context.Context, id string) error {
	if err := s.repo.DeleteVariant(ctx, id); err != nil {
		return err
	}
	s.invalidateCache(ctx)
	return nil
}

// UpdateProduct replaces the editable fields of an existing product.
func (s *Service) UpdateProduct(ctx context.Context, product Product) error {
	if err := s.repo.UpdateProduct(ctx, product); err != nil {
		return err
	}
	s.invalidateCache(ctx)
	return nil
}

// DeleteProduct removes a product, refusing when it is still referenced by
// an existing cart (ErrProductInUse) or when it doesn't exist
// (ErrProductNotFound).
func (s *Service) DeleteProduct(ctx context.Context, id string) error {
	if err := s.repo.DeleteProduct(ctx, id); err != nil {
		return err
	}
	s.invalidateCache(ctx)
	return nil
}
