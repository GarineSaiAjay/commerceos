package auth

import (
	"context"
	"errors"
	"time"
)

var ErrOperatorNotFound = errors.New("operator not found")
var ErrSessionNotFound = errors.New("session not found")

// Repository persists operator accounts and their bearer sessions.
type Repository interface {
	// GetOperatorByEmail returns ErrOperatorNotFound if no operator has
	// that email.
	GetOperatorByEmail(ctx context.Context, email string) (OperatorRecord, error)

	// CreateSession persists a new session keyed by the token's hash.
	CreateSession(ctx context.Context, tokenHash string, operatorID string, expiresAt time.Time) error

	// GetSession resolves a token hash to the operator it belongs to and
	// the session's expiry. Returns ErrSessionNotFound if the hash is
	// unknown; the caller is responsible for checking expiresAt.
	GetSession(ctx context.Context, tokenHash string) (Operator, time.Time, error)

	// DeleteSession removes a session (logout). Deleting an unknown
	// token hash is not an error -- logout is idempotent.
	DeleteSession(ctx context.Context, tokenHash string) error
}
