package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// SessionTTL is how long an operator's bearer token stays valid after
// login.
const SessionTTL = 24 * time.Hour

// Login lockout: POST /auth/login previously had no defense at all
// against repeated password guesses -- an unauthenticated caller could
// hammer it as fast as the network allowed. After maxLoginAttempts
// consecutive failures for the same (normalized) email, further
// attempts for that email are rejected with ErrTooManyAttempts for
// lockoutDuration, without even touching the repository or the
// password hasher.
//
// This is deliberately the simplest lockout that closes the gap, not a
// production-grade one: it's in-memory and per-process (a restart, or
// a second replica behind a load balancer, resets/bypasses it), and
// it's keyed by email rather than by client IP, which means an
// attacker who already knows an operator's email can grief that
// operator by repeatedly failing their login on purpose to lock them
// out for lockoutDuration. That trade-off is accepted here: it still
// stops the far more common case (an attacker with no valid IP
// affinity blindly guessing passwords against one known email) that
// had zero mitigation before, and a buildathon-scale single-operator
// deployment doesn't have the "second replica" problem in the first
// place. A production deployment would want a shared store (Redis) and
// IP-based limiting alongside this, not instead of it.
const (
	maxLoginAttempts = 5
	lockoutDuration  = 15 * time.Minute
)

var ErrInvalidCredentials = errors.New("invalid email or password")
var ErrInvalidSession = errors.New("invalid or expired session")
var ErrTooManyAttempts = errors.New("too many failed login attempts, try again later")

type loginAttemptState struct {
	failures    int
	lockedUntil time.Time
}

type Service struct {
	repo Repository
	now  func() time.Time

	mu       sync.Mutex
	attempts map[string]*loginAttemptState
}

func NewService(repo Repository) *Service {
	return &Service{
		repo:     repo,
		now:      time.Now,
		attempts: make(map[string]*loginAttemptState),
	}
}

// Login verifies email/password and, on success, issues a new bearer
// session token. The same ErrInvalidCredentials is returned whether the
// email is unknown or the password is wrong, so a caller can't use this
// endpoint to enumerate registered operator emails.
func (s *Service) Login(ctx context.Context, email, password string) (token string, operator Operator, err error) {
	// Normalize case/whitespace before lookup: emails are stored
	// lowercase (see db/seeds/002_operator.sql), and Postgres TEXT
	// equality is case-sensitive, so "Owner@..." or a trailing space
	// picked up from a copy-paste would otherwise fail identically to a
	// wrong password, with no way to tell the two apart from the UI.
	email = strings.ToLower(strings.TrimSpace(email))

	if s.isLockedOut(email) {
		return "", Operator{}, ErrTooManyAttempts
	}

	record, err := s.repo.GetOperatorByEmail(ctx, email)
	if err != nil {
		s.recordFailedAttempt(email)
		return "", Operator{}, ErrInvalidCredentials
	}

	if !VerifyPassword(password, record.PasswordHash) {
		s.recordFailedAttempt(email)
		return "", Operator{}, ErrInvalidCredentials
	}

	token, tokenHash, err := generateSessionToken()
	if err != nil {
		return "", Operator{}, err
	}

	if err := s.repo.CreateSession(ctx, tokenHash, record.ID, s.now().Add(SessionTTL)); err != nil {
		return "", Operator{}, err
	}

	s.clearAttempts(email)

	return token, Operator{ID: record.ID, MerchantID: record.MerchantID, Email: record.Email}, nil
}

// isLockedOut reports whether email is currently locked out, clearing
// an expired lockout as a side effect so it doesn't linger in the map
// forever.
func (s *Service) isLockedOut(email string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.attempts[email]
	if !ok || state.lockedUntil.IsZero() {
		return false
	}
	if s.now().After(state.lockedUntil) {
		delete(s.attempts, email)
		return false
	}
	return true
}

// recordFailedAttempt counts a failed login attempt for email, locking
// it out for lockoutDuration once maxLoginAttempts is reached.
func (s *Service) recordFailedAttempt(email string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.attempts[email]
	if !ok {
		state = &loginAttemptState{}
		s.attempts[email] = state
	}
	state.failures++
	if state.failures >= maxLoginAttempts {
		state.lockedUntil = s.now().Add(lockoutDuration)
	}
}

// clearAttempts resets the failure count after a successful login.
func (s *Service) clearAttempts(email string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.attempts, email)
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
