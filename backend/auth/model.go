package auth

import "time"

// Operator is the authenticated identity attached to a request context
// once a valid session token has been verified.
type Operator struct {
	ID         string
	MerchantID string
	Email      string
}

// OperatorRecord is the persisted operator row, including the password
// hash -- never exposed outside the repository/service layer.
type OperatorRecord struct {
	ID           string
	MerchantID   string
	Email        string
	PasswordHash string
}

// Invite is a pending (or already-accepted) invitation for a new
// operator to join a merchant's account -- item 40. The raw invite
// token is never persisted, only its SHA-256 hash (see
// backend/auth/invite.go), mirroring how OperatorRecord's password hash
// and the session token hash are handled.
type Invite struct {
	ID         string
	MerchantID string
	Email      string
	InvitedBy  string // the inviting operator's ID
	ExpiresAt  time.Time
	AcceptedAt *time.Time // nil until AcceptInvite succeeds
}
