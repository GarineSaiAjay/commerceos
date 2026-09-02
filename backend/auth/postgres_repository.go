package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

func (r *PostgresRepository) ListOperators(ctx context.Context, merchantID string) ([]OperatorRecord, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, merchant_id, email, password_hash
		FROM operators
		WHERE merchant_id = $1
		ORDER BY created_at ASC
	`, merchantID)
	if err != nil {
		return nil, fmt.Errorf("list operators: %w", err)
	}
	defer rows.Close()

	var records []OperatorRecord
	for rows.Next() {
		var rec OperatorRecord
		if err := rows.Scan(&rec.ID, &rec.MerchantID, &rec.Email, &rec.PasswordHash); err != nil {
			return nil, fmt.Errorf("list operators: scan: %w", err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list operators: %w", err)
	}

	return records, nil
}

func (r *PostgresRepository) CreateOperator(ctx context.Context, rec OperatorRecord) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO operators (id, merchant_id, email, password_hash)
		VALUES ($1, $2, $3, $4)
	`, rec.ID, rec.MerchantID, rec.Email, rec.PasswordHash)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrEmailAlreadyRegistered
		}
		return fmt.Errorf("create operator: %w", err)
	}
	return nil
}

func (r *PostgresRepository) DeleteOperator(ctx context.Context, operatorID string, merchantID string) (int64, error) {
	tag, err := r.db.Exec(ctx, `
		DELETE FROM operators WHERE id = $1 AND merchant_id = $2
	`, operatorID, merchantID)
	if err != nil {
		return 0, fmt.Errorf("delete operator: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *PostgresRepository) CreateInvite(ctx context.Context, invite Invite, tokenHash string) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO operator_invites (id, merchant_id, email, token_hash, invited_by, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, invite.ID, invite.MerchantID, invite.Email, tokenHash, invite.InvitedBy, invite.ExpiresAt)
	if err != nil {
		return fmt.Errorf("create invite: %w", err)
	}
	return nil
}

func (r *PostgresRepository) GetInviteByTokenHash(ctx context.Context, tokenHash string) (Invite, error) {
	var inv Invite

	err := r.db.QueryRow(ctx, `
		SELECT id, merchant_id, email, invited_by, expires_at, accepted_at
		FROM operator_invites
		WHERE token_hash = $1
	`, tokenHash).Scan(&inv.ID, &inv.MerchantID, &inv.Email, &inv.InvitedBy, &inv.ExpiresAt, &inv.AcceptedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return Invite{}, ErrInviteNotFound
	}
	if err != nil {
		return Invite{}, fmt.Errorf("get invite by token hash: %w", err)
	}

	return inv, nil
}

func (r *PostgresRepository) ListInvites(ctx context.Context, merchantID string) ([]Invite, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, merchant_id, email, invited_by, expires_at, accepted_at
		FROM operator_invites
		WHERE merchant_id = $1
		ORDER BY created_at DESC
	`, merchantID)
	if err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	defer rows.Close()

	var invites []Invite
	for rows.Next() {
		var inv Invite
		if err := rows.Scan(&inv.ID, &inv.MerchantID, &inv.Email, &inv.InvitedBy, &inv.ExpiresAt, &inv.AcceptedAt); err != nil {
			return nil, fmt.Errorf("list invites: scan: %w", err)
		}
		invites = append(invites, inv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}

	return invites, nil
}

func (r *PostgresRepository) MarkInviteAccepted(ctx context.Context, inviteID string, acceptedAt time.Time) error {
	if _, err := r.db.Exec(ctx, `
		UPDATE operator_invites SET accepted_at = $2 WHERE id = $1
	`, inviteID, acceptedAt); err != nil {
		return fmt.Errorf("mark invite accepted: %w", err)
	}
	return nil
}

func (r *PostgresRepository) DeleteInvite(ctx context.Context, inviteID string, merchantID string) (int64, error) {
	tag, err := r.db.Exec(ctx, `
		DELETE FROM operator_invites WHERE id = $1 AND merchant_id = $2
	`, inviteID, merchantID)
	if err != nil {
		return 0, fmt.Errorf("delete invite: %w", err)
	}
	return tag.RowsAffected(), nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
