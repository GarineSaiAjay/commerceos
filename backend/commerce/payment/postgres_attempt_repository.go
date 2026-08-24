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

// nilIdempotency maps an empty idempotency key to NULL so the unique
// partial index (only on non-NULL keys) is not violated by attempts
// created without a key.
func nilIdempotency(key string) any {
	if key == "" {
		return nil
	}
	return key
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
			error_description,
			idempotency_key
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
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
		nilIdempotency(attempt.IdempotencyKey),
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

// MarkFailed transitions the attempt for an order to failed and records
// the provider error details.
func (r *PostgresAttemptRepository) MarkFailed(
	ctx context.Context,
	orderID string,
	razorpayPaymentID string,
	errorCode string,
	errorDescription string,
) (PaymentAttempt, error) {
	var attempt PaymentAttempt

	err := r.db.QueryRow(ctx, `
		UPDATE payment_attempts
		SET status = 'failed',
		    razorpay_payment_id = COALESCE($1, razorpay_payment_id),
		    error_code = $3,
		    error_description = $4,
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
		errorCode,
		errorDescription,
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
		// No attempted row to fail — this is not an error for the
		// webhook path (there may not be a locally tracked attempt).
		return PaymentAttempt{}, nil
	}

	if err != nil {
		return PaymentAttempt{}, fmt.Errorf(
			"mark payment attempt failed: %w",
			err,
		)
	}

	return attempt, nil
}

// GetLatestForOrder returns the most recent attempt for an order.
func (r *PostgresAttemptRepository) GetLatestForOrder(
	ctx context.Context,
	orderID string,
) (PaymentAttempt, error) {
	var attempt PaymentAttempt

	err := r.db.QueryRow(ctx, `
		SELECT
			id, payment_id, order_id, provider_order_id, razorpay_payment_id,
			amount, currency, status, error_code, error_description
		FROM payment_attempts
		WHERE order_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, orderID).Scan(
		&attempt.ID, &attempt.PaymentID, &attempt.OrderID, &attempt.ProviderOrderID, &attempt.RazorpayPaymentID,
		&attempt.Amount, &attempt.Currency, &attempt.Status, &attempt.ErrorCode, &attempt.ErrorDescription,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentAttempt{}, fmt.Errorf("no payment attempt for order %s", orderID)
	}
	if err != nil {
		return PaymentAttempt{}, fmt.Errorf("get latest attempt: %w", err)
	}

	return attempt, nil
}
