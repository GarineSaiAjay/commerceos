package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	db *pgxpool.Pool
}

func NewPostgresRepository(db *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) GetOperatorByEmail(ctx context.Context, email string) (OperatorRecord, error) {
	var rec OperatorRecord

	err := r.db.QueryRow(ctx, `
		SELECT id, merchant_id, email, password_hash
		FROM operators
		WHERE email = $1
	`, email).Scan(&rec.ID, &rec.MerchantID, &rec.Email, &rec.PasswordHash)

	if errors.Is(err, pgx.ErrNoRows) {
		return OperatorRecord{}, ErrOperatorNotFound
	}
	if err != nil {
		return OperatorRecord{}, fmt.Errorf("get operator by email: %w", err)
	}

	return rec, nil
}

func (r *PostgresRepository) CreateSession(
	ctx context.Context,
	tokenHash string,
	operatorID string,
	expiresAt time.Time,
) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO operator_sessions (token_hash, operator_id, expires_at)
		VALUES ($1, $2, $3)
	`, tokenHash, operatorID, expiresAt)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (r *PostgresRepository) GetSession(
	ctx context.Context,
	tokenHash string,
) (Operator, time.Time, error) {
	var op Operator
	var expiresAt time.Time

	err := r.db.QueryRow(ctx, `
		SELECT o.id, o.merchant_id, o.email, s.expires_at
		FROM operator_sessions s
		JOIN operators o ON o.id = s.operator_id
		WHERE s.token_hash = $1
	`, tokenHash).Scan(&op.ID, &op.MerchantID, &op.Email, &expiresAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return Operator{}, time.Time{}, ErrSessionNotFound
	}
	if err != nil {
		return Operator{}, time.Time{}, fmt.Errorf("get session: %w", err)
	}

	return op, expiresAt, nil
}

func (r *PostgresRepository) DeleteSession(ctx context.Context, tokenHash string) error {
	if _, err := r.db.Exec(ctx, `
		DELETE FROM operator_sessions WHERE token_hash = $1
	`, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}
