package payment

import (
	"context"
	"errors"
	"fmt"

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

	err := r.db.QueryRow(ctx, `
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
		return Payment{}, fmt.Errorf("get payment: %w", err)
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
			status
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`,
		payment.ID,
		payment.OrderID,
		payment.Provider,
		payment.ProviderOrderID,
		payment.Amount,
		payment.Currency,
		payment.Status,
	)

	if err != nil {
		return fmt.Errorf("create payment: %w", err)
	}

	return nil
}

func (r *PostgresRepository) MarkPaid(
	ctx context.Context,
	orderID string,
	providerPaymentID string,
) (Payment, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Payment{}, fmt.Errorf(
			"begin payment verification transaction: %w",
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

	// Idempotency:
	// If the payment has already been marked paid,
	// return the existing payment instead of changing it again.
	if payment.Status == "paid" {
		return payment, ErrPaymentAlreadyPaid
	}

	_, err = tx.Exec(ctx, `
		UPDATE payments
		SET status = 'paid',
		    provider_payment_id = $1,
		    updated_at = NOW()
		WHERE order_id = $2
	`,
		providerPaymentID,
		orderID,
	)

	if err != nil {
		return Payment{}, fmt.Errorf(
			"mark payment paid: %w",
			err,
		)
	}

	// The order becomes paid in the same transaction.
	commandTag, err := tx.Exec(ctx, `
		UPDATE orders
		SET status = 'paid',
		    updated_at = NOW()
		WHERE id = $1
		  AND status = 'pending'
	`,
		orderID,
	)

	if err != nil {
		return Payment{}, fmt.Errorf(
			"mark order paid: %w",
			err,
		)
	}

	if commandTag.RowsAffected() == 0 {
		return Payment{}, fmt.Errorf(
			"order %s is not pending",
			orderID,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return Payment{}, fmt.Errorf(
			"commit payment verification transaction: %w",
			err,
		)
	}

	payment.Status = "paid"

	return payment, nil
}
