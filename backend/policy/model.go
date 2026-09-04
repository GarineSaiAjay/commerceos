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
		Ceiling: 3_000_000,
		// AllowedProducts is now only the FALLBACK product list --
		// backend/cmd/server/main.go wires Engine.WithProductExistsFunc
		// to check the live catalog instead, so a product added at
		// runtime (e.g. via frontend/app/dashboard/catalog/page.tsx,
		// item 14) is immediately purchasable without touching this
		// file. This list still matters for: any Engine constructed
		// without WithProductExistsFunc (every test in this package
		// does exactly that, deliberately, to keep policy tests free of
		// a live database), and campaign.DefaultConfig() (see
		// campaign/model.go), which reuses this exact slice for
		// campaign-eligibility gating and has not been given the same
		// live-catalog fix (lower stakes than blocking checkout --
		// tracked as a follow-up, not fixed here). Kept in sync with
		// db/seeds/001_catalog.sql for that reason, same as before --
		// this has already gone stale twice as a *checkout-blocking*
		// list: first when it only listed the original 4 SKUs
		// (files/AUDIT-2026-08-29.md §3.2), then again after
		// airpods-pro-3/airtag-4pack/beats-fit-pro were added and this
		// list wasn't ("product beats-fit-pro is not permitted"), and a
		// third time after item 14 shipped, which is what made a static
		// list -- hand-maintained or generated -- fundamentally
		// insufficient for checkout specifically, and prompted the live
		// check above. Regenerated a fourth time (mechanically, from
		// db/seeds/001_catalog.sql's actual product IDs) when the
		// catalog was expanded from 13 to 100 products -- campaign
		// eligibility and Engine-without-a-live-check tests would
		// otherwise have silently rejected 87 of the 100 real SKUs.
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
			"airpods-pro-3",
			"airtag-4pack",
			"beats-fit-pro",
			"airpods-2nd-gen",
			"airpods-4",
			"airpods-4-anc",
			"airpods-max-usbc",
			"earpods-usbc",
			"earpods-35mm",
			"beats-solo-buds",
			"beats-studio-buds-plus",
			"beats-powerbeats-pro-2",
			"beats-powerbeats-4",
			"beats-studio-pro",
			"beats-solo4",
			"beats-flex",
			"airpods-pro-2-refurb",
			"airpods-pro-2-clear",
			"beats-fit-pro-special",
			"airpods-max-refurb",
			"beats-powerbeats-pro",
			"usb-c-charger-20w",
			"usb-c-charger-30w",
			"usb-c-charger-35w-dual",
			"usb-c-charger-67w",
			"usb-c-charger-96w",
			"magsafe-battery-pack",
			"power-bank-20000",
			"power-bank-10000-magsafe",
			"car-charger-dual",
			"usbc-cable-braided-2m",
			"magsafe-stand-3in1",
			"wireless-charging-stand",
			"usbc-hub-charger-6in1",
			"usb-c-charger-140w",
			"charging-station-3in1",
			"solar-charger-portable",
			"usbc-extension-cable-3m",
			"wireless-charger-2pack",
			"airpods-pro-2-case",
			"airpods-3-case",
			"airpods-max-smart-case",
			"beats-fit-pro-eartips",
			"earbuds-cleaning-kit",
			"carrying-pouch-universal",
			"cable-organizer-set",
			"airpods-skin-wrap",
			"airtag-anti-lost-strap",
			"applecare-airpods",
			"applecare-macbook-1yr",
			"applecare-macbook-2yr",
			"microfiber-cleaning-cloth",
			"gift-wrap-service",
			"extended-warranty-3yr",
			"screen-cleaning-spray-kit",
			"protective-sleeve-universal",
			"travel-cable-case",
			"airpods-pro-2-eartips-large",
			"airtag-leather-case",
			"usbc-to-35mm-adapter",
			"anti-theft-cable-lock",
			"gift-card-sleeve",
			"airtag-single",
			"airtag-2pack",
			"airtag-loop-leather",
			"airtag-loop-sport",
			"airtag-wallet-card",
			"macbook-air-sleeve-13",
			"macbook-pro-sleeve-14",
			"macbook-pro-sleeve-16",
			"laptop-stand-aluminum",
			"laptop-stand-portable",
			"usbc-hub-7in1",
			"usbc-hub-11in1-dual-hdmi",
			"magic-keyboard",
			"magic-keyboard-numpad",
			"magic-mouse",
			"magic-trackpad",
			"usbc-laptop-charger-96w",
			"laptop-cooling-pad",
			"external-ssd-1tb",
			"external-ssd-2tb",
			"webcam-1080p-usbc",
			"laptop-privacy-filter-13",
			"laptop-privacy-filter-14",
			"laptop-backpack",
			"docking-station-thunderbolt",
			"macbook-sleeve-12",
			"usbc-to-hdmi-adapter",
			"laptop-screen-cleaning-kit",
		},
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
