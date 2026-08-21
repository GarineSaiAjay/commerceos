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

	if product.Price.Amount != 24900 {
		t.Fatalf("expected price 24900, got %d", product.Price.Amount)
	}

	if product.Price.Currency != "INR" {
		t.Fatalf("expected currency INR, got %s", product.Price.Currency)
	}

	if product.Availability != 12 {
		t.Fatalf("expected availability 12, got %d", product.Availability)
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

	if len(products) != 4 {
		t.Fatalf("expected 4 products, got %d", len(products))
	}
}
