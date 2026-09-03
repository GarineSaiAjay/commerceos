package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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

// ErrVariantAlreadyExists mirrors ErrProductAlreadyExists -- returned
// when creating a variant whose id or sku (product_variants.sku is its
// own UNIQUE column) is already taken.
var ErrVariantAlreadyExists = errors.New("variant already exists")

// ErrVariantInUse mirrors ErrProductInUse exactly, one level down:
// returned when deleting a variant that is still referenced by an
// existing cart_items row -- cart_items.variant_id has no ON DELETE
// CASCADE either (unlike product_variants itself, which does cascade-
// delete when its PARENT PRODUCT is removed).
var ErrVariantInUse = errors.New("variant is referenced by an existing cart or order")

type Repository interface {
	CreateProduct(ctx context.Context, product Product) error
	GetProduct(ctx context.Context, id string) (Product, error)
	ListProducts(ctx context.Context) ([]Product, error)
	GetVariant(ctx context.Context, id string) (ProductVariant, error)

	// ListVariantsByProduct returns every variant of one product,
	// ordered by id for a deterministic display order (PLAN-02-CATALOG-
	// AND-COMMERCE.md §1, item 10) -- the buyer catalog's variant
	// picker and GetProduct's own Variants field both rely on this
	// order being stable across calls.
	ListVariantsByProduct(ctx context.Context, productID string) ([]ProductVariant, error)

	// CreateVariant adds a new variant row to an existing product
	// (PLAN-02-CATALOG-AND-COMMERCE.md §5.2 / PLAN-05-SELLER-
	// DASHBOARD.md §1's "variant sub-editor" -- item 10 shipped real
	// variants with no way to add/edit/delete them from the dashboard
	// until this). Returns ErrProductNotFound if variant.ProductID
	// doesn't reference a real product (the table's own foreign key),
	// or ErrVariantAlreadyExists if the id or sku is already taken.
	CreateVariant(ctx context.Context, variant ProductVariant) error

	// UpdateVariant replaces an existing variant's editable fields (sku,
	// price, availability, attributes) -- the same full-replace-via-
	// PATCH convention UpdateProduct already uses, not a partial patch.
	// Returns ErrVariantNotFound if no such variant exists, or
	// ErrVariantAlreadyExists if the new sku collides with another
	// variant's.
	UpdateVariant(ctx context.Context, variant ProductVariant) error

	// DeleteVariant removes a variant. Returns ErrVariantInUse if it's
	// still referenced by an existing cart_items row, or
	// ErrVariantNotFound if it doesn't exist. Deliberately does NOT
	// refuse deleting a product's last remaining variant -- see
	// catalog/handler.go's DeleteVariant doc comment for why that's a
	// documented trade-off, not an oversight.
	DeleteVariant(ctx context.Context, id string) error

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

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin create product transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(
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

	// Every product needs at least one variant for cart/checkout to
	// work end-to-end: commerce/cart.Service.AddItem and the MCP
	// add_item tool both key off variant_id, never product_id directly
	// (PLAN-02-CATALOG-AND-COMMERCE.md §1). CreateProduct's own request
	// shape has no way to specify variants yet (that's item 10's real-
	// variants work), so auto-provision a single "<product_id>-default"
	// variant mirroring the product's own price/availability -- the
	// exact convention db/seeds/001_catalog.sql already establishes by
	// hand for every seeded product, and the exact ID checkout.tsx's
	// addToCart already assumes exists
	// (`${product.product_id}-default`). Without this, a product
	// created through POST /products (including
	// frontend/app/dashboard/catalog/page.tsx, item 14) had zero
	// variants and "Add to cart" silently failed for it while working
	// for every seeded product -- reported against that page directly.
	// Both inserts share one transaction so a product is never left
	// half-created with no usable variant if the second insert fails.
	_, err = tx.Exec(
		ctx,
		`
		INSERT INTO product_variants (id, product_id, sku, price_amount, availability, attributes)
		VALUES ($1, $2, $3, $4, $5, '{}'::jsonb)
		`,
		product.ID+"-default",
		product.ID,
		strings.ToUpper(product.ID),
		product.Price.Amount,
		product.Availability,
	)
	if err != nil {
		return fmt.Errorf("create default variant: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit create product transaction: %w", err)
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

	// The LEFT JOIN against a per-product rating aggregate (PLAN-02-
	// CATALOG-AND-COMMERCE.md §2) is computed here, at read time, rather
	// than stored on products -- it can never go stale, and at this
	// catalog's size (13-50 products) the aggregate is cheap even
	// though ListProducts calls GetProduct once per row below. A
	// product with zero reviews gets average_rating 0 / review_count 0
	// via COALESCE, never a NULL that would fail the float64/int scan.
	err := r.db.QueryRow(
		ctx,
		`
		SELECT
			p.id,
			p.title,
			p.price_amount,
			p.price_currency,
			p.availability,
			p.features,
			p.compatibility,
			p.use_cases,
			p.return_policy,
			p.shipping,
			p.attributes,
			p.purchase_constraints,
			p.merchant_id,
			COALESCE(r.average_rating, 0),
			COALESCE(r.review_count, 0)
		FROM products p
		LEFT JOIN (
			SELECT
				product_id,
				AVG(rating)::float8 AS average_rating,
				COUNT(*)::int AS review_count
			FROM reviews
			GROUP BY product_id
		) r ON r.product_id = p.id
		WHERE p.id = $1
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
		&product.AverageRating,
		&product.ReviewCount,
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

	variants, err := r.ListVariantsByProduct(ctx, product.ID)
	if err != nil {
		return Product{}, fmt.Errorf("list variants for product: %w", err)
	}
	product.Variants = variants

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

// GetVariant looks up one variant by its own ID. Selects pv.availability
// (the VARIANT's own stock), not the parent product's -- this was a
// real, previously-invisible bug: every product had exactly one
// variant whose availability was seeded identically to its parent
// product, so selecting p.availability instead of pv.availability
// happened to return the same number and nothing ever caught it. Item
// 10 (PLAN-02-CATALOG-AND-COMMERCE.md §1) is the first time a product
// legitimately has variants with DIFFERENT stock levels ("out of stock
// in starlight, 16 left in white"), which would have made this wrong
// in a way that actually mattered: cart.Service.AddItem's
// `newQuantity > variant.Availability` check (backend/commerce/cart/
// service.go) exists specifically to enforce the number this query
// returns, so a wrong number here is a real overselling/underselling
// bug, not a cosmetic one.
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
			pv.availability,
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

// ListVariantsByProduct returns every variant of one product, ordered
// by id for a deterministic display order. Selects pv.availability for
// the same reason GetVariant's doc comment explains above.
func (r *PostgresRepository) ListVariantsByProduct(ctx context.Context, productID string) ([]ProductVariant, error) {
	rows, err := r.db.Query(
		ctx,
		`
		SELECT
			pv.id,
			pv.product_id,
			pv.sku,
			pv.price_amount,
			pv.availability,
			pv.attributes,
			p.price_currency
		FROM product_variants pv
		JOIN products p ON p.id = pv.product_id
		WHERE pv.product_id = $1
		ORDER BY pv.id
		`,
		productID,
	)
	if err != nil {
		return nil, fmt.Errorf("list variants by product: %w", err)
	}
	defer rows.Close()

	var variants []ProductVariant

	for rows.Next() {
		var variant ProductVariant
		var attributes []byte

		if err := rows.Scan(
			&variant.ID,
			&variant.ProductID,
			&variant.SKU,
			&variant.Price.Amount,
			&variant.Availability,
			&attributes,
			&variant.Price.Currency,
		); err != nil {
			return nil, fmt.Errorf("scan variant: %w", err)
		}

		if len(attributes) > 0 {
			if err := json.Unmarshal(attributes, &variant.Attributes); err != nil {
				return nil, fmt.Errorf("decode variant attributes: %w", err)
			}
		}

		variants = append(variants, variant)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate variants: %w", err)
	}

	return variants, nil
}

// CreateVariant adds a new variant row to an existing product. A
// foreign-key violation here means variant.ProductID doesn't reference
// a real product (ErrProductNotFound) -- distinct from DeleteProduct/
// DeleteVariant's isForeignKeyViolation usage, which means the OPPOSITE
// (a row still referencing this one), so the two call sites map the
// same Postgres error code to different sentinel errors deliberately.
func (r *PostgresRepository) CreateVariant(ctx context.Context, variant ProductVariant) error {
	attributes, err := json.Marshal(variant.Attributes)
	if err != nil {
		return fmt.Errorf("marshal variant attributes: %w", err)
	}

	_, err = r.db.Exec(
		ctx,
		`
		INSERT INTO product_variants (id, product_id, sku, price_amount, availability, attributes)
		VALUES ($1, $2, $3, $4, $5, $6)
		`,
		variant.ID,
		variant.ProductID,
		variant.SKU,
		variant.Price.Amount,
		variant.Availability,
		attributes,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrVariantAlreadyExists
		}
		if isForeignKeyViolation(err) {
			return ErrProductNotFound
		}
		return fmt.Errorf("create variant: %w", err)
	}

	return nil
}

// UpdateVariant replaces sku/price/availability/attributes for an
// existing variant -- product_id is deliberately not in the SET list,
// a variant can never be moved to a different product through this.
func (r *PostgresRepository) UpdateVariant(ctx context.Context, variant ProductVariant) error {
	attributes, err := json.Marshal(variant.Attributes)
	if err != nil {
		return fmt.Errorf("marshal variant attributes: %w", err)
	}

	tag, err := r.db.Exec(
		ctx,
		`
		UPDATE product_variants SET
			sku = $2,
			price_amount = $3,
			availability = $4,
			attributes = $5,
			updated_at = NOW()
		WHERE id = $1
		`,
		variant.ID,
		variant.SKU,
		variant.Price.Amount,
		variant.Availability,
		attributes,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrVariantAlreadyExists
		}
		return fmt.Errorf("update variant: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrVariantNotFound
	}

	return nil
}

// DeleteVariant removes a variant. A still-referencing cart_items row
// (no ON DELETE CASCADE there) surfaces as ErrVariantInUse instead of a
// raw constraint-violation error, mirroring DeleteProduct exactly.
func (r *PostgresRepository) DeleteVariant(ctx context.Context, id string) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM product_variants WHERE id = $1`, id)
	if err != nil {
		if isForeignKeyViolation(err) {
			return ErrVariantInUse
		}
		return fmt.Errorf("delete variant: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrVariantNotFound
	}

	return nil
}
