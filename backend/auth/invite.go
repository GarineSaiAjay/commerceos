package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

// This file adds item 40 (PLAN-05-SELLER-DASHBOARD.md §7 / PLAN-06's
// phasing table: "Multi-operator invite flow, 2-3 days, medium-high,
// security-sensitive") on top of the single-operator model documented
// in files/AUTH.md. It deliberately does not send any email -- this
// project has no outbound mail integration and none was added for this
// item (adding one would be its own separate, judgeable scope). Instead
// InviteOperator hands the raw invite token back to the *inviting*
// operator (in the HTTP response, exactly once, the same way Login
// hands back a session token), and the dashboard is expected to show it
// as a copyable link for the operator to share out-of-band -- see
// frontend/app/dashboard/settings/team.tsx.

// InviteTTL is how long an invite link stays redeemable before it must
// be reissued by another InviteOperator call.
const InviteTTL = 7 * 24 * time.Hour

// minInvitedPasswordLength is enforced only in AcceptInvite. Login never
// validates password strength -- by the time Login runs, a password is
// already hashed and stored, so there's nothing left to floor. Accepting
// an invite is the one place in this package a brand-new credential is
// actually chosen, so it's the one place a minimum is worth enforcing.
const minInvitedPasswordLength = 12

var ErrInvalidEmail = errors.New("a valid email is required")
var ErrInviteExpired = errors.New("this invite has expired")
var ErrInviteAlreadyAccepted = errors.New("this invite has already been used")
var ErrPasswordTooShort = fmt.Errorf("password must be at least %d characters", minInvitedPasswordLength)
var ErrCannotRemoveSelf = errors.New("cannot remove your own operator account")
var ErrCannotRemoveLastOperator = errors.New("cannot remove a merchant's last remaining operator account")

// InviteOperator lets an already-authenticated operator (inviter) invite
// a new teammate by email, scoped to the inviter's own merchant --
// there is no cross-merchant invite path. Returns the raw invite token;
// like a session token, only its SHA-256 hash is persisted
// (repo.CreateInvite), so this is the only place the caller can read it.
func (s *Service) InviteOperator(ctx context.Context, inviter Operator, email string) (token string, invite Invite, err error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return "", Invite{}, ErrInvalidEmail
	}

	// Fail fast with a clear error rather than letting the invitee
	// discover the conflict later at AcceptInvite -- the inviting
	// operator is in a much better position to know "oh, they already
	// have an account" than someone who just clicked a link.
	if _, getErr := s.repo.GetOperatorByEmail(ctx, email); getErr == nil {
		return "", Invite{}, ErrEmailAlreadyRegistered
	} else if !errors.Is(getErr, ErrOperatorNotFound) {
		return "", Invite{}, getErr
	}

	id, err := NewRandomID("invite_")
	if err != nil {
		return "", Invite{}, err
	}
	token, tokenHash, err := generateInviteToken()
	if err != nil {
		return "", Invite{}, err
	}

	invite = Invite{
		ID:         id,
		MerchantID: inviter.MerchantID,
		Email:      email,
		InvitedBy:  inviter.ID,
		ExpiresAt:  s.now().Add(InviteTTL),
	}

	if err := s.repo.CreateInvite(ctx, invite, tokenHash); err != nil {
		return "", Invite{}, err
	}

	return token, invite, nil
}

// ListInvites returns every invite (pending and accepted) for
// merchantID, for the dashboard's team-management view.
func (s *Service) ListInvites(ctx context.Context, merchantID string) ([]Invite, error) {
	return s.repo.ListInvites(ctx, merchantID)
}

// ListOperators returns every operator on merchantID's account.
func (s *Service) ListOperators(ctx context.Context, merchantID string) ([]OperatorRecord, error) {
	return s.repo.ListOperators(ctx, merchantID)
}

// RevokeInvite deletes a still-pending invite, scoped to the calling
// operator's own merchant. An invite that's already been accepted,
// doesn't exist, or belongs to a different merchant all report the same
// ErrInviteNotFound -- there's nothing actionable to tell those apart
// for the caller, and an operator has no legitimate reason to probe
// which one it is.
func (s *Service) RevokeInvite(ctx context.Context, operator Operator, inviteID string) error {
	n, err := s.repo.DeleteInvite(ctx, inviteID, operator.MerchantID)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrInviteNotFound
	}
	return nil
}

// RemoveOperator deletes another operator from the caller's own
// merchant. Two guards keep a merchant from ever locking itself out:
// an operator can't remove themselves (use a teammate's session, or
// just don't log back in, to step down), and the merchant's last
// remaining operator can never be removed at all (there would be no
// account left to perform the removal, or to log into the dashboard
// afterward). Neither guard existed before this item because there was
// never more than one operator to remove.
func (s *Service) RemoveOperator(ctx context.Context, operator Operator, targetOperatorID string) error {
	if targetOperatorID == operator.ID {
		return ErrCannotRemoveSelf
	}

	operators, err := s.repo.ListOperators(ctx, operator.MerchantID)
	if err != nil {
		return err
	}
	if len(operators) <= 1 {
		return ErrCannotRemoveLastOperator
	}

	n, err := s.repo.DeleteOperator(ctx, targetOperatorID, operator.MerchantID)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrOperatorNotFound
	}
	return nil
}

// AcceptInvite redeems a still-valid, still-pending invite: it creates
// the new operator account and, like Login, immediately issues a
// session for it -- accepting an invite should feel exactly like a
// successful login, not require a second round trip back through
// /auth/login.
func (s *Service) AcceptInvite(ctx context.Context, token, password string) (sessionToken string, operator Operator, err error) {
	if token == "" {
		return "", Operator{}, ErrInviteNotFound
	}
	if len(password) < minInvitedPasswordLength {
		return "", Operator{}, ErrPasswordTooShort
	}

	invite, err := s.repo.GetInviteByTokenHash(ctx, hashToken(token))
	if err != nil {
		return "", Operator{}, err
	}
	if invite.AcceptedAt != nil {
		return "", Operator{}, ErrInviteAlreadyAccepted
	}
	if s.now().After(invite.ExpiresAt) {
		return "", Operator{}, ErrInviteExpired
	}

	passwordHash, err := HashPassword(password)
	if err != nil {
		return "", Operator{}, err
	}

	operatorID, err := NewRandomID("operator_")
	if err != nil {
		return "", Operator{}, err
	}

	rec := OperatorRecord{
		ID:           operatorID,
		MerchantID:   invite.MerchantID,
		Email:        invite.Email,
		PasswordHash: passwordHash,
	}
	// CreateOperator re-checks the unique email constraint at the
	// database level (translated to ErrEmailAlreadyRegistered) -- the
	// window between InviteOperator's pre-check and here is small but
	// real (e.g. two invites for the same email both accepted
	// concurrently), so this is not a redundant check.
	if err := s.repo.CreateOperator(ctx, rec); err != nil {
		return "", Operator{}, err
	}

	// From here on the operator account is real and already usable via
	// POST /auth/login, so a failure in either remaining step is logged
	// rather than treated as if AcceptInvite itself failed -- the
	// invitee's story ("I set a password and got in") should not
	// degrade into a support request over something they can't see.
	if markErr := s.repo.MarkInviteAccepted(ctx, invite.ID, s.now()); markErr != nil {
		log.Printf("auth: mark invite %s accepted: %v", invite.ID, markErr)
	}

	sessionToken, tokenHash, err := generateSessionToken()
	if err != nil {
		log.Printf("auth: generate session token after invite accept for operator %s: %v", rec.ID, err)
		return "", Operator{ID: rec.ID, MerchantID: rec.MerchantID, Email: rec.Email}, nil
	}
	if createErr := s.repo.CreateSession(ctx, tokenHash, rec.ID, s.now().Add(SessionTTL)); createErr != nil {
		log.Printf("auth: create session after invite accept for operator %s: %v", rec.ID, createErr)
		return "", Operator{ID: rec.ID, MerchantID: rec.MerchantID, Email: rec.Email}, nil
	}

	return sessionToken, Operator{ID: rec.ID, MerchantID: rec.MerchantID, Email: rec.Email}, nil
}

// generateInviteToken mirrors generateSessionToken (password.go): a
// random 256-bit token, hex-encoded, with only its SHA-256 hash
// persisted.
func generateInviteToken() (token string, tokenHash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate invite token: %w", err)
	}
	token = hex.EncodeToString(raw)
	return token, hashToken(token), nil
}

// NewRandomID returns a short random id with prefix, using the same
// crypto/rand source as session/invite tokens. Not a secret itself --
// just a unique identifier -- but this environment has no database
// sequence or UUID library wired up for these two new tables, so a
// random suffix (rather than, say, a counter) is what's available
// without a new Go module. Exported (originally invite/operator-ID-only,
// package-private) so backend/policy can use the same unguessable,
// crypto/rand-backed generator for mandate IDs -- see its call site's
// doc comment for why a predictable ID there is a real vulnerability,
// not just a cosmetic one.
func NewRandomID(prefix string) (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate id for %s: %w", prefix, err)
	}
	return prefix + hex.EncodeToString(raw), nil
}
