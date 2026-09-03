package catalog

import (
	"context"
	"testing"
)

// fakeRepository is an in-memory Repository double. Only the methods
// Service actually calls in this test file need real behavior; the
// rest exist purely to satisfy the interface.
type fakeRepository struct {
	products        map[string]Product
	variants        map[string]ProductVariant
	listProductsErr error
	listCalls       int
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{products: map[string]Product{}, variants: map[string]ProductVariant{}}
}

func (r *fakeRepository) CreateProduct(ctx context.Context, product Product) error {
	if _, exists := r.products[product.ID]; exists {
		return ErrProductAlreadyExists
	}
	r.products[product.ID] = product
	return nil
}

func (r *fakeRepository) GetProduct(ctx context.Context, id string) (Product, error) {
	p, ok := r.products[id]
	if !ok {
		return Product{}, ErrProductNotFound
	}
	return p, nil
}

func (r *fakeRepository) ListProducts(ctx context.Context) ([]Product, error) {
	r.listCalls++
	if r.listProductsErr != nil {
		return nil, r.listProductsErr
	}
	var out []Product
	for _, p := range r.products {
		out = append(out, p)
	}
	return out, nil
}

func (r *fakeRepository) GetVariant(ctx context.Context, id string) (ProductVariant, error) {
	v, ok := r.variants[id]
	if !ok {
		return ProductVariant{}, ErrVariantNotFound
	}
	return v, nil
}

func (r *fakeRepository) ListVariantsByProduct(ctx context.Context, productID string) ([]ProductVariant, error) {
	var out []ProductVariant
	for _, v := range r.variants {
		if v.ProductID == productID {
			out = append(out, v)
		}
	}
	return out, nil
}

// CreateVariant, UpdateVariant and DeleteVariant mirror the
// products map's own CreateProduct/UpdateProduct/DeleteProduct
// behavior one level down, so tests can exercise Service's variant
// methods (including cache invalidation) without a real database.
func (r *fakeRepository) CreateVariant(ctx context.Context, variant ProductVariant) error {
	if _, exists := r.products[variant.ProductID]; !exists {
		return ErrProductNotFound
	}
	if _, exists := r.variants[variant.ID]; exists {
		return ErrVariantAlreadyExists
	}
	for _, v := range r.variants {
		if v.SKU == variant.SKU {
			return ErrVariantAlreadyExists
		}
	}
	r.variants[variant.ID] = variant
	return nil
}

func (r *fakeRepository) UpdateVariant(ctx context.Context, variant ProductVariant) error {
	if _, exists := r.variants[variant.ID]; !exists {
		return ErrVariantNotFound
	}
	for id, v := range r.variants {
		if id != variant.ID && v.SKU == variant.SKU {
			return ErrVariantAlreadyExists
		}
	}
	r.variants[variant.ID] = variant
	return nil
}

func (r *fakeRepository) DeleteVariant(ctx context.Context, id string) error {
	if _, exists := r.variants[id]; !exists {
		return ErrVariantNotFound
	}
	delete(r.variants, id)
	return nil
}

func (r *fakeRepository) UpdateProduct(ctx context.Context, product Product) error {
	if _, exists := r.products[product.ID]; !exists {
		return ErrProductNotFound
	}
	r.products[product.ID] = product
	return nil
}

func (r *fakeRepository) DeleteProduct(ctx context.Context, id string) error {
	if _, exists := r.products[id]; !exists {
		return ErrProductNotFound
	}
	delete(r.products, id)
	return nil
}

// fakeProductsCache is an in-memory ProductsCache double that also
// counts Invalidate calls, so tests can prove a mutation triggered
// invalidation without needing a real Redis instance.
type fakeProductsCache struct {
	stored           []Product
	hasValue         bool
	invalidateCalls  int
	failGet, failSet bool
}

func (c *fakeProductsCache) Get(ctx context.Context) ([]Product, bool) {
	if c.failGet || !c.hasValue {
		return nil, false
	}
	return c.stored, true
}

func (c *fakeProductsCache) Set(ctx context.Context, products []Product) {
	if c.failSet {
		return
	}
	c.stored = products
	c.hasValue = true
}

func (c *fakeProductsCache) Invalidate(ctx context.Context) {
	c.invalidateCalls++
	c.hasValue = false
	c.stored = nil
}

func testProduct(id string) Product {
	return Product{ID: id, Title: "Test Product " + id, Price: Money{Amount: 1000, Currency: "INR"}}
}

// TestListProductsPopulatesCacheOnMiss proves a cold ListProducts call
// falls through to the repository and then populates the cache, so
// the next call doesn't have to.
func TestListProductsPopulatesCacheOnMiss(t *testing.T) {
	repo := newFakeRepository()
	repo.products["p1"] = testProduct("p1")
	cache := &fakeProductsCache{}
	svc := NewService(repo).WithCache(cache)

	products, err := svc.ListProducts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(products))
	}
	if repo.listCalls != 1 {
		t.Fatalf("expected repo.ListProducts called once, got %d", repo.listCalls)
	}
	if !cache.hasValue {
		t.Fatal("expected cache to be populated after a miss")
	}
}

// TestListProductsHitsCacheOnSecondCall proves a warm cache is used
// instead of hitting the repository again -- the actual point of item
// 23: ListProducts' N+1 query pattern (see cache.go's doc comment)
// should run once, not on every request within the TTL.
func TestListProductsHitsCacheOnSecondCall(t *testing.T) {
	repo := newFakeRepository()
	repo.products["p1"] = testProduct("p1")
	cache := &fakeProductsCache{}
	svc := NewService(repo).WithCache(cache)

	if _, err := svc.ListProducts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListProducts(context.Background()); err != nil {
		t.Fatal(err)
	}

	if repo.listCalls != 1 {
		t.Fatalf("expected repo.ListProducts called exactly once across 2 ListProducts calls, got %d", repo.listCalls)
	}
}

// TestListProductsFallsBackWhenCacheUnreachable proves a cache that
// can't be read from degrades to "always hit the repository" rather
// than failing the request -- the same contract as a nil cache.
func TestListProductsFallsBackWhenCacheUnreachable(t *testing.T) {
	repo := newFakeRepository()
	repo.products["p1"] = testProduct("p1")
	cache := &fakeProductsCache{failGet: true}
	svc := NewService(repo).WithCache(cache)

	if _, err := svc.ListProducts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListProducts(context.Background()); err != nil {
		t.Fatal(err)
	}

	if repo.listCalls != 2 {
		t.Fatalf("expected repo.ListProducts called on every call when the cache can't be read, got %d", repo.listCalls)
	}
}

// TestListProductsWithoutCacheConfigured proves a Service that never
// calls WithCache behaves exactly as it did before item 23 -- every
// ListProducts call reaches the repository.
func TestListProductsWithoutCacheConfigured(t *testing.T) {
	repo := newFakeRepository()
	repo.products["p1"] = testProduct("p1")
	svc := NewService(repo)

	if _, err := svc.ListProducts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListProducts(context.Background()); err != nil {
		t.Fatal(err)
	}

	if repo.listCalls != 2 {
		t.Fatalf("expected repo.ListProducts called on every call with no cache configured, got %d", repo.listCalls)
	}
}

// TestMutationsInvalidateCache proves CreateProduct, UpdateProduct, and
// DeleteProduct each invalidate the cache on success -- a merchant's
// own catalog edit (frontend/app/dashboard/catalog/page.tsx, item 14)
// must never be masked by a stale cached list.
func TestMutationsInvalidateCache(t *testing.T) {
	repo := newFakeRepository()
	cache := &fakeProductsCache{}
	svc := NewService(repo).WithCache(cache)
	ctx := context.Background()

	if err := svc.CreateProduct(ctx, testProduct("p1")); err != nil {
		t.Fatal(err)
	}
	if cache.invalidateCalls != 1 {
		t.Fatalf("expected 1 invalidation after CreateProduct, got %d", cache.invalidateCalls)
	}

	// Warm the cache, then prove UpdateProduct invalidates it.
	if _, err := svc.ListProducts(ctx); err != nil {
		t.Fatal(err)
	}
	if !cache.hasValue {
		t.Fatal("expected cache warmed after ListProducts")
	}
	if err := svc.UpdateProduct(ctx, testProduct("p1")); err != nil {
		t.Fatal(err)
	}
	if cache.invalidateCalls != 2 {
		t.Fatalf("expected 2 invalidations after UpdateProduct, got %d", cache.invalidateCalls)
	}
	if cache.hasValue {
		t.Fatal("expected UpdateProduct to have cleared the warm cache")
	}

	if err := svc.DeleteProduct(ctx, "p1"); err != nil {
		t.Fatal(err)
	}
	if cache.invalidateCalls != 3 {
		t.Fatalf("expected 3 invalidations after DeleteProduct, got %d", cache.invalidateCalls)
	}
}

// TestFailedMutationDoesNotInvalidateCache proves a rejected mutation
// (e.g. updating a product that doesn't exist) never touches the
// cache -- only a successful write does.
func TestFailedMutationDoesNotInvalidateCache(t *testing.T) {
	repo := newFakeRepository()
	cache := &fakeProductsCache{}
	svc := NewService(repo).WithCache(cache)

	err := svc.UpdateProduct(context.Background(), testProduct("does-not-exist"))
	if err != ErrProductNotFound {
		t.Fatalf("expected ErrProductNotFound, got %v", err)
	}
	if cache.invalidateCalls != 0 {
		t.Fatalf("expected 0 invalidations after a failed update, got %d", cache.invalidateCalls)
	}
}

func testVariant(id, productID, sku string) ProductVariant {
	return ProductVariant{ID: id, ProductID: productID, SKU: sku, Price: Money{Amount: 500, Currency: "INR"}, Availability: 3}
}

// TestCreateVariantRequiresExistingProduct proves CreateVariant surfaces
// ErrProductNotFound rather than silently creating an orphaned variant
// row when variant.ProductID doesn't reference a real product -- the
// same foreign-key relationship the real PostgresRepository enforces
// at the database level (repository.go's CreateVariant doc comment).
func TestCreateVariantRequiresExistingProduct(t *testing.T) {
	repo := newFakeRepository()
	svc := NewService(repo)

	err := svc.CreateVariant(context.Background(), testVariant("v1", "does-not-exist", "sku-1"))
	if err != ErrProductNotFound {
		t.Fatalf("expected ErrProductNotFound, got %v", err)
	}
}

// TestCreateVariantRejectsDuplicateSKU proves CreateVariant surfaces
// ErrVariantAlreadyExists when the sku collides with another variant's
// -- product_variants.sku is its own UNIQUE column, independent of id.
func TestCreateVariantRejectsDuplicateSKU(t *testing.T) {
	repo := newFakeRepository()
	repo.products["p1"] = testProduct("p1")
	svc := NewService(repo)
	ctx := context.Background()

	if err := svc.CreateVariant(ctx, testVariant("v1", "p1", "shared-sku")); err != nil {
		t.Fatal(err)
	}

	err := svc.CreateVariant(ctx, testVariant("v2", "p1", "shared-sku"))
	if err != ErrVariantAlreadyExists {
		t.Fatalf("expected ErrVariantAlreadyExists, got %v", err)
	}
}

// TestUpdateVariantRequiresExistingVariant proves UpdateVariant surfaces
// ErrVariantNotFound for an id that was never created, mirroring
// UpdateProduct's ErrProductNotFound behavior one level down.
func TestUpdateVariantRequiresExistingVariant(t *testing.T) {
	repo := newFakeRepository()
	svc := NewService(repo)

	err := svc.UpdateVariant(context.Background(), testVariant("does-not-exist", "p1", "sku-1"))
	if err != ErrVariantNotFound {
		t.Fatalf("expected ErrVariantNotFound, got %v", err)
	}
}

// TestDeleteVariantRequiresExistingVariant proves DeleteVariant surfaces
// ErrVariantNotFound for an id that doesn't exist, rather than silently
// succeeding.
func TestDeleteVariantRequiresExistingVariant(t *testing.T) {
	repo := newFakeRepository()
	svc := NewService(repo)

	err := svc.DeleteVariant(context.Background(), "does-not-exist")
	if err != ErrVariantNotFound {
		t.Fatalf("expected ErrVariantNotFound, got %v", err)
	}
}

// TestVariantMutationsInvalidateCache proves CreateVariant, UpdateVariant
// and DeleteVariant each invalidate the products cache on success --
// GetProduct's embedded Variants field (which the cached ListProducts
// response carries per product) must never go stale after a variant
// edit, the same correctness requirement TestMutationsInvalidateCache
// proves for the product-level mutations.
func TestVariantMutationsInvalidateCache(t *testing.T) {
	repo := newFakeRepository()
	repo.products["p1"] = testProduct("p1")
	cache := &fakeProductsCache{}
	svc := NewService(repo).WithCache(cache)
	ctx := context.Background()

	if err := svc.CreateVariant(ctx, testVariant("v1", "p1", "sku-1")); err != nil {
		t.Fatal(err)
	}
	if cache.invalidateCalls != 1 {
		t.Fatalf("expected 1 invalidation after CreateVariant, got %d", cache.invalidateCalls)
	}

	// Warm the cache, then prove UpdateVariant invalidates it.
	if _, err := svc.ListProducts(ctx); err != nil {
		t.Fatal(err)
	}
	if !cache.hasValue {
		t.Fatal("expected cache warmed after ListProducts")
	}
	if err := svc.UpdateVariant(ctx, testVariant("v1", "p1", "sku-1-updated")); err != nil {
		t.Fatal(err)
	}
	if cache.invalidateCalls != 2 {
		t.Fatalf("expected 2 invalidations after UpdateVariant, got %d", cache.invalidateCalls)
	}
	if cache.hasValue {
		t.Fatal("expected UpdateVariant to have cleared the warm cache")
	}

	if err := svc.DeleteVariant(ctx, "v1"); err != nil {
		t.Fatal(err)
	}
	if cache.invalidateCalls != 3 {
		t.Fatalf("expected 3 invalidations after DeleteVariant, got %d", cache.invalidateCalls)
	}
}

// TestFailedVariantMutationDoesNotInvalidateCache proves a rejected
// variant mutation never touches the cache, mirroring
// TestFailedMutationDoesNotInvalidateCache for the product-level case.
func TestFailedVariantMutationDoesNotInvalidateCache(t *testing.T) {
	repo := newFakeRepository()
	cache := &fakeProductsCache{}
	svc := NewService(repo).WithCache(cache)

	err := svc.UpdateVariant(context.Background(), testVariant("does-not-exist", "p1", "sku-1"))
	if err != ErrVariantNotFound {
		t.Fatalf("expected ErrVariantNotFound, got %v", err)
	}
	if cache.invalidateCalls != 0 {
		t.Fatalf("expected 0 invalidations after a failed variant update, got %d", cache.invalidateCalls)
	}
}

// TestListVariantsByProductFiltersByProduct proves ListVariantsByProduct
// only returns variants belonging to the requested product, not every
// variant in the repository.
func TestListVariantsByProductFiltersByProduct(t *testing.T) {
	repo := newFakeRepository()
	repo.products["p1"] = testProduct("p1")
	repo.products["p2"] = testProduct("p2")
	svc := NewService(repo)
	ctx := context.Background()

	if err := svc.CreateVariant(ctx, testVariant("v1", "p1", "sku-1")); err != nil {
		t.Fatal(err)
	}
	if err := svc.CreateVariant(ctx, testVariant("v2", "p2", "sku-2")); err != nil {
		t.Fatal(err)
	}

	variants, err := svc.ListVariantsByProduct(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(variants) != 1 || variants[0].ID != "v1" {
		t.Fatalf("expected exactly [v1] for product p1, got %+v", variants)
	}
}
