package order

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/garinesaiajay/commerceos/statemachine"
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

func (r *PostgresRepository) CheckoutCart(
	ctx context.Context,
	cartID string,
	orderID string,
) (Order, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Order{}, fmt.Errorf("begin checkout transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Lock the cart so two checkout requests cannot process it
	// simultaneously.
	var (
		merchantID string
		currency   string
		subtotal   int64
		status     string
		expiresAt  time.Time
	)

	err = tx.QueryRow(ctx, `
		SELECT
			merchant_id,
			currency,
			subtotal_amount,
			status,
			expires_at
		FROM carts
		WHERE id = $1
		FOR UPDATE
	`, cartID).Scan(
		&merchantID,
		&currency,
		&subtotal,
		&status,
		&expiresAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrCartNotFound
	}

	if err != nil {
		return Order{}, fmt.Errorf("lock cart: %w", err)
	}

	if status == "checked_out" {
		return Order{}, ErrCartAlreadyCheckedOut
	}

	if time.Now().After(expiresAt) {
		return Order{}, ErrCartExpired
	}

	// Load cart items.
	rows, err := tx.Query(ctx, `
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
	`, cartID)
	if err != nil {
		return Order{}, fmt.Errorf("load cart items: %w", err)
	}
	defer rows.Close()

	var items []OrderItem

	for rows.Next() {
		var item OrderItem

		if err := rows.Scan(
			&item.ProductID,
			&item.VariantID,
			&item.Title,
			&item.Quantity,
			&item.UnitPrice,
			&item.Total,
		); err != nil {
			return Order{}, fmt.Errorf("scan cart item: %w", err)
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return Order{}, fmt.Errorf("iterate cart items: %w", err)
	}

	if len(items) == 0 {
		return Order{}, ErrCartEmpty
	}

	// Lock each product row and decrement inventory.
	//
	// GetVariant currently reads availability from products, so the
	// product row is the inventory row we lock here.
	for _, item := range items {
		var availability int

		err := tx.QueryRow(ctx, `
			SELECT availability
			FROM products
			WHERE id = $1
			FOR UPDATE
		`, item.ProductID).Scan(&availability)

		if errors.Is(err, pgx.ErrNoRows) {
			return Order{}, fmt.Errorf(
				"product %s not found",
				item.ProductID,
			)
		}

		if err != nil {
			return Order{}, fmt.Errorf(
				"lock inventory for product %s: %w",
				item.ProductID,
				err,
			)
		}

		if availability < item.Quantity {
			return Order{}, ErrInsufficientAvailability
		}

		_, err = tx.Exec(ctx, `
			UPDATE products
			SET availability = availability - $1,
			    updated_at = NOW()
			WHERE id = $2
		`,
			item.Quantity,
			item.ProductID,
		)

		if err != nil {
			return Order{}, fmt.Errorf(
				"decrement inventory for product %s: %w",
				item.ProductID,
				err,
			)
		}
	}

	// Create order. Checkout immediately enters the payment phase, so
	// the order starts in payment_pending (per the Phase 2 state
	// machine: DRAFT → AUTHORIZED → PAYMENT_PENDING → PAID → ...).
	order := Order{
		ID:         orderID,
		MerchantID: merchantID,
		CartID:     cartID,
		Currency:   currency,
		Subtotal:   subtotal,
		Status:     "payment_pending",
		Items:      items,
		CreatedAt:  time.Now(),
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO orders (
			id,
			merchant_id,
			cart_id,
			currency,
			subtotal,
			status
		)
		VALUES ($1, $2, $3, $4, $5, $6)
	`,
		order.ID,
		order.MerchantID,
		order.CartID,
		order.Currency,
		order.Subtotal,
		order.Status,
	)

	if err != nil {
		return Order{}, fmt.Errorf("create order: %w", err)
	}

	// Create order items.
	for _, item := range order.Items {
		_, err = tx.Exec(ctx, `
			INSERT INTO order_items (
				order_id,
				product_id,
				variant_id,
				title,
				quantity,
				unit_price,
				total
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`,
			order.ID,
			item.ProductID,
			item.VariantID,
			item.Title,
			item.Quantity,
			item.UnitPrice,
			item.Total,
		)

		if err != nil {
			return Order{}, fmt.Errorf("create order item: %w", err)
		}
	}

	// Finally mark the cart as checked out.
	_, err = tx.Exec(ctx, `
		UPDATE carts
		SET status = 'checked_out',
		    updated_at = NOW()
		WHERE id = $1
	`, cartID)

	if err != nil {
		return Order{}, fmt.Errorf("mark cart checked out: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Order{}, fmt.Errorf("commit checkout transaction: %w", err)
	}

	return order, nil
}

func (r *PostgresRepository) GetOrder(
	ctx context.Context,
	orderID string,
) (Order, error) {
	var order Order

	err := r.db.QueryRow(ctx, `
		SELECT
			id,
			merchant_id,
			cart_id,
			currency,
			subtotal,
			status,
			created_at
		FROM orders
		WHERE id = $1
	`, orderID).Scan(
		&order.ID,
		&order.MerchantID,
		&order.CartID,
		&order.Currency,
		&order.Subtotal,
		&order.Status,
		&order.CreatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrOrderNotFound
	}

	if err != nil {
		return Order{}, fmt.Errorf("get order: %w", err)
	}

	rows, err := r.db.Query(ctx, `
		SELECT
			product_id,
			variant_id,
			title,
			quantity,
			unit_price,
			total
		FROM order_items
		WHERE order_id = $1
		ORDER BY id
	`, orderID)
	if err != nil {
		return Order{}, fmt.Errorf("get order items: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var item OrderItem

		if err := rows.Scan(
			&item.ProductID,
			&item.VariantID,
			&item.Title,
			&item.Quantity,
			&item.UnitPrice,
			&item.Total,
		); err != nil {
			return Order{}, fmt.Errorf("scan order item: %w", err)
		}

		order.Items = append(order.Items, item)
	}

	if err := rows.Err(); err != nil {
		return Order{}, fmt.Errorf("iterate order items: %w", err)
	}

	return order, nil
}

// TransitionStatus moves the order to `to` only if the edge is legal in
// the centralized order state machine. It reads the current status and
// updates atomically in one transaction.
func (r *PostgresRepository) TransitionStatus(
	ctx context.Context,
	orderID string,
	to string,
) (Order, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Order{}, fmt.Errorf("begin order transition: %w", err)
	}
	defer tx.Rollback(ctx)

	var from string

	err = tx.QueryRow(ctx, `
		SELECT status
		FROM orders
		WHERE id = $1
		FOR UPDATE
	`, orderID).Scan(&from)

	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrOrderNotFound
	}

	if err != nil {
		return Order{}, fmt.Errorf("lock order: %w", err)
	}

	// Guard via the centralized state machine.
	if _, err := statemachine.OrderTransitionTable().Transition(from, to); err != nil {
		return Order{}, err
	}

	_, err = tx.Exec(ctx, `
		UPDATE orders
		SET status = $1,
		    updated_at = NOW()
		WHERE id = $2
	`, to, orderID)

	if err != nil {
		return Order{}, fmt.Errorf("update order status: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Order{}, fmt.Errorf("commit order transition: %w", err)
	}

	order, err := r.GetOrder(ctx, orderID)
	if err != nil {
		return Order{}, err
	}

	return order, nil
}
