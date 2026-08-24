package order

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestOrderImmutability proves the Phase 1 requirement (section 4.3):
// an order is created from a *snapshot* of the cart, not a live
// reference. Mutating the cart after order creation must not change
// the order.
func TestOrderImmutability(t *testing.T) {
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

	cartID := "cart_immutability_test"
	orderID := "order_immutability_test"
	productID := "immutability-test-product"
	variantID := "immutability-test-variant"

	// Clean up in case the test was run before.
	_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id = $1`, orderID)
	_, _ = pool.Exec(ctx, `DELETE FROM carts WHERE id = $1`, cartID)
	_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, productID)

	// Create a dedicated product so availability is deterministic
	// (seeded products get decremented by checkout runs).
	_, err = pool.Exec(ctx, `
		INSERT INTO products (
			id, merchant_id, title, price_amount, price_currency,
			availability, features, compatibility, use_cases,
			return_policy, shipping, attributes, purchase_constraints
		)
		VALUES (
			$1, 'merchant_001', 'Immutability Test Product', 24900, 'INR',
			100, '[]', '[]', '[]',
			'{"days": 7}', '{"estimated_days": 3}', '{}', '{}'
		)
	`, productID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO product_variants (id, product_id, sku, price_amount, availability, attributes)
		VALUES ($1, $2, 'IMMUTABILITY-TEST', 24900, 100, '{}')
	`, variantID, productID)
	if err != nil {
		t.Fatal(err)
	}

	// Create a cart with one item.
	_, err = pool.Exec(ctx, `
		INSERT INTO carts (id, merchant_id, currency, subtotal_amount, status, expires_at)
		VALUES ($1, 'merchant_001', 'INR', 24900, 'active', $2)
	`, cartID, time.Now().Add(9*time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO cart_items (
			cart_id, product_id, variant_id, title, quantity,
			unit_price_amount, total_amount
		)
		VALUES ($1, $2, $3, 'Immutability Test Product', 1, 24900, 24900)
	`, cartID, productID, variantID)
	if err != nil {
		t.Fatal(err)
	}

	repo := NewPostgresRepository(pool)

	// Create the order from the cart snapshot.
	order, err := repo.CheckoutCart(ctx, cartID, orderID)
	if err != nil {
		t.Fatal(err)
	}

	if order.Subtotal != 24900 {
		t.Fatalf("expected order subtotal 24900, got %d", order.Subtotal)
	}
	if len(order.Items) != 1 {
		t.Fatalf("expected 1 order item, got %d", len(order.Items))
	}

	// Mutate the original cart: change quantity and add a second item.
	_, err = pool.Exec(ctx, `
		UPDATE cart_items
		SET quantity = 5, total_amount = 124500
		WHERE cart_id = $1
	`, cartID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO cart_items (
			cart_id, product_id, variant_id, title, quantity,
			unit_price_amount, total_amount
		)
		VALUES ($1, 'airpods-case', 'airpods-case-default', 'AirPods Case', 1, 1999, 1999)
	`, cartID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = pool.Exec(ctx, `
		UPDATE carts SET subtotal_amount = 126499 WHERE id = $1
	`, cartID)
	if err != nil {
		t.Fatal(err)
	}

	// Re-fetch the order — it must be unchanged.
	fetched, err := repo.GetOrder(ctx, orderID)
	if err != nil {
		t.Fatal(err)
	}

	if fetched.Subtotal != 24900 {
		t.Fatalf(
			"order subtotal changed after cart mutation: expected 24900, got %d",
			fetched.Subtotal,
		)
	}

	if len(fetched.Items) != 1 {
		t.Fatalf(
			"order item count changed after cart mutation: expected 1, got %d",
			len(fetched.Items),
		)
	}

	if fetched.Items[0].Quantity != 1 {
		t.Fatalf(
			"order item quantity changed after cart mutation: expected 1, got %d",
			fetched.Items[0].Quantity,
		)
	}

	if fetched.Items[0].Total != 24900 {
		t.Fatalf(
			"order item total changed after cart mutation: expected 24900, got %d",
			fetched.Items[0].Total,
		)
	}

	// Clean up.
	_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id = $1`, orderID)
	_, _ = pool.Exec(ctx, `DELETE FROM carts WHERE id = $1`, cartID)
	_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, productID)
	_, _ = pool.Exec(ctx, `DELETE FROM product_variants WHERE id = $1`, variantID)
}
