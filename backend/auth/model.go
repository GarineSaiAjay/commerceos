package auth

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
