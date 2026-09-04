package payment

import (
	"context"
	"errors"
	"fmt"

	"github.com/garinesaiajay/commerceos/statemachine"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrPaymentNotFound = errors.New("payment not found")

var ErrPaymentAlreadyPaid = errors.New("payment already paid")

type PostgresRepository struct {
	db *pgxpool.Pool
}

func NewPostgresRepository(db *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{
		db: db,
	}
}

func (r *PostgresRepository) GetByOrderID(
	ctx context.Context,
	orderID string,
) (Payment, error) {
	var payment Payment
	var providerPaymentID *string

	err := r.db.QueryRow(ctx, `
		SELECT
			id,
			order_id,
			provider,
			provider_order_id,
			provider_payment_id,
			amount,
			currency,
			status
		FROM payments
		WHERE order_id = $1
	`, orderID).Scan(
		&payment.ID,
		&payment.OrderID,
		&payment.Provider,
		&payment.ProviderOrderID,
		&providerPaymentID,
		&payment.Amount,
		&payment.Currency,
		&payment.Status,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return Payment{}, ErrPaymentNotFound
	}

	if err != nil {
		return Payment{}, fmt.Errorf("get payment: %w", err)
	}

	if providerPaymentID != nil {
		payment.ProviderPaymentID = *providerPaymentID
	}

	return payment, nil
}

// GetByProviderOrderID mirrors GetByOrderID but looks the payment up by
// provider_order_id (the payment provider's own order ID -- e.g.
// Razorpay's "order_..." ID) instead of commerceos's own order_id. See
// the Repository interface's doc comment on this method for why the two
// are never interchangeable.
func (r *PostgresRepository) GetByProviderOrderID(
	ctx context.Context,
	providerOrderID string,
) (Payment, error) {
	var payment Payment
	var providerPaymentID *string

	err := r.db.QueryRow(ctx, `
		SELECT
			id,
			order_id,
			provider,
			provider_order_id,
			provider_payment_id,
			amount,
			currency,
			status
		FROM payments
		WHERE provider_order_id = $1
	`, providerOrderID).Scan(
		&payment.ID,
		&payment.OrderID,
		&payment.Provider,
		&payment.ProviderOrderID,
		&providerPaymentID,
		&payment.Amount,
		&payment.Currency,
		&payment.Status,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return Payment{}, ErrPaymentNotFound
	}

	if err != nil {
		return Payment{}, fmt.Errorf("get payment by provider order id: %w", err)
	}

	if providerPaymentID != nil {
		payment.ProviderPaymentID = *providerPaymentID
	}

	return payment, nil
}

func (r *PostgresRepository) GetByIdempotencyKey(
	ctx context.Context,
	key string,
) (Payment, error) {
	var payment Payment
	var providerPaymentID *string

	err := r.db.QueryRow(ctx, `
		SELECT
			id,
			order_id,
			provider,
			provider_order_id,
			provider_payment_id,
			amount,
			currency,
			status
		FROM payments
		WHERE idempotency_key = $1
	`, key).Scan(
		&payment.ID,
		&payment.OrderID,
		&payment.Provider,
		&payment.ProviderOrderID,
		&providerPaymentID,
		&payment.Amount,
		&payment.Currency,
		&payment.Status,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return Payment{}, ErrPaymentNotFound
	}

	if err != nil {
		return Payment{}, fmt.Errorf("get payment by idempotency key: %w", err)
	}

	if providerPaymentID != nil {
		payment.ProviderPaymentID = *providerPaymentID
	}

	return payment, nil
}

func (r *PostgresRepository) Create(
	ctx context.Context,
	payment Payment,
) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO payments (
			id,
			order_id,
			provider,
			provider_order_id,
			amount,
			currency,
			status,
			idempotency_key
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		payment.ID,
		payment.OrderID,
		payment.Provider,
		payment.ProviderOrderID,
		payment.Amount,
		payment.Currency,
		payment.Status,
		payment.IdempotencyKey,
	)

	if err != nil {
		return fmt.Errorf("create payment: %w", err)
	}

	return nil
}

func (r *PostgresRepository) TransitionStatus(
	ctx context.Context,
	orderID string,
	to string,
	providerPaymentID string,
) (Payment, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Payment{}, fmt.Errorf(
			"begin payment transition transaction: %w",
			err,
		)
	}

	defer tx.Rollback(ctx)

	var payment Payment

	err = tx.QueryRow(ctx, `
		SELECT
			id,
			order_id,
			provider,
			provider_order_id,
			amount,
			currency,
			status
		FROM payments
		WHERE order_id = $1
		FOR UPDATE
	`, orderID).Scan(
		&payment.ID,
		&payment.OrderID,
		&payment.Provider,
		&payment.ProviderOrderID,
		&payment.Amount,
		&payment.Currency,
		&payment.Status,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return Payment{}, ErrPaymentNotFound
	}

	if err != nil {
		return Payment{}, fmt.Errorf(
			"lock payment: %w",
			err,
		)
	}

	// Guard via the centralized payment state machine.
	if _, err := statemachine.PaymentTransitionTable().Transition(
		payment.Status,
		to,
	); err != nil {
		return Payment{}, err
	}

	_, err = tx.Exec(ctx, `
		UPDATE payments
		SET status = $1,
		    provider_payment_id = $2,
		    updated_at = NOW()
		WHERE order_id = $3
	`,
		to,
		providerPaymentID,
		orderID,
	)

	if err != nil {
		return Payment{}, fmt.Errorf(
			"mark payment %s: %w",
			to,
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return Payment{}, fmt.Errorf(
			"commit payment transition transaction: %w",
			err,
		)
	}

	payment.Status = to
	payment.ProviderPaymentID = providerPaymentID

	return payment, nil
}
