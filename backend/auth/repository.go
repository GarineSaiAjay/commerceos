package auth

import (
	"context"
	"errors"
	"time"
)

var ErrOperatorNotFound = errors.New("operator not found")
var ErrSessionNotFound = errors.New("session not found")

// ErrInviteNotFound covers both "no such token" and "already used" --
// the repository layer doesn't distinguish an unknown token from an
// accepted one; Service.AcceptInvite does that distinction itself once
// it has the row (see invite.go), so a caller here never learns which
// case it is from the error alone.
var ErrInviteNotFound = errors.New("invite not found")

// ErrEmailAlreadyRegistered is returned by CreateOperator when the
// email is already taken -- operators.email is UNIQUE across the whole
// table (not just per-merchant), so this can happen for an invite to an
// email that already has an operator account on a *different*
// merchant, not just a duplicate invite on the same one.
var ErrEmailAlreadyRegistered = errors.New("an operator with this email already exists")

// Repository persists operator accounts, their bearer sessions, and
// (item 40) pending invitations for new operators.
type Repository interface {
	GetOperatorByEmail(ctx context.Context, email string) (OperatorRecord, error)
	CreateSession(ctx context.Context, tokenHash string, operatorID string, expiresAt time.Time) error
	GetSession(ctx context.Context, tokenHash string) (Operator, time.Time, error)
	DeleteSession(ctx context.Context, tokenHash string) error

	// ListOperators returns every operator on merchantID's account,
	// oldest first. Used by the dashboard's team-management view and by
	// Service's last-operator-standing check before a removal.
	ListOperators(ctx context.Context, merchantID string) ([]OperatorRecord, error)
	// CreateOperator inserts a new operator row. Returns
	// ErrEmailAlreadyRegistered if rec.Email is already taken.
	CreateOperator(ctx context.Context, rec OperatorRecord) error
	// DeleteOperator removes operatorID's row, scoped to merchantID so
	// one merchant's operator can never delete another merchant's
	// account by ID alone. Cascades to that operator's sessions
	// (operator_sessions.operator_id has ON DELETE CASCADE). Returns the
	// number of rows deleted (0 or 1) so the caller can tell "not found /
	// not yours" from "removed".
	DeleteOperator(ctx context.Context, operatorID string, merchantID string) (int64, error)

	// CreateInvite persists a new invite, storing tokenHash rather than
	// the raw token (invite.ID is expected to already be set by the
	// caller, same convention as CreateOperator).
	CreateInvite(ctx context.Context, invite Invite, tokenHash string) error
	// GetInviteByTokenHash looks up an invite by its token's hash.
	// Returns ErrInviteNotFound if no invite has that hash.
	GetInviteByTokenHash(ctx context.Context, tokenHash string) (Invite, error)
	// ListInvites returns every invite (pending and accepted) for
	// merchantID, newest first.
	ListInvites(ctx context.Context, merchantID string) ([]Invite, error)
	// MarkInviteAccepted stamps an invite's accepted_at, so it can never
	// be redeemed a second time.
	MarkInviteAccepted(ctx context.Context, inviteID string, acceptedAt time.Time) error
	// DeleteInvite revokes a still-pending invite, scoped to merchantID
	// for the same reason DeleteOperator is. Returns the number of rows
	// deleted (0 or 1).
	DeleteInvite(ctx context.Context, inviteID string, merchantID string) (int64, error)
}
