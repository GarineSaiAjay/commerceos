package payment

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresAttemptRepository struct {
	db *pgxpool.Pool
}

func NewPostgresAttemptRepository(
	db *pgxpool.Pool,
) *PostgresAttemptRepository {
	return &PostgresAttemptRepository{
		db: db,
	}
}

func (r *PostgresAttemptRepository) Create(
	ctx context.Context,
	attempt PaymentAttempt,
) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO payment_attempts (
			id,
			payment_id,
			order_id,
			provider_order_id,
			razorpay_payment_id,
			amount,
			currency,
			status,
			error_code,
			error_description
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`,
		attempt.ID,
		attempt.PaymentID,
		attempt.OrderID,
		attempt.ProviderOrderID,
		attempt.RazorpayPaymentID,
		attempt.Amount,
		attempt.Currency,
		attempt.Status,
		attempt.ErrorCode,
		attempt.ErrorDescription,
	)

	if err != nil {
		return fmt.Errorf("create payment attempt: %w", err)
	}

	return nil
}

func (r *PostgresAttemptRepository) MarkPaid(
	ctx context.Context,
	orderID string,
	razorpayPaymentID string,
) (PaymentAttempt, error) {
	var attempt PaymentAttempt

	err := r.db.QueryRow(ctx, `
		UPDATE payment_attempts
		SET status = 'paid',
		    razorpay_payment_id = $1,
		    updated_at = NOW()
		WHERE order_id = $2
		  AND status = 'attempted'
		RETURNING
			id,
			payment_id,
			order_id,
			provider_order_id,
			razorpay_payment_id,
			amount,
			currency,
			status,
			error_code,
			error_description
	`,
		razorpayPaymentID,
		orderID,
	).Scan(
		&attempt.ID,
		&attempt.PaymentID,
		&attempt.OrderID,
		&attempt.ProviderOrderID,
		&attempt.RazorpayPaymentID,
		&attempt.Amount,
		&attempt.Currency,
		&attempt.Status,
		&attempt.ErrorCode,
		&attempt.ErrorDescription,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentAttempt{}, fmt.Errorf(
			"no attempted payment found for order %s",
			orderID,
		)
	}

	if err != nil {
		return PaymentAttempt{}, fmt.Errorf(
			"mark payment attempt paid: %w",
			err,
		)
	}

	return attempt, nil
}
