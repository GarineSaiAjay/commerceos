package policy

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ProductExistsFunc checks whether a product ID currently exists in the
// live catalog. Deliberately a plain function type, not an interface
// tied to the catalog package's types -- Engine must stay free of any
// catalog dependency (see checkProducts's doc comment and
// PolicyConfig.AllowedProducts's), so the only thing Engine knows about
// this func is its signature. WithProductExistsFunc wires a real one in
// production (backend/cmd/server/main.go, backed by
// catalog.Service.GetProduct); left nil, Engine falls back to
// config.AllowedProducts's static list exactly as it always has, which
// is what every existing test that constructs Engine via plain
// NewEngine still exercises.
type ProductExistsFunc func(ctx context.Context, productID string) (bool, error)

// Engine evaluates proposed actions against the deterministic policy
// checklist. It never delegates to the LLM.
//
// configMu guards config (item 25, ROADMAP-PRIORITIZED.md P2 /
// PLAN-05-SELLER-DASHBOARD.md §4): before this, config was set once at
// NewEngine and never touched again, so a bare struct field was safe.
// Now Service.UpdatePolicyConfig can call UpdateConfig concurrently
// with in-flight Evaluate calls from live checkouts, so every read/
// write goes through the mutex. Evaluate takes ONE snapshot (Config())
// at the top of the call and threads that same copy through every
// check for the rest of that call -- not a fresh e.config read per
// check -- so a single proposal is always evaluated against one
// consistent config, never a torn mix of pre- and post-update values
// if an operator happens to save new settings mid-evaluation.
type Engine struct {
	configMu sync.RWMutex
	config   PolicyConfig
	repo     Repository
	now      func() time.Time
	products ProductExistsFunc
}

func NewEngine(config PolicyConfig, repo Repository) *Engine {
	return &Engine{
		config: config,
		repo:   repo,
		now:    time.Now,
	}
}

// Config returns a copy of the engine's current live configuration.
// Safe to call concurrently with Evaluate/UpdateConfig.
func (e *Engine) Config() PolicyConfig {
	e.configMu.RLock()
	defer e.configMu.RUnlock()
	return e.config
}

// UpdateConfig replaces the engine's live configuration, taking effect
// for every Evaluate call that starts after this returns. AllowedProducts
// is deliberately preserved from the PREVIOUS config, never taken from
// cfg -- it's a test/campaign-only fallback list today (superseded by a
// live catalog check via WithProductExistsFunc in production, see
// checkProducts's doc comment), not a real merchant-editable policy
// knob, so there is nothing for a caller to legitimately change here.
// Callers (policy.Service.UpdatePolicyConfig) are expected to persist
// cfg durably BEFORE calling this -- UpdateConfig itself only ever
// touches the in-memory copy Evaluate reads from.
func (e *Engine) UpdateConfig(cfg PolicyConfig) {
	e.configMu.Lock()
	defer e.configMu.Unlock()
	cfg.AllowedProducts = e.config.AllowedProducts
	e.config = cfg
}

// WithProductExistsFunc wires a live product-existence check, replacing
// the static config.AllowedProducts membership test in checkProducts.
// Optional and additive, same convention as every other WithX(...)
// fluent setter in this codebase (agents.BuyerAgent.WithConversationStore,
// agents.Handler.WithLoopAgent, payment.Handler.WithCallCounter, ...):
// existing callers that never call this keep the exact prior behavior.
func (e *Engine) WithProductExistsFunc(f ProductExistsFunc) *Engine {
	e.products = f
	return e
}

// Evaluate runs every check. All must pass for APPROVED; any single
// failure produces REJECTED with that specific reason. The risk score is
// an input to the level router, exactly as the spec requires.
func (e *Engine) Evaluate(
	ctx context.Context,
	action ProposedAction,
	mandate Mandate,
	riskScore float64,
) Decision {
	cfg := e.Config()

	checks := []CheckResult{
		e.checkMerchantAllowlisted(cfg, action.Merchant),
		e.checkCurrency(cfg, action.Currency),
		e.checkCeiling(cfg, action.Amount),
		e.checkProducts(ctx, cfg, action.Items),
		e.checkBudget(cfg, action.Amount, mandate),
		e.checkMandateExpired(mandate),
		e.checkMandateBound(action, mandate),
		e.checkMandateCartBound(action, mandate),
		e.checkUserConsent(mandate),
	}

	for _, c := range checks {
		if !c.Passed {
			return Decision{
				Decision:      DecisionRejected,
				PolicyVersion: PolicyVersion,
				FailedCheck:   c.Name,
				Reason:        c.Reason,
				RiskScore:     riskScore,
			}
		}
	}

	return Decision{
		Decision:      DecisionApproved,
		PolicyVersion: PolicyVersion,
		Level:         routeLevel(action.Amount, mandate, riskScore),
		RiskScore:     riskScore,
	}
}

func (e *Engine) checkMerchantAllowlisted(cfg PolicyConfig, merchant string) CheckResult {
	for _, m := range cfg.AllowedMerchants {
		if m == merchant {
			return CheckResult{Name: CheckMerchantAllowlisted, Passed: true}
		}
	}
	return CheckResult{Name: CheckMerchantAllowlisted, Reason: fmt.Sprintf("merchant %s is not allowlisted", merchant)}
}

func (e *Engine) checkCurrency(cfg PolicyConfig, currency string) CheckResult {
	for _, c := range cfg.AllowedCurrencies {
		if c == currency {
			return CheckResult{Name: CheckCurrencyAllowed, Passed: true}
		}
	}
	return CheckResult{Name: CheckCurrencyAllowed, Reason: fmt.Sprintf("currency %s is not allowed", currency)}
}

func (e *Engine) checkCeiling(cfg PolicyConfig, amount int64) CheckResult {
	if amount <= cfg.Ceiling {
		return CheckResult{Name: CheckAmountCeiling, Passed: true}
	}
	return CheckResult{Name: CheckAmountCeiling, Reason: fmt.Sprintf("amount %d exceeds ceiling %d", amount, cfg.Ceiling)}
}

// checkProducts confirms every item in a proposal is a real, currently
// sellable product. This has gone stale three times now with a purely
// static config.AllowedProducts list: first at only 4 SKUs
// (files/AUDIT-2026-08-29.md §3.2), again after airpods-pro-3/
// airtag-4pack/beats-fit-pro were added to the seed catalog without
// updating this list, and a third time -- this fix -- after
// ROADMAP-PRIORITIZED.md item 14 (frontend/app/dashboard/catalog/
// page.tsx) shipped a way to add real products at runtime, which no
// static list, hand-maintained or generated, can ever keep up with.
// e.products (wired in production via WithProductExistsFunc) checks
// the live catalog instead when set; the static list remains the
// fallback so Engine stays fully deterministic and dependency-free for
// every test and any deployment that never wires a live check --
// PolicyConfig.AllowedProducts's own doc comment documents that
// tradeoff in depth.
func (e *Engine) checkProducts(ctx context.Context, cfg PolicyConfig, items []string) CheckResult {
	for _, item := range items {
		ok, err := e.productExists(ctx, cfg, item)
		if err != nil {
			// Fail closed: an error means "could not verify," never
			// "assume it's fine." Payment authorization is exactly the
			// wrong place to fall back to optimistic behavior on an
			// infra error.
			return CheckResult{Name: CheckProductPermitted, Reason: fmt.Sprintf("could not verify product %s: %v", item, err)}
		}
		if !ok {
			return CheckResult{Name: CheckProductPermitted, Reason: fmt.Sprintf("product %s is not permitted", item)}
		}
	}
	return CheckResult{Name: CheckProductPermitted, Passed: true}
}

func (e *Engine) productExists(ctx context.Context, cfg PolicyConfig, productID string) (bool, error) {
	if e.products != nil {
		return e.products(ctx, productID)
	}
	for _, p := range cfg.AllowedProducts {
		if p == productID {
			return true, nil
		}
	}
	return false, nil
}

// checkBudget enforces the mandate ceiling with the configured tolerance.
// The tolerance is applied as a percentage of the mandate maximum so it
// matches the growth agent's BudgetCheck semantics (§5.1).
func (e *Engine) checkBudget(cfg PolicyConfig, amount int64, m Mandate) CheckResult {
	tolerance := int64(float64(m.MaximumAmount) * cfg.BudgetTolerance)
	if amount <= m.MaximumAmount+tolerance {
		return CheckResult{Name: CheckBudgetTolerance, Passed: true}
	}
	return CheckResult{Name: CheckBudgetTolerance, Reason: fmt.Sprintf("amount %d exceeds budget %d", amount, m.MaximumAmount)}
}

func (e *Engine) checkMandateExpired(m Mandate) CheckResult {
	if m.Status != "ACTIVE" {
		return CheckResult{Name: CheckMandateNotExpired, Reason: fmt.Sprintf("mandate status is %s", m.Status)}
	}
	expires, err := time.Parse(time.RFC3339, m.ExpiresAt)
	if err != nil {
		return CheckResult{Name: CheckMandateNotExpired, Reason: "mandate expiry unparseable"}
	}
	if e.now().After(expires) {
		return CheckResult{Name: CheckMandateNotExpired, Reason: "mandate has expired"}
	}
	return CheckResult{Name: CheckMandateNotExpired, Passed: true}
}

// checkMandateBound verifies the mandate is bound to the merchant/currency
// end-to-end (merchant swap, currency drift).
func (e *Engine) checkMandateBound(action ProposedAction, m Mandate) CheckResult {
	if m.Merchant != action.Merchant {
		return CheckResult{Name: CheckMandateBound, Reason: fmt.Sprintf("mandate merchant %s != action merchant %s", m.Merchant, action.Merchant)}
	}
	if m.Currency != action.Currency {
		return CheckResult{Name: CheckMandateBound, Reason: "mandate currency mismatch"}
	}
	return CheckResult{Name: CheckMandateBound, Passed: true}
}

// checkMandateCartBound enforces the Mandate → Cart binding from the spec
// (§5.3): when a mandate is issued for a specific cart, the proposal must
// carry that same cart_id — otherwise the binding drifted. Generalized
// (cart-less) proposals are unaffected.
func (e *Engine) checkMandateCartBound(action ProposedAction, m Mandate) CheckResult {
	if m.CartID == "" {
		return CheckResult{Name: CheckMandateCartBound, Passed: true}
	}
	if action.CartID == "" {
		return CheckResult{Name: CheckMandateCartBound, Reason: fmt.Sprintf("mandate is bound to cart %s but the proposal does not reference a cart", m.CartID)}
	}
	if action.CartID != m.CartID {
		return CheckResult{Name: CheckMandateCartBound, Reason: fmt.Sprintf("mandate is bound to cart %s but the proposal references cart %s", m.CartID, action.CartID)}
	}
	return CheckResult{Name: CheckMandateCartBound, Passed: true}
}

// checkUserConsent requires an active mandate, which is the explicit
// consent record for the agent to act.
func (e *Engine) checkUserConsent(m Mandate) CheckResult {
	if m.Status != "ACTIVE" {
		return CheckResult{Name: CheckUserConsent, Reason: fmt.Sprintf("no active consent: mandate status is %s", m.Status)}
	}
	return CheckResult{Name: CheckUserConsent, Passed: true}
}

// routeLevel implements the three authorization levels as a function of
// (amount, merchant_trust, risk_score) — NOT a single amount threshold.
// Level 3 triggers on unknown merchant or high risk regardless of amount.
func routeLevel(amount int64, m Mandate, riskScore float64) int {
	// Level 3: unknown merchant or high risk regardless of amount.
	if !isTrustedMerchant(m.Merchant) {
		return 3
	}
	if riskScore >= 0.7 {
		return 3
	}

	// Level 1: auto-approve ≤ ₹1,000 (100_000 paise) with low risk.
	if amount <= 100_000 && riskScore < 0.3 {
		return 1
	}

	// Level 2: confirm ₹1,001 – ₹10,000 (100_001 – 1_000_000 paise).
	if amount <= 1_000_000 {
		return 2
	}

	// Level 3: hard gate > ₹10,000 (1_000_000 paise).
	return 3
}

func isTrustedMerchant(merchant string) bool {
	return merchant == "merchant_001"
}
