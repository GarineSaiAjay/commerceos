package order

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/garinesaiajay/commerceos/audit"
	"github.com/garinesaiajay/commerceos/statemachine"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	db *pgxpool.Pool
	// auditWriter is nil-safe (see the nil check where it's used in
	// CheckoutCart) so existing callers/tests that construct this
	// repository without WithAuditWriter keep working unchanged.
	auditWriter audit.Writer
}

func NewPostgresRepository(db *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{
		db: db,
	}
}

// WithAuditWriter attaches the audit trail writer used to record
// campaign-discount events at checkout (discount applied / campaign
// budget exhausted). Matches the WithAttempts/WithRecoveryReaders
// builder pattern already used elsewhere in this codebase
// (backend/commerce/payment/webhook_applier.go, handler.go) so it can
// be added without changing this constructor's signature at any
// existing call site.
func (r *PostgresRepository) WithAuditWriter(w audit.Writer) *PostgresRepository {
	r.auditWriter = w
	return r
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

	// Campaign discount hook: apply at most one campaign discount per
	// checkout -- the first cart item whose product has a currently
	// ACTIVE campaign for this merchant. One discount per checkout (not
	// one per matching line item) is a deliberate MVP scope cut.
	//
	// The atomic guard below (`UPDATE ... WHERE spent + $1 <= budget_cap`)
	// is the only place a campaign's spend is incremented, and it can
	// never push spent past budget_cap because the invariant is
	// re-checked in the same statement that performs the increment --
	// no read-then-write race window between two concurrent checkouts
	// against the same campaign.
	//
	// If the budget is already exhausted (or was spent by a concurrent
	// checkout between the SELECT and this UPDATE), that is NOT an
	// error: checkout proceeds at full price. This is the "one failure
	// handled gracefully" case for this feature -- recorded via the
	// audit trail after commit (see below), never by failing checkout.
	var discountAmount int64
	var appliedCampaignID string
	var appliedProductID string
	type skippedCampaign struct {
		campaignID string
		productID  string
	}
	var skippedCampaigns []skippedCampaign

	for i := range items {
		var campaignID string
		var discountPercent int

		err := tx.QueryRow(ctx, `
			SELECT id, discount_percent FROM campaigns
			WHERE merchant_id = $1 AND product_id = $2 AND status = 'ACTIVE'
			  AND (ends_at IS NULL OR ends_at > NOW())
			ORDER BY created_at DESC LIMIT 1
		`, merchantID, items[i].ProductID).Scan(&campaignID, &discountPercent)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // no active campaign for this product; try the next item
		}
		if err != nil {
			return Order{}, fmt.Errorf(
				"look up active campaign for product %s: %w",
				items[i].ProductID, err,
			)
		}

		candidateDiscount := items[i].Total * int64(discountPercent) / 100

		var guardedID string
		err = tx.QueryRow(ctx, `
			UPDATE campaigns SET spent = spent + $1, updated_at = NOW()
			WHERE id = $2 AND spent + $1 <= budget_cap
			RETURNING id
		`, candidateDiscount, campaignID).Scan(&guardedID)
		if errors.Is(err, pgx.ErrNoRows) {
			skippedCampaigns = append(skippedCampaigns, skippedCampaign{
				campaignID: campaignID, productID: items[i].ProductID,
			})
			continue
		}
		if err != nil {
			return Order{}, fmt.Errorf("apply campaign discount guard: %w", err)
		}

		discountAmount = candidateDiscount
		appliedCampaignID = campaignID
		appliedProductID = items[i].ProductID
		break
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
		ID:             orderID,
		MerchantID:     merchantID,
		CartID:         cartID,
		Currency:       currency,
		Subtotal:       subtotal - discountAmount,
		DiscountAmount: discountAmount,
		CampaignID:     appliedCampaignID,
		Status:         "payment_pending",
		Items:          items,
		CreatedAt:      time.Now(),
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO orders (
			id,
			merchant_id,
			cart_id,
			currency,
			subtotal,
			discount_amount,
			campaign_id,
			status
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		order.ID,
		order.MerchantID,
		order.CartID,
		order.Currency,
		order.Subtotal,
		order.DiscountAmount,
		nilIfEmpty(order.CampaignID),
		order.Status,
	)

	if err != nil {
		return Order{}, fmt.Errorf("create order: %w", err)
	}

	if discountAmount > 0 {
		_, err = tx.Exec(ctx, `
			INSERT INTO campaign_redemptions (campaign_id, order_id, discount_amount)
			VALUES ($1, $2, $3)
		`, appliedCampaignID, order.ID, discountAmount)
		if err != nil {
			return Order{}, fmt.Errorf("record campaign redemption: %w", err)
		}
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

	// Campaign audit events fire AFTER commit, not inside the
	// transaction above: audit.Writer.Write always opens its own
	// transaction (backend/audit/postgres_writer.go) and cannot
	// participate in this one, so writing it earlier could record an
	// event describing an order that a later error in this function
	// rolled back. Best-effort: a failed audit write does not fail an
	// already-committed checkout.
	if r.auditWriter != nil {
		if discountAmount > 0 {
			if err := r.auditWriter.Write(ctx, "system", "campaign_discount_applied", "campaign", appliedCampaignID, map[string]any{
				"order_id":        order.ID,
				"product_id":      appliedProductID,
				"discount_amount": discountAmount,
			}); err != nil {
				log.Printf("[order] audit write failed for campaign_discount_applied (order %s, campaign %s): %v", order.ID, appliedCampaignID, err)
			}
		}
		for _, skipped := range skippedCampaigns {
			if err := r.auditWriter.Write(ctx, "system", "campaign_budget_exhausted", "campaign", skipped.campaignID, map[string]any{
				"order_id":   order.ID,
				"product_id": skipped.productID,
			}); err != nil {
				log.Printf("[order] audit write failed for campaign_budget_exhausted (order %s, campaign %s): %v", order.ID, skipped.campaignID, err)
			}
		}
	}

	return order, nil
}

// nilIfEmpty turns an empty string into a SQL NULL -- used for the
// nullable campaign_id FK on orders, which is empty unless a campaign
// discount was applied at checkout.
func nilIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func (r *PostgresRepository) GetOrder(
	ctx context.Context,
	orderID string,
) (Order, error) {
	var order Order
	var campaignID *string
	var runID *string

	// LEFT JOIN payments -- 1:1 per order (payments.order_id is UNIQUE),
	// so this can never duplicate the orders row; COALESCE keeps
	// PaymentStatus an honest empty string rather than a literal "NULL"
	// when no payment has been created for this order yet (item 15,
	// PLAN-05-SELLER-DASHBOARD.md §2).
	err := r.db.QueryRow(ctx, `
		SELECT
			o.id,
			o.merchant_id,
			o.cart_id,
			o.currency,
			o.subtotal,
			o.discount_amount,
			o.campaign_id,
			o.status,
			o.created_at,
			COALESCE(p.status, ''),
			o.run_id
		FROM orders o
		LEFT JOIN payments p ON p.order_id = o.id
		WHERE o.id = $1
	`, orderID).Scan(
		&order.ID,
		&order.MerchantID,
		&order.CartID,
		&order.Currency,
		&order.Subtotal,
		&order.DiscountAmount,
		&campaignID,
		&order.Status,
		&order.CreatedAt,
		&order.PaymentStatus,
		&runID,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrOrderNotFound
	}

	if err != nil {
		return Order{}, fmt.Errorf("get order: %w", err)
	}
	if campaignID != nil {
		order.CampaignID = *campaignID
	}
	if runID != nil {
		order.RunID = *runID
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

// ListOrders returns a merchant's orders, most recent first, each with
// its items attached. Two queries (orders, then their items in bulk)
// instead of one GetOrder call per order, so this stays O(1) round
// trips regardless of how many orders the merchant has.
func (r *PostgresRepository) ListOrders(
	ctx context.Context,
	merchantID string,
) ([]Order, error) {
	rows, err := r.db.Query(ctx, `
		SELECT
			o.id,
			o.merchant_id,
			o.cart_id,
			o.currency,
			o.subtotal,
			o.discount_amount,
			o.campaign_id,
			o.status,
			o.created_at,
			COALESCE(p.status, ''),
			o.run_id
		FROM orders o
		LEFT JOIN payments p ON p.order_id = o.id
		WHERE o.merchant_id = $1
		ORDER BY o.created_at DESC
	`, merchantID)
	if err != nil {
		return nil, fmt.Errorf("list orders: %w", err)
	}
	defer rows.Close()

	var orders []Order
	orderIndex := make(map[string]int)

	for rows.Next() {
		var o Order
		var campaignID *string
		var runID *string

		if err := rows.Scan(
			&o.ID,
			&o.MerchantID,
			&o.CartID,
			&o.Currency,
			&o.Subtotal,
			&o.DiscountAmount,
			&campaignID,
			&o.Status,
			&o.CreatedAt,
			&o.PaymentStatus,
			&runID,
		); err != nil {
			return nil, fmt.Errorf("scan order: %w", err)
		}
		if campaignID != nil {
			o.CampaignID = *campaignID
		}
		if runID != nil {
			o.RunID = *runID
		}

		o.Items = []OrderItem{}
		orderIndex[o.ID] = len(orders)
		orders = append(orders, o)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate orders: %w", err)
	}

	if len(orders) == 0 {
		return []Order{}, nil
	}

	orderIDs := make([]string, 0, len(orders))
	for _, o := range orders {
		orderIDs = append(orderIDs, o.ID)
	}

	itemRows, err := r.db.Query(ctx, `
		SELECT
			order_id,
			product_id,
			variant_id,
			title,
			quantity,
			unit_price,
			total
		FROM order_items
		WHERE order_id = ANY($1)
		ORDER BY id
	`, orderIDs)
	if err != nil {
		return nil, fmt.Errorf("list order items: %w", err)
	}
	defer itemRows.Close()

	for itemRows.Next() {
		var orderID string
		var item OrderItem

		if err := itemRows.Scan(
			&orderID,
			&item.ProductID,
			&item.VariantID,
			&item.Title,
			&item.Quantity,
			&item.UnitPrice,
			&item.Total,
		); err != nil {
			return nil, fmt.Errorf("scan order item: %w", err)
		}

		idx, ok := orderIndex[orderID]
		if !ok {
			continue
		}

		orders[idx].Items = append(orders[idx].Items, item)
	}

	if err := itemRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate order items: %w", err)
	}

	return orders, nil
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

// SetRunID tags an order with the agent run that authorized its
// payment (PLAN-05-SELLER-DASHBOARD.md §2). Unlike TransitionStatus,
// this deliberately does NOT go through the centralized order state
// machine or check the order's current status -- run_id is pure audit
// metadata, not a state the checkout saga (order/saga.go) transitions
// through, so there is no illegal-edge concept for it to violate.
func (r *PostgresRepository) SetRunID(
	ctx context.Context,
	orderID string,
	runID string,
) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE orders
		SET run_id = $1,
		    updated_at = NOW()
		WHERE id = $2
	`, runID, orderID)

	if err != nil {
		return fmt.Errorf("set order run id: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrOrderNotFound
	}

	return nil
}
