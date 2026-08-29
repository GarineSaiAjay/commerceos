package campaign

import (
	"context"
	"fmt"
)

// Engine evaluates a proposed campaign against the deterministic policy
// checklist -- mirrors policy.Engine.Evaluate exactly: every check must
// pass, the first failure short-circuits with its specific reason. It
// never delegates to the LLM (discount_percent/duration_days are
// operator-supplied inputs to the caller, not something this engine or
// the CampaignAgent chooses on its own -- see agent.go).
type Engine struct {
	config Config
}

func NewEngine(config Config) *Engine {
	return &Engine{config: config}
}

// Evaluate runs every check against a not-yet-persisted campaign
// proposal. activeBudgetForMerchant is the sum of budget_cap across the
// merchant's other currently ACTIVE campaigns (fetched by the caller via
// Repository.SumActiveBudget) so Evaluate itself stays a pure function
// of its inputs -- no hidden repo read mid-check a unit test wouldn't
// see coming, same discipline as policy.Engine.Evaluate.
func (e *Engine) Evaluate(
	ctx context.Context,
	c Campaign,
	activeBudgetForMerchant int64,
) Decision {
	checks := []CheckResult{
		e.checkDiscountPercent(c.DiscountPercent),
		e.checkBudgetCap(c.BudgetCap),
		e.checkMerchantBudgetCeiling(c.BudgetCap, activeBudgetForMerchant),
		e.checkDuration(c.DurationDays),
		e.checkProductAllowlisted(c.ProductID),
		e.checkSufficientDemand(c.RejectedDemandCount),
	}

	for _, check := range checks {
		if !check.Passed {
			return Decision{
				Decision:      DecisionRejected,
				PolicyVersion: PolicyVersion,
				FailedCheck:   check.Name,
				Reason:        check.Reason,
			}
		}
	}

	return Decision{Decision: DecisionApproved, PolicyVersion: PolicyVersion}
}

func (e *Engine) checkDiscountPercent(pct int) CheckResult {
	if pct > 0 && pct <= e.config.MaxDiscountPercent {
		return CheckResult{Name: CheckDiscountPercentBounded, Passed: true}
	}
	return CheckResult{
		Name:   CheckDiscountPercentBounded,
		Reason: fmt.Sprintf("discount %d%% exceeds max allowed %d%%", pct, e.config.MaxDiscountPercent),
	}
}

func (e *Engine) checkBudgetCap(cap int64) CheckResult {
	if cap > 0 && cap <= e.config.MaxBudgetCapPerCampaign {
		return CheckResult{Name: CheckBudgetCapBounded, Passed: true}
	}
	return CheckResult{
		Name:   CheckBudgetCapBounded,
		Reason: fmt.Sprintf("budget cap %d exceeds max %d", cap, e.config.MaxBudgetCapPerCampaign),
	}
}

func (e *Engine) checkMerchantBudgetCeiling(cap int64, activeTotal int64) CheckResult {
	if activeTotal+cap <= e.config.MaxTotalActiveBudget {
		return CheckResult{Name: CheckMerchantBudgetCeiling, Passed: true}
	}
	return CheckResult{
		Name: CheckMerchantBudgetCeiling,
		Reason: fmt.Sprintf(
			"merchant's total active campaign budget %d + this campaign's %d exceeds ceiling %d",
			activeTotal, cap, e.config.MaxTotalActiveBudget,
		),
	}
}

func (e *Engine) checkDuration(days int) CheckResult {
	if days > 0 && days <= e.config.MaxDurationDays {
		return CheckResult{Name: CheckDurationBounded, Passed: true}
	}
	return CheckResult{
		Name:   CheckDurationBounded,
		Reason: fmt.Sprintf("duration %d days exceeds max %d days", days, e.config.MaxDurationDays),
	}
}

// checkProductAllowlisted uses a static config allowlist for MVP,
// matching policy.PolicyConfig.AllowedProducts's exact pattern, rather
// than a DB-backed per-merchant allowlist table. Whether the product
// actually EXISTS in the catalog is a separate check made at the agent
// layer (agent.go), which has a CatalogReader to ask.
func (e *Engine) checkProductAllowlisted(productID string) CheckResult {
	for _, p := range e.config.AllowedProducts {
		if p == productID {
			return CheckResult{Name: CheckProductAllowlisted, Passed: true}
		}
	}
	return CheckResult{
		Name:   CheckProductAllowlisted,
		Reason: fmt.Sprintf("product %s is not allowlisted for campaigns", productID),
	}
}

func (e *Engine) checkSufficientDemand(count int) CheckResult {
	if count >= e.config.MinRejectedDemandCount {
		return CheckResult{Name: CheckSufficientDemand, Passed: true}
	}
	return CheckResult{
		Name:   CheckSufficientDemand,
		Reason: fmt.Sprintf("only %d rejected recommendations observed, below minimum %d", count, e.config.MinRejectedDemandCount),
	}
}
