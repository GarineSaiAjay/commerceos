package review

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestCreateRejectsDuplicateReview proves the new
// reviews_order_product_unique constraint (db/migrations/
// *_add_reviews_order_product_unique.sql, added after a fresh audit
// against PLAN-02-CATALOG-AND-COMMERCE.md found no guard here) is
// actually enforced end to end: PostgresRepository.Create translates
// the resulting unique-violation into ErrDuplicateReview, not a raw
// SQL error, and a real second review for the same order+product never
// lands a second row.
func TestCreateRejectsDuplicateReview(t *testing.T) {
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

	productID := "review-dup-test-product"
	orderID := "order_review_dup_test"
	cartID := "cart_review_dup_test"

	_, _ = pool.Exec(ctx, `DELETE FROM reviews WHERE product_id = $1`, productID)
	_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id = $1`, orderID)
	_, _ = pool.Exec(ctx, `DELETE FROM carts WHERE id = $1`, cartID)
	_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, productID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM reviews WHERE product_id = $1`, productID)
		_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id = $1`, orderID)
		_, _ = pool.Exec(ctx, `DELETE FROM carts WHERE id = $1`, cartID)
		_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, productID)
	})

	_, err = pool.Exec(ctx, `
		INSERT INTO products (
			id, merchant_id, title, price_amount, price_currency,
			availability, features, compatibility, use_cases,
			return_policy, shipping, attributes, purchase_constraints
		)
		VALUES (
			$1, 'merchant_001', 'Review Dup Test Product', 5000, 'INR',
			10, '[]', '[]', '[]',
			'{"days": 7}', '{"estimated_days": 3}', '{}', '{}'
		)
	`, productID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO carts (id, merchant_id, currency, subtotal_amount, status, expires_at)
		VALUES ($1, 'merchant_001', 'INR', 5000, 'checked_out', $2)
	`, cartID, time.Now().Add(9*time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO orders (id, merchant_id, cart_id, currency, subtotal, status)
		VALUES ($1, 'merchant_001', $2, 'INR', 5000, 'paid')
	`, orderID, cartID)
	if err != nil {
		t.Fatal(err)
	}

	repo := NewPostgresRepository(pool)

	rev := Review{
		ProductID:      productID,
		OrderID:        orderID,
		BuyerReference: "Test Buyer",
		Rating:         5,
		Comment:        "Great!",
	}

	if _, err := repo.Create(ctx, rev); err != nil {
		t.Fatalf("first Create should succeed: %v", err)
	}

	if _, err := repo.Create(ctx, rev); !errors.Is(err, ErrDuplicateReview) {
		t.Fatalf("expected ErrDuplicateReview on a second review for the same order+product, got %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM reviews WHERE order_id = $1 AND product_id = $2`, orderID, productID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 review row after the rejected duplicate, got %d", count)
	}

	// Seed-style reviews (nullable order_id, db/seeds/003_reviews.sql's
	// own convention) must remain unaffected by this constraint --
	// Postgres never treats two NULLs as equal in a unique index, so
	// multiple order_id-less reviews for the same product must still
	// be allowed.
	seedRev := Review{ProductID: productID, BuyerReference: "Seed Reviewer", Rating: 4, Comment: "ok"}
	if _, err := repo.Create(ctx, seedRev); err != nil {
		t.Fatalf("first seed-style (no order_id) review should succeed: %v", err)
	}
	if _, err := repo.Create(ctx, seedRev); err != nil {
		t.Fatalf("a second seed-style (no order_id) review for the same product must NOT be rejected as a duplicate, got %v", err)
	}
}
