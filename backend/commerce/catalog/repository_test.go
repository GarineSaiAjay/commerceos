package catalog

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresRepositoryGetProduct(t *testing.T) {
	ctx := context.Background()

	pool, err := pgxpool.New(
		ctx,
		"postgres://commerceos:commerceos_dev_password@localhost:5433/commerceos?sslmode=disable",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	repo := NewPostgresRepository(pool)

	product, err := repo.GetProduct(ctx, "airpods-pro-2")
	if err != nil {
		t.Fatal(err)
	}

	if product.ID != "airpods-pro-2" {
		t.Fatalf("expected product ID %q, got %q", "airpods-pro-2", product.ID)
	}

	if product.Title != "AirPods Pro" {
		t.Fatalf("expected title %q, got %q", "AirPods Pro", product.Title)
	}

	// Amounts are paise (README.md, db/migrations/20260822160000_normalize_money_to_paise.sql):
	// AirPods Pro is seeded at ₹24,900 = 2490000 paise.
	if product.Price.Amount != 2490000 {
		t.Fatalf("expected price 2490000, got %d", product.Price.Amount)
	}

	if product.Price.Currency != "INR" {
		t.Fatalf("expected currency INR, got %s", product.Price.Currency)
	}

	// Availability is decremented by checkout, so it may be less than
	// the seeded value of 12. Assert it is a valid non-negative value
	// that never exceeds the seed.
	if product.Availability < 0 || product.Availability > 12 {
		t.Fatalf(
			"expected availability in [0,12], got %d",
			product.Availability,
		)
	}
}

// TestPostgresRepositoryCreateProductProvisionsDefaultVariant proves
// the fix for a real reported bug: a product created through
// CreateProduct (e.g. via frontend/app/dashboard/catalog/page.tsx,
// item 14) must be immediately addable to a cart, which requires a
// "<product_id>-default" variant to exist -- checkout.tsx's addToCart
// hardcodes exactly that ID. Before this fix, CreateProduct only wrote
// to products, leaving every dashboard-created product with zero
// variants and a silently-failing "Add to cart" button.
func TestPostgresRepositoryCreateProductProvisionsDefaultVariant(t *testing.T) {
	ctx := context.Background()

	pool, err := pgxpool.New(
		ctx,
		"postgres://commerceos:commerceos_dev_password@localhost:5433/commerceos?sslmode=disable",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	repo := NewPostgresRepository(pool)

	productID := "default-variant-test-product"
	_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, productID)

	product := Product{
		ID:           productID,
		Title:        "Default Variant Test Product",
		Price:        Money{Amount: 70000, Currency: "INR"},
		Availability: 9,
		Merchant:     MerchantRef{ID: "merchant_001"},
		ReturnPolicy: ReturnPolicy{Days: 7},
		Shipping:     Shipping{EstimatedDays: 3},
	}
	if err := repo.CreateProduct(ctx, product); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, productID)
	}()

	variant, err := repo.GetVariant(ctx, productID+"-default")
	if err != nil {
		t.Fatalf("expected a default variant to exist, got: %v", err)
	}
	if variant.ProductID != productID {
		t.Fatalf("expected variant.ProductID %q, got %q", productID, variant.ProductID)
	}
	if variant.Price.Amount != product.Price.Amount {
		t.Fatalf("expected variant price %d (mirroring the product), got %d", product.Price.Amount, variant.Price.Amount)
	}
	if variant.Availability != product.Availability {
		t.Fatalf("expected variant availability %d (mirroring the product), got %d", product.Availability, variant.Availability)
	}
}

// TestPostgresRepositoryGetProductVariantsAreDifferentiated proves item
// 10 (PLAN-02-CATALOG-AND-COMMERCE.md §1): "airpods-case" is seeded
// with three variants (its original "-default"/black plus new
// "-white"/"-starlight" colorways, db/seeds/001_catalog.sql), and
// GetProduct's Variants field must surface all three with their OWN
// price/availability -- not the parent product's, which is exactly the
// bug GetVariant/ListVariantsByProduct's pv.availability fix (see the
// doc comment on GetVariant) exists to prevent. White and starlight are
// seeded at different availabilities (16 vs 9) specifically so this
// test can catch a regression back to "every variant reports the same
// number" the way the pre-fix p.availability bug would have produced.
func TestPostgresRepositoryGetProductVariantsAreDifferentiated(t *testing.T) {
	ctx := context.Background()

	pool, err := pgxpool.New(
		ctx,
		"postgres://commerceos:commerceos_dev_password@localhost:5433/commerceos?sslmode=disable",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	repo := NewPostgresRepository(pool)

	product, err := repo.GetProduct(ctx, "airpods-case")
	if err != nil {
		t.Fatal(err)
	}

	byID := make(map[string]ProductVariant, len(product.Variants))
	for _, v := range product.Variants {
		byID[v.ID] = v
	}

	if len(product.Variants) < 3 {
		t.Fatalf("expected at least 3 variants for airpods-case (default/white/starlight), got %d", len(product.Variants))
	}

	white, ok := byID["airpods-case-white"]
	if !ok {
		t.Fatal("expected an airpods-case-white variant")
	}
	if white.Price.Amount != 199900 {
		t.Fatalf("expected airpods-case-white price 199900, got %d", white.Price.Amount)
	}
	// Seeded at 16, only ever decremented by a real checkout -- never
	// exceeds the seed, matching the tolerance pattern used by
	// TestPostgresRepositoryGetProduct above.
	if white.Availability < 0 || white.Availability > 16 {
		t.Fatalf("expected airpods-case-white availability in [0,16], got %d", white.Availability)
	}
	if white.Attributes["color"] != "white" {
		t.Fatalf("expected airpods-case-white attributes.color = white, got %v", white.Attributes["color"])
	}

	starlight, ok := byID["airpods-case-starlight"]
	if !ok {
		t.Fatal("expected an airpods-case-starlight variant")
	}
	if starlight.Availability < 0 || starlight.Availability > 9 {
		t.Fatalf("expected airpods-case-starlight availability in [0,9], got %d", starlight.Availability)
	}

	// The two colorways must never report the same availability number
	// by coincidence of both reading the parent product's stock instead
	// of their own -- that's precisely the bug this test guards against.
	if white.Availability == starlight.Availability {
		t.Fatalf(
			"white and starlight availability both read %d -- likely regressed to selecting the parent product's availability instead of each variant's own (see GetVariant's doc comment)",
			white.Availability,
		)
	}

	// ListVariantsByProduct is the same query path GetProduct uses
	// internally -- assert it's independently callable and consistent.
	variants, err := repo.ListVariantsByProduct(ctx, "airpods-case")
	if err != nil {
		t.Fatal(err)
	}
	if len(variants) != len(product.Variants) {
		t.Fatalf("expected ListVariantsByProduct to return %d variants (matching GetProduct), got %d", len(product.Variants), len(variants))
	}
}

// TestPostgresRepositoryGetProductIncludesRatingAggregate proves
// GetProduct's rating join (PLAN-02-CATALOG-AND-COMMERCE.md §2, item
// 11) both computes a real average/count from the reviews table and
// never returns NULL/zero-value-that-looks-like-a-real-zero for an
// unrated product's aggregate (it's a genuine 0, not a masked NULL).
func TestPostgresRepositoryGetProductIncludesRatingAggregate(t *testing.T) {
	ctx := context.Background()

	pool, err := pgxpool.New(
		ctx,
		"postgres://commerceos:commerceos_dev_password@localhost:5433/commerceos?sslmode=disable",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	repo := NewPostgresRepository(pool)

	productID := "rating-test-product"
	_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, productID)

	product := Product{
		ID:           productID,
		Title:        "Rating Test Product",
		Price:        Money{Amount: 5000, Currency: "INR"},
		Availability: 3,
		Merchant:     MerchantRef{ID: "merchant_001"},
		ReturnPolicy: ReturnPolicy{Days: 7},
		Shipping:     Shipping{EstimatedDays: 3},
	}
	if err := repo.CreateProduct(ctx, product); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, productID)
	}()

	// No reviews yet: average_rating 0, review_count 0.
	fetched, err := repo.GetProduct(ctx, productID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.AverageRating != 0 || fetched.ReviewCount != 0 {
		t.Fatalf("expected 0/0 for an unrated product, got %v/%d", fetched.AverageRating, fetched.ReviewCount)
	}

	// Seed three reviews directly (order_id NULL, same shape
	// db/seeds/003_reviews.sql uses for its starter set) -- average
	// (5+3+4)/3 = 4.
	_, err = pool.Exec(ctx, `
		INSERT INTO reviews (product_id, order_id, buyer_reference, rating, comment)
		VALUES
			($1, NULL, 'Test Buyer A', 5, ''),
			($1, NULL, 'Test Buyer B', 3, ''),
			($1, NULL, 'Test Buyer C', 4, '')
	`, productID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM reviews WHERE product_id = $1`, productID)
	}()

	fetched, err = repo.GetProduct(ctx, productID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.ReviewCount != 3 {
		t.Fatalf("expected review_count 3, got %d", fetched.ReviewCount)
	}
	if fetched.AverageRating != 4 {
		t.Fatalf("expected average_rating 4, got %v", fetched.AverageRating)
	}
}

func TestPostgresRepositoryListProducts(t *testing.T) {
	ctx := context.Background()

	pool, err := pgxpool.New(
		ctx,
		"postgres://commerceos:commerceos_dev_password@localhost:5433/commerceos?sslmode=disable",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	repo := NewPostgresRepository(pool)

	products, err := repo.ListProducts(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// db/seeds/001_catalog.sql seeds 13 products (the original 10 as of
	// commit d30f155, "fix(catalog): tag products so agent matching can
	// score, and add five more", 2026-08-28, plus airpods-pro-3,
	// airtag-4pack, and beats-fit-pro added afterward). This assertion
	// used to be an exact `!= 13` check and went stale twice for the
	// same reason (a hardcoded product count with no link back to the
	// seed file). Since ROADMAP-PRIORITIZED.md item 14
	// (frontend/app/dashboard/catalog/page.tsx) shipped, exact equality
	// is now permanently wrong, not just occasionally stale: a merchant
	// running against this same dev database can add real products
	// through the dashboard, and a real report against this branch did
	// exactly that ("refrigerator magnets") -- this test failing on
	// their machine was this exact assertion working as badly as
	// designed, not a regression. `>= 13` keeps the one guarantee this
	// test can actually make (the seed data loaded) without asserting
	// something dashboard-added products necessarily break.
	if len(products) < 13 {
		t.Fatalf("expected at least the 13 seeded products, got %d", len(products))
	}
}

// TestProductSchemaRoundTrip proves the Phase 1 requirement (section 2.2.4):
// a product with nested features/attributes/purchase_constraints must
// round-trip exactly (create → fetch → deep equality).
func TestProductSchemaRoundTrip(t *testing.T) {
	ctx := context.Background()

	pool, err := pgxpool.New(
		ctx,
		"postgres://commerceos:commerceos_dev_password@localhost:5433/commerceos?sslmode=disable",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	repo := NewPostgresRepository(pool)

	productID := "roundtrip-test-product"

	// Clean up in case the test was run before.
	_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, productID)

	product := Product{
		ID:            productID,
		Title:         "Round-Trip Test Product",
		Price:         Money{Amount: 12345, Currency: "INR"},
		Availability:  7,
		Features:      []string{"alpha", "beta"},
		Compatibility: []string{"ios", "android"},
		UseCases:      []string{"travel", "music"},
		Merchant:      MerchantRef{ID: "merchant_001"},
		ReturnPolicy:  ReturnPolicy{Days: 14},
		Shipping:      Shipping{EstimatedDays: 5},
		Attributes: map[string]any{
			"color":         "midnight",
			"battery_hours": 40,
			"wireless":      true,
		},
		PurchaseConstraints: map[string]any{
			"max_quantity": 3,
			"requires_id":  true,
		},
	}

	if err := repo.CreateProduct(ctx, product); err != nil {
		t.Fatal(err)
	}

	fetched, err := repo.GetProduct(ctx, productID)
	if err != nil {
		t.Fatal(err)
	}

	// Scalar fields.
	if fetched.ID != product.ID {
		t.Fatalf("expected id %q, got %q", product.ID, fetched.ID)
	}
	if fetched.Title != product.Title {
		t.Fatalf("expected title %q, got %q", product.Title, fetched.Title)
	}
	if fetched.Price.Amount != product.Price.Amount {
		t.Fatalf("expected price %d, got %d", product.Price.Amount, fetched.Price.Amount)
	}
	if fetched.Price.Currency != product.Price.Currency {
		t.Fatalf("expected currency %q, got %q", product.Price.Currency, fetched.Price.Currency)
	}
	if fetched.Availability != product.Availability {
		t.Fatalf("expected availability %d, got %d", product.Availability, fetched.Availability)
	}
	if fetched.Merchant.ID != product.Merchant.ID {
		t.Fatalf("expected merchant %q, got %q", product.Merchant.ID, fetched.Merchant.ID)
	}
	if fetched.ReturnPolicy.Days != product.ReturnPolicy.Days {
		t.Fatalf("expected return days %d, got %d", product.ReturnPolicy.Days, fetched.ReturnPolicy.Days)
	}
	if fetched.Shipping.EstimatedDays != product.Shipping.EstimatedDays {
		t.Fatalf("expected shipping days %d, got %d", product.Shipping.EstimatedDays, fetched.Shipping.EstimatedDays)
	}

	// Nested arrays.
	if !equalStrings(fetched.Features, product.Features) {
		t.Fatalf("features mismatch: got %v", fetched.Features)
	}
	if !equalStrings(fetched.Compatibility, product.Compatibility) {
		t.Fatalf("compatibility mismatch: got %v", fetched.Compatibility)
	}
	if !equalStrings(fetched.UseCases, product.UseCases) {
		t.Fatalf("use cases mismatch: got %v", fetched.UseCases)
	}

	// Nested maps.
	if !equalMaps(fetched.Attributes, product.Attributes) {
		t.Fatalf("attributes mismatch: got %v", fetched.Attributes)
	}
	if !equalMaps(fetched.PurchaseConstraints, product.PurchaseConstraints) {
		t.Fatalf("purchase constraints mismatch: got %v", fetched.PurchaseConstraints)
	}

	// Clean up.
	_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, productID)
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

func equalMaps(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}

	for k, v := range a {
		if _, ok := b[k]; !ok {
			return false
		}

		if !equalAny(b[k], v) {
			return false
		}
	}

	return true
}

// equalAny compares two `any` values, normalizing integer types so that
// JSON-decoded int64 values compare equal to integer literals (int).
func equalAny(a, b any) bool {
	aInt, aIsInt := toInt64(a)
	bInt, bIsInt := toInt64(b)

	if aIsInt && bIsInt {
		return aInt == bInt
	}

	return a == b
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case int32:
		return int64(n), true
	case uint:
		return int64(n), true
	case uint64:
		return int64(n), true
	case float64:
		// JSON numbers decode as float64; treat integral values as ints.
		if n == float64(int64(n)) {
			return int64(n), true
		}
		return 0, false
	default:
		return 0, false
	}
}
