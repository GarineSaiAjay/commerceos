package policy

import (
	"errors"
	"fmt"
)

// PolicyVersion tags the deterministic checkout/authorization policy
// logic (merchant/currency/ceiling/product/budget/mandate checks in
// engine.go). Bump whenever that logic changes. Named to the same
// "<domain>_policy_v<N>" scheme as growth.PolicyVersion so the two
// policies -- what's allowed to be authorized vs. what's recommended --
// are clearly distinct, intentionally versioned identifiers rather than
// an accidental "v1" vs "cross_sell_policy_v4" inconsistency.
const PolicyVersion = "checkout_policy_v1"

// Decision outcomes.
const (
	DecisionApproved = "APPROVED"
	DecisionRejected = "REJECTED"
	// DecisionPendingApproval is returned for Level 2/3 proposals that
	// require durable human approval before an authorization is issued.
	DecisionPendingApproval = "PENDING_HUMAN_APPROVAL"
)

// Check names — each is a discrete, independently testable check.
const (
	CheckMerchantAllowlisted = "merchant_allowlisted"
	CheckCurrencyAllowed     = "currency_allowed"
	CheckAmountCeiling       = "amount_ceiling"
	CheckProductPermitted    = "product_permitted"
	CheckBudgetTolerance     = "budget_tolerance"
	CheckNoDuplicate         = "no_duplicate"
	CheckUserConsent         = "user_consent"
	CheckMandateNotExpired   = "mandate_not_expired"
	CheckMandateBound        = "mandate_bound"
	CheckMandateCartBound    = "mandate_cart_bound"
)

// ProposedAction is the canonical shape every agent must use.
type ProposedAction struct {
	Action   string   `json:"action"`
	Amount   int64    `json:"amount"`
	Currency string   `json:"currency"`
	Merchant string   `json:"merchant"`
	Items    []string `json:"items"`
	// CartID binds the proposal to a specific cart when present; the
	// policy engine verifies it matches the mandate's cart binding.
	CartID string `json:"cart_id,omitempty"`
}

// Mandate is the authorization object bound to a cart.
type Mandate struct {
	ID                        string   `json:"mandate_id"`
	Buyer                     string   `json:"buyer"`
	Merchant                  string   `json:"merchant"`
	AllowedCategories         []string `json:"allowed_categories"`
	MaximumAmount             int64    `json:"maximum_amount"`
	Currency                  string   `json:"currency"`
	RequiresConfirmationAbove int64    `json:"requires_confirmation_above"`
	AllowedPaymentMethods     []string `json:"allowed_payment_methods"`
	ExpiresAt                 string   `json:"expires_at"`
	Purpose                   string   `json:"purpose"`
	Status                    string   `json:"status"`
	CartID                    string   `json:"cart_id"`
}

// PolicyConfig holds the deterministic policy knobs.
type PolicyConfig struct {
	AllowedMerchants  []string
	AllowedCurrencies []string
	Ceiling           int64
	AllowedProducts   []string
	// BudgetTolerance is a fractional tolerance (0.10 = +10%) applied to
	// the mandate maximum, matching the growth agent's BudgetCheck.
	BudgetTolerance float64
}

func DefaultConfig() PolicyConfig {
	return PolicyConfig{
		AllowedMerchants:  []string{"merchant_001"},
		AllowedCurrencies: []string{"INR"},
		// All monetary amounts are paise; this is a ₹30,000 ceiling.
		Ceiling:         3_000_000,
		AllowedProducts: []string{"airpods-pro-2", "airpods-case", "applecare", "usb-c-adapter"},
		BudgetTolerance: 0,
	}
}

// CheckResult is the outcome of a single policy check.
type CheckResult struct {
	Name   string
	Passed bool
	Reason string
}

// Decision is the full policy decision output. JSON keys are lowercase
// snake_case (the conventional API shape).
type Decision struct {
	Decision          string  `json:"decision"`
	PolicyVersion     string  `json:"policy_version"`
	AuthorizationID   string  `json:"authorization_id"`
	ApprovalRequestID string  `json:"approval_request_id"`
	ExpiresAt         string  `json:"expires_at"`
	Level             int     `json:"level"`
	RiskScore         float64 `json:"risk_score"`
	FailedCheck       string  `json:"failed_check"`
	Reason            string  `json:"reason"`
	// ActionID is the run_id GET /runs/{id} takes -- the caller's own
	// audit trail (proposed -> risk-assessed -> policy-evaluated ->
	// authorized), regardless of whether this proposal auto-approved or
	// went through Level 2/3 human approval first.
	ActionID string `json:"action_id,omitempty"`
}

var ErrInvalidProposal = errors.New("invalid proposal")

// ValidateProposal strictly validates the canonical shape.
func ValidateProposal(p ProposedAction) error {
	if p.Action == "" {
		return fmt.Errorf("%v: action required", ErrInvalidProposal)
	}
	if p.Amount <= 0 {
		return fmt.Errorf("%v: amount must be positive", ErrInvalidProposal)
	}
	if p.Currency == "" {
		return fmt.Errorf("%v: currency required", ErrInvalidProposal)
	}
	if p.Merchant == "" {
		return fmt.Errorf("%v: merchant required", ErrInvalidProposal)
	}
	if len(p.Items) == 0 {
		return fmt.Errorf("%v: items required", ErrInvalidProposal)
	}
	return nil
}
