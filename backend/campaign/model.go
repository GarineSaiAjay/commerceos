// Package campaign implements the campaign orchestrator: merchant-side
// promotional discount campaigns proposed from observed rejected
// cross-sell demand (backend/growth/'s recommendations table), gated by
// a deterministic policy engine, and approved by an operator before
// they can affect checkout. Mirrors backend/policy/'s file layout and
// "deterministic gate, never the LLM" posture.
package campaign

import "time"

// PolicyVersion tags the campaign proposal/approval policy logic
// (engine.go), using the same "<domain>_policy_v<N>" scheme as
// policy.PolicyVersion and growth.PolicyVersion.
const PolicyVersion = "campaign_policy_v1"

// Campaign lifecycle statuses.
const (
	StatusProposed  = "PROPOSED"
	StatusApproved  = "APPROVED"
	StatusRejected  = "REJECTED"
	StatusActive    = "ACTIVE"
	StatusCompleted = "COMPLETED"
	StatusExpired   = "EXPIRED"
)

// Check names -- each is a discrete, independently testable check,
// matching policy.CheckMerchantAllowlisted's style.
const (
	CheckDiscountPercentBounded = "discount_percent_bounded"
	CheckBudgetCapBounded       = "budget_cap_bounded"
	CheckMerchantBudgetCeiling  = "merchant_budget_ceiling"
	CheckDurationBounded        = "duration_bounded"
	CheckProductAllowlisted     = "product_allowlisted"
	CheckSufficientDemand       = "sufficient_rejected_demand"
)

// Decision outcomes for a campaign proposal moving past PROPOSED.
const (
	DecisionApproved = "APPROVED"
	DecisionRejected = "REJECTED"
)

// Campaign is a merchant-side promotional discount campaign proposed
// from observed rejected cross-sell demand. All money amounts are
// paise, matching every other amount in this codebase.
type Campaign struct {
	ID                  string     `json:"campaign_id"`
	MerchantID          string     `json:"merchant_id"`
	ProductID           string     `json:"product_id"`
	DiscountPercent     int        `json:"discount_percent"`
	BudgetCap           int64      `json:"budget_cap"`
	Spent               int64      `json:"spent"`
	DurationDays        int        `json:"duration_days"`
	StartsAt            *time.Time `json:"starts_at,omitempty"`
	EndsAt              *time.Time `json:"ends_at,omitempty"`
	Status              string     `json:"status"`
	PolicyVersion       string     `json:"policy_version"`
	RejectedDemandCount int        `json:"rejected_demand_count"`
	Reasoning           string     `json:"reasoning"`
	ApprovedBy          string     `json:"approved_by,omitempty"`
	RejectedReason      string     `json:"rejected_reason,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// Config holds the deterministic policy knobs -- mirrors
// policy.PolicyConfig / policy.DefaultConfig(). Demo-scoped defaults,
// same posture as policy.DefaultConfig's own comment about this
// project's single-merchant scope.
type Config struct {
	MaxDiscountPercent int
	// MaxBudgetCapPerCampaign bounds any single campaign's spend.
	MaxBudgetCapPerCampaign int64
	// MaxTotalActiveBudget bounds the SUM of budget_cap across every one
	// of a merchant's simultaneously ACTIVE campaigns, not just this one
	// -- a merchant can't get around the per-campaign cap by running
	// many campaigns at once.
	MaxTotalActiveBudget int64
	MaxDurationDays      int
	AllowedProducts      []string
	// MinRejectedDemandCount is the floor below which a proposal is
	// rejected as "not enough observed demand to justify a campaign" --
	// keeps the agent from proposing a campaign off a single rejection.
	MinRejectedDemandCount int
}

func DefaultConfig() Config {
	return Config{
		MaxDiscountPercent:      30,
		MaxBudgetCapPerCampaign: 500_000,   // ₹5,000
		MaxTotalActiveBudget:    2_000_000, // ₹20,000 across all active campaigns
		MaxDurationDays:         14,
		// AllowedProducts previously only listed the first 4 of what is
		// now a 10-product catalog, so the campaign agent could never
		// propose a merchant discount against the other 6 products even
		// when demand data justified one. Same staleness bug as the one
		// fixed in policy/model.go and growth/simulator.go (this list
		// has no dynamic catalog lookup either -- see engine.go's
		// checkProductAllowlisted) -- kept in sync with
		// policy.DefaultConfig().AllowedProducts by hand.
		AllowedProducts: []string{
			"airpods-pro-2",
			"airpods-case",
			"applecare",
			"usb-c-adapter",
			"wireless-charging-pad",
			"airpods-max",
			"airpods-3",
			"magsafe-charger",
			"lightning-usbc-cable",
			"airpods-eartips",
		},
		MinRejectedDemandCount: 3,
	}
}

// CheckResult mirrors policy.CheckResult.
type CheckResult struct {
	Name   string
	Passed bool
	Reason string
}

// Decision mirrors policy.Decision's shape for a campaign proposal --
// whether the proposal itself is allowed to leave PROPOSED status.
type Decision struct {
	Decision      string `json:"decision"`
	PolicyVersion string `json:"policy_version"`
	FailedCheck   string `json:"failed_check,omitempty"`
	Reason        string `json:"reason,omitempty"`
}
