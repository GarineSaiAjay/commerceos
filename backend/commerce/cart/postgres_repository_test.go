package cart

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresRepositoryCartLifecycle(t *testing.T) {
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

	cartID := "cart_test_001"

	// Clean up in case the test was run before.
	_, err = pool.Exec(ctx, `
		DELETE FROM carts
		WHERE id = $1
	`, cartID)
	if err != nil {
		t.Fatal(err)
	}

	cart := Cart{
		ID:         cartID,
		MerchantID: "merchant_001",
		Currency:   "INR",
		Subtotal:   0,
		Items:      []CartItem{},
		ExpiresAt:  time.Now().Add(9 * time.Minute),
	}

	t.Run("CreateCart", func(t *testing.T) {
		if err := repo.CreateCart(ctx, cart); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("SaveCart", func(t *testing.T) {
		cart.Items = []CartItem{
			{
				ProductID: "airpods-pro-2",
				VariantID: "airpods-pro-2-default",
				Title:     "AirPods Pro",
				Quantity:  2,
				UnitPrice: 24900,
				Total:     49800,
			},
		}
		cart.Subtotal = 49800

		if err := repo.SaveCart(ctx, cart); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("GetCart", func(t *testing.T) {
		got, err := repo.GetCart(ctx, cartID)
		if err != nil {
			t.Fatal(err)
		}

		if got.ID != cartID {
			t.Fatalf("expected cart ID %q, got %q", cartID, got.ID)
		}

		if got.Subtotal != 49800 {
			t.Fatalf("expected subtotal 49800, got %d", got.Subtotal)
		}

		if len(got.Items) != 1 {
			t.Fatalf("expected 1 item, got %d", len(got.Items))
		}

		if got.Items[0].Quantity != 2 {
			t.Fatalf("expected quantity 2, got %d", got.Items[0].Quantity)
		}

		if got.Items[0].Total != 49800 {
			t.Fatalf("expected item total 49800, got %d", got.Items[0].Total)
		}
	})

	// Final cleanup.
	_, err = pool.Exec(ctx, `
		DELETE FROM carts
		WHERE id = $1
	`, cartID)
	if err != nil {
		t.Fatal(err)
	}
}
