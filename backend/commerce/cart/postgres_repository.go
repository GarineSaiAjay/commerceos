package cart

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	db *pgxpool.Pool
}

func NewPostgresRepository(db *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{
		db: db,
	}
}

func (r *PostgresRepository) CreateCart(ctx context.Context, cart Cart) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO carts (
			id,
			merchant_id,
			currency,
			subtotal_amount,
			status,
			expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6)
	`,
		cart.ID,
		cart.MerchantID,
		cart.Currency,
		cart.Subtotal,
		"active",
		cart.ExpiresAt,
	)

	if err != nil {
		return fmt.Errorf("create cart: %w", err)
	}

	return nil
}

func (r *PostgresRepository) GetCart(ctx context.Context, id string) (Cart, error) {
	var cart Cart

	err := r.db.QueryRow(ctx, `
		SELECT
			id,
			merchant_id,
			currency,
			subtotal_amount,
			status,
			expires_at,
			version
		FROM carts
		WHERE id = $1
	`, id).Scan(
		&cart.ID,
		&cart.MerchantID,
		&cart.Currency,
		&cart.Subtotal,
		&cart.Status,
		&cart.ExpiresAt,
		&cart.Version,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return Cart{}, ErrCartNotFound
	}

	if err != nil {
		return Cart{}, fmt.Errorf("get cart: %w", err)
	}

	rows, err := r.db.Query(ctx, `
		SELECT
			product_id,
			variant_id,
			title,
			quantity,
			unit_price_amount,
			total_amount
		FROM cart_items
		WHERE cart_id = $1
		ORDER BY id
	`, id)
	if err != nil {
		return Cart{}, fmt.Errorf("get cart items: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var item CartItem

		if err := rows.Scan(
			&item.ProductID,
			&item.VariantID,
			&item.Title,
			&item.Quantity,
			&item.UnitPrice,
			&item.Total,
		); err != nil {
			return Cart{}, fmt.Errorf("scan cart item: %w", err)
		}

		cart.Items = append(cart.Items, item)
	}

	if err := rows.Err(); err != nil {
		return Cart{}, fmt.Errorf("iterate cart items: %w", err)
	}

	if cart.Items == nil {
		cart.Items = []CartItem{}
	}

	return cart, nil
}

// SaveCart persists cart.Items/Subtotal, guarded by optimistic
// concurrency (full-codebase re-audit, P2 -- see
// 20260904110000_add_carts_version.sql for the full rationale): the
// UPDATE only applies if cart.Version still matches the row's current
// version (i.e. nothing else saved this cart since the caller's own
// GetCart), and bumps version by one. Zero rows affected means a
// concurrent SaveCart won the race since this cart was read --
// ErrCartConflict tells Service to re-read and retry its mutation
// rather than silently proceeding as if the stale write had applied.
func (r *PostgresRepository) SaveCart(ctx context.Context, cart Cart) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin cart transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE carts
		SET
			subtotal_amount = $1,
			version = version + 1,
			updated_at = NOW()
		WHERE id = $2 AND version = $3
	`,
		cart.Subtotal,
		cart.ID,
		cart.Version,
	)

	if err != nil {
		return fmt.Errorf("update cart: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrCartConflict
	}

	_, err = tx.Exec(ctx, `
		DELETE FROM cart_items
		WHERE cart_id = $1
	`, cart.ID)

	if err != nil {
		return fmt.Errorf("delete cart items: %w", err)
	}

	for _, item := range cart.Items {
		_, err = tx.Exec(ctx, `
			INSERT INTO cart_items (
				cart_id,
				product_id,
				variant_id,
				title,
				quantity,
				unit_price_amount,
				total_amount
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`,
			cart.ID,
			item.ProductID,
			item.VariantID,
			item.Title,
			item.Quantity,
			item.UnitPrice,
			item.Total,
		)

		if err != nil {
			return fmt.Errorf("insert cart item: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit cart transaction: %w", err)
	}

	return nil
}
