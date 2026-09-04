package review

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	db *pgxpool.Pool
}

func NewPostgresRepository(db *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) Create(ctx context.Context, rev Review) (Review, error) {
	var orderID any
	if rev.OrderID != "" {
		orderID = rev.OrderID
	}

	err := r.db.QueryRow(
		ctx,
		`
		INSERT INTO reviews (product_id, order_id, buyer_reference, rating, comment)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at
		`,
		rev.ProductID, orderID, rev.BuyerReference, rev.Rating, rev.Comment,
	).Scan(&rev.ID, &rev.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return Review{}, ErrDuplicateReview
		}
		return Review{}, fmt.Errorf("create review: %w", err)
	}

	rev.VerifiedPurchase = rev.OrderID != ""
	return rev, nil
}

func (r *PostgresRepository) ListByProduct(ctx context.Context, productID string) ([]Review, error) {
	rows, err := r.db.Query(
		ctx,
		`
		SELECT
			id,
			product_id,
			COALESCE(order_id, ''),
			buyer_reference,
			rating,
			comment,
			(order_id IS NOT NULL),
			created_at
		FROM reviews
		WHERE product_id = $1
		ORDER BY created_at DESC
		`,
		productID,
	)
	if err != nil {
		return nil, fmt.Errorf("list reviews: %w", err)
	}
	defer rows.Close()

	var reviews []Review

	for rows.Next() {
		var rev Review
		if err := rows.Scan(
			&rev.ID,
			&rev.ProductID,
			&rev.OrderID,
			&rev.BuyerReference,
			&rev.Rating,
			&rev.Comment,
			&rev.VerifiedPurchase,
			&rev.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan review: %w", err)
		}
		reviews = append(reviews, rev)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reviews: %w", err)
	}

	return reviews, nil
}

// isUniqueViolation matches the codebase-wide convention for detecting
// a Postgres unique-constraint violation (SQLSTATE 23505) -- see
// catalog.isUniqueViolation, payment.isUniqueViolation, auth.
// isUniqueViolation for the same helper defined per-package.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
