package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrProductNotFound = errors.New("product not found")

// ErrProductAlreadyExists is returned when creating a product whose ID is
// already taken (products.id is the primary key).
var ErrProductAlreadyExists = errors.New("product already exists")

// ErrProductInUse is returned when deleting a product that is still
// referenced by an existing cart_items row -- cart_items.product_id has
// no ON DELETE CASCADE, unlike product_variants, so historical carts are
// never silently orphaned by a catalog edit.
var ErrProductInUse = errors.New("product is referenced by an existing cart or order")

type Repository interface {
	CreateProduct(ctx context.Context, product Product) error
	GetProduct(ctx context.Context, id string) (Product, error)
	ListProducts(ctx context.Context) ([]Product, error)
	GetVariant(ctx context.Context, id string) (ProductVariant, error)

	// UpdateProduct replaces the editable fields of an existing product
	// (everything except id/merchant_id/created_at). Returns
	// ErrProductNotFound if no such product exists.
	UpdateProduct(ctx context.Context, product Product) error

	// DeleteProduct removes a product. product_variants cascade-delete
	// automatically; a product still referenced by cart_items returns
	// ErrProductInUse instead of silently breaking that history.
	DeleteProduct(ctx context.Context, id string) error
}

type PostgresRepository struct {
	db *pgxpool.Pool
}

func NewPostgresRepository(db *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) CreateProduct(ctx context.Context, product Product) error {
	features, err := json.Marshal(product.Features)
	if err != nil {
		return fmt.Errorf("marshal features: %w", err)
	}

	compatibility, err := json.Marshal(product.Compatibility)
	if err != nil {
		return fmt.Errorf("marshal compatibility: %w", err)
	}

	useCases, err := json.Marshal(product.UseCases)
	if err != nil {
		return fmt.Errorf("marshal use cases: %w", err)
	}

	attributes, err := json.Marshal(product.Attributes)
	if err != nil {
		return fmt.Errorf("marshal attributes: %w", err)
	}

	purchaseConstraints, err := json.Marshal(product.PurchaseConstraints)
	if err != nil {
		return fmt.Errorf("marshal purchase constraints: %w", err)
	}

	returnPolicy, err := json.Marshal(product.ReturnPolicy)
	if err != nil {
		return fmt.Errorf("marshal return policy: %w", err)
	}

	shipping, err := json.Marshal(product.Shipping)
	if err != nil {
		return fmt.Errorf("marshal shipping: %w", err)
	}

	_, err = r.db.Exec(
		ctx,
		`
		INSERT INTO products (
			id,
			merchant_id,
			title,
			price_amount,
			price_currency,
			availability,
			features,
			compatibility,
			use_cases,
			return_policy,
			shipping,
			attributes,
			purchase_constraints
		)
		VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11, $12, $13
		)
		`,
		product.ID,
		product.Merchant.ID,
		product.Title,
		product.Price.Amount,
		product.Price.Currency,
		product.Availability,
		features,
		compatibility,
		useCases,
		returnPolicy,
		shipping,
		attributes,
		purchaseConstraints,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrProductAlreadyExists
		}
		return fmt.Errorf("create product: %w", err)
	}

	return nil
}

// UpdateProduct replaces every editable field of an existing product.
func (r *PostgresRepository) UpdateProduct(ctx context.Context, product Product) error {
	features, err := json.Marshal(product.Features)
	if err != nil {
		return fmt.Errorf("marshal features: %w", err)
	}

	compatibility, err := json.Marshal(product.Compatibility)
	if err != nil {
		return fmt.Errorf("marshal compatibility: %w", err)
	}

	useCases, err := json.Marshal(product.UseCases)
	if err != nil {
		return fmt.Errorf("marshal use cases: %w", err)
	}

	attributes, err := json.Marshal(product.Attributes)
	if err != nil {
		return fmt.Errorf("marshal attributes: %w", err)
	}

	purchaseConstraints, err := json.Marshal(product.PurchaseConstraints)
	if err != nil {
		return fmt.Errorf("marshal purchase constraints: %w", err)
	}

	returnPolicy, err := json.Marshal(product.ReturnPolicy)
	if err != nil {
		return fmt.Errorf("marshal return policy: %w", err)
	}

	shipping, err := json.Marshal(product.Shipping)
	if err != nil {
		return fmt.Errorf("marshal shipping: %w", err)
	}

	tag, err := r.db.Exec(
		ctx,
		`
		UPDATE products SET
			title = $2,
			price_amount = $3,
			price_currency = $4,
			availability = $5,
			features = $6,
			compatibility = $7,
			use_cases = $8,
			return_policy = $9,
			shipping = $10,
			attributes = $11,
			purchase_constraints = $12,
			updated_at = NOW()
		WHERE id = $1
		`,
		product.ID,
		product.Title,
		product.Price.Amount,
		product.Price.Currency,
		product.Availability,
		features,
		compatibility,
		useCases,
		returnPolicy,
		shipping,
		attributes,
		purchaseConstraints,
	)
	if err != nil {
		return fmt.Errorf("update product: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrProductNotFound
	}

	return nil
}

// DeleteProduct removes a product. Its variants cascade-delete at the DB
// level; a product still referenced by a cart_items row returns
// ErrProductInUse instead of a raw constraint-violation error.
func (r *PostgresRepository) DeleteProduct(ctx context.Context, id string) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM products WHERE id = $1`, id)
	if err != nil {
		if isForeignKeyViolation(err) {
			return ErrProductInUse
		}
		return fmt.Errorf("delete product: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrProductNotFound
	}

	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23503"
	}
	return false
}

func (r *PostgresRepository) GetProduct(ctx context.Context, id string) (Product, error) {
	var product Product
	var features []byte
	var compatibility []byte
	var useCases []byte
	var returnPolicy []byte
	var shipping []byte
	var attributes []byte
	var purchaseConstraints []byte

	err := r.db.QueryRow(
		ctx,
		`
		SELECT
			id,
			title,
			price_amount,
			price_currency,
			availability,
			features,
			compatibility,
			use_cases,
			return_policy,
			shipping,
			attributes,
			purchase_constraints,
			merchant_id
		FROM products
		WHERE id = $1
		`,
		id,
	).Scan(
		&product.ID,
		&product.Title,
		&product.Price.Amount,
		&product.Price.Currency,
		&product.Availability,
		&features,
		&compatibility,
		&useCases,
		&returnPolicy,
		&shipping,
		&attributes,
		&purchaseConstraints,
		&product.Merchant.ID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, ErrProductNotFound
	}
	if err != nil {
		return Product{}, fmt.Errorf("get product: %w", err)
	}

	if err := json.Unmarshal(features, &product.Features); err != nil {
		return Product{}, fmt.Errorf("decode features: %w", err)
	}

	if err := json.Unmarshal(compatibility, &product.Compatibility); err != nil {
		return Product{}, fmt.Errorf("decode compatibility: %w", err)
	}

	if err := json.Unmarshal(useCases, &product.UseCases); err != nil {
		return Product{}, fmt.Errorf("decode use cases: %w", err)
	}

	if err := json.Unmarshal(returnPolicy, &product.ReturnPolicy); err != nil {
		return Product{}, fmt.Errorf("decode return policy: %w", err)
	}

	if err := json.Unmarshal(shipping, &product.Shipping); err != nil {
		return Product{}, fmt.Errorf("decode shipping: %w", err)
	}

	if err := json.Unmarshal(attributes, &product.Attributes); err != nil {
		return Product{}, fmt.Errorf("decode attributes: %w", err)
	}

	if err := json.Unmarshal(purchaseConstraints, &product.PurchaseConstraints); err != nil {
		return Product{}, fmt.Errorf("decode purchase constraints: %w", err)
	}

	return product, nil
}

func (r *PostgresRepository) ListProducts(ctx context.Context) ([]Product, error) {
	rows, err := r.db.Query(
		ctx,
		`
		SELECT id
		FROM products
		ORDER BY created_at
		`,
	)
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}
	defer rows.Close()

	var products []Product

	for rows.Next() {
		var id string

		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan product id: %w", err)
		}

		product, err := r.GetProduct(ctx, id)
		if err != nil {
			return nil, err
		}

		products = append(products, product)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate products: %w", err)
	}

	return products, nil
}

func (r *PostgresRepository) GetVariant(ctx context.Context, id string) (ProductVariant, error) {
	var variant ProductVariant
	var attributes []byte

	err := r.db.QueryRow(
		ctx,
		`
		SELECT
			pv.id,
			pv.product_id,
			pv.sku,
			pv.price_amount,
			p.availability,
			pv.attributes,
			p.price_currency
		FROM product_variants pv
		JOIN products p ON p.id = pv.product_id
		WHERE pv.id = $1
		`,
		id,
	).Scan(
		&variant.ID,
		&variant.ProductID,
		&variant.SKU,
		&variant.Price.Amount,
		&variant.Availability,
		&attributes,
		&variant.Price.Currency,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return ProductVariant{}, ErrVariantNotFound
	}

	if err != nil {
		return ProductVariant{}, fmt.Errorf("get variant: %w", err)
	}

	if len(attributes) > 0 {
		if err := json.Unmarshal(attributes, &variant.Attributes); err != nil {
			return ProductVariant{}, fmt.Errorf("decode variant attributes: %w", err)
		}
	}

	return variant, nil
}
