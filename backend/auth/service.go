package auth

import (
	"context"
	"errors"
	"time"
)

// SessionTTL is how long an operator's bearer token stays valid after
// login.
const SessionTTL = 24 * time.Hour

var ErrInvalidCredentials = errors.New("invalid email or password")
var ErrInvalidSession = errors.New("invalid or expired session")

type Service struct {
	repo Repository
	now  func() time.Time
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

// Login verifies email/password and, on success, issues a new bearer
// session token. The same ErrInvalidCredentials is returned whether the
// email is unknown or the password is wrong, so a caller can't use this
// endpoint to enumerate registered operator emails.
func (s *Service) Login(ctx context.Context, email, password string) (token string, operator Operator, err error) {
	record, err := s.repo.GetOperatorByEmail(ctx, email)
	if err != nil {
		return "", Operator{}, ErrInvalidCredentials
	}

	if !VerifyPassword(password, record.PasswordHash) {
		return "", Operator{}, ErrInvalidCredentials
	}

	token, tokenHash, err := generateSessionToken()
	if err != nil {
		return "", Operator{}, err
	}

	if err := s.repo.CreateSession(ctx, tokenHash, record.ID, s.now().Add(SessionTTL)); err != nil {
		return "", Operator{}, err
	}

	return token, Operator{ID: record.ID, MerchantID: record.MerchantID, Email: record.Email}, nil
}

// Logout deletes the session for token. Idempotent: logging out twice,
// or with an already-invalid token, is not an error.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.repo.DeleteSession(ctx, hashToken(token))
}

// ValidateToken resolves a bearer token to the operator it belongs to,
// or ErrInvalidSession if the token is unknown or expired.
func (s *Service) ValidateToken(ctx context.Context, token string) (Operator, error) {
	if token == "" {
		return Operator{}, ErrInvalidSession
	}

	operator, expiresAt, err := s.repo.GetSession(ctx, hashToken(token))
	if err != nil {
		return Operator{}, ErrInvalidSession
	}

	if s.now().After(expiresAt) {
		return Operator{}, ErrInvalidSession
	}

	return operator, nil
}
