package campaign

import (
	"context"
	"testing"
)

// baseCampaign returns a campaign that passes every check against
// DefaultConfig() -- each test below mutates exactly one field so a
// failure is attributable to the one check under test, same style as
// policy/policy_test.go's scenario tests.
func baseCampaign() Campaign {
	return Campaign{
		ID:                  "campaign_test",
		MerchantID:          "merchant_001",
		ProductID:           "airpods-case",
		DiscountPercent:     15,
		BudgetCap:           100_000,
		DurationDays:        7,
		RejectedDemandCount: 5,
	}
}

func TestEngineApprovesWhenAllChecksPass(t *testing.T) {
	e := NewEngine(DefaultConfig())
	d := e.Evaluate(context.Background(), baseCampaign(), 0)
	if d.Decision != DecisionApproved {
		t.Fatalf("got decision %q, reason %q, want APPROVED", d.Decision, d.Reason)
	}
	if d.PolicyVersion != PolicyVersion {
		t.Fatalf("got policy version %q, want %q", d.PolicyVersion, PolicyVersion)
	}
}

func TestEngineRejectsDiscountPercentTooHigh(t *testing.T) {
	e := NewEngine(DefaultConfig())
	c := baseCampaign()
	c.DiscountPercent = 50 // > DefaultConfig().MaxDiscountPercent (30)

	d := e.Evaluate(context.Background(), c, 0)
	if d.Decision != DecisionRejected || d.FailedCheck != CheckDiscountPercentBounded {
		t.Fatalf("got decision=%q failedCheck=%q, want REJECTED/%s", d.Decision, d.FailedCheck, CheckDiscountPercentBounded)
	}
}

func TestEngineRejectsBudgetCapTooHigh(t *testing.T) {
	e := NewEngine(DefaultConfig())
	c := baseCampaign()
	c.BudgetCap = 1_000_000 // > DefaultConfig().MaxBudgetCapPerCampaign (500_000)

	d := e.Evaluate(context.Background(), c, 0)
	if d.Decision != DecisionRejected || d.FailedCheck != CheckBudgetCapBounded {
		t.Fatalf("got decision=%q failedCheck=%q, want REJECTED/%s", d.Decision, d.FailedCheck, CheckBudgetCapBounded)
	}
}

func TestEngineRejectsWhenMerchantActiveBudgetCeilingExceeded(t *testing.T) {
	e := NewEngine(DefaultConfig())
	c := baseCampaign()
	c.BudgetCap = 400_000 // under the per-campaign cap on its own

	// But the merchant already has 1_800_000 committed to other active
	// campaigns -- 1_800_000 + 400_000 > MaxTotalActiveBudget (2_000_000).
	d := e.Evaluate(context.Background(), c, 1_800_000)
	if d.Decision != DecisionRejected || d.FailedCheck != CheckMerchantBudgetCeiling {
		t.Fatalf("got decision=%q failedCheck=%q, want REJECTED/%s", d.Decision, d.FailedCheck, CheckMerchantBudgetCeiling)
	}
}

func TestEngineAllowsMerchantActiveBudgetCeilingWithHeadroom(t *testing.T) {
	e := NewEngine(DefaultConfig())
	c := baseCampaign()
	c.BudgetCap = 100_000

	d := e.Evaluate(context.Background(), c, 1_800_000) // 1_800_000+100_000 <= 2_000_000
	if d.Decision != DecisionApproved {
		t.Fatalf("got decision=%q reason=%q, want APPROVED", d.Decision, d.Reason)
	}
}

func TestEngineRejectsDurationTooLong(t *testing.T) {
	e := NewEngine(DefaultConfig())
	c := baseCampaign()
	c.DurationDays = 30 // > DefaultConfig().MaxDurationDays (14)

	d := e.Evaluate(context.Background(), c, 0)
	if d.Decision != DecisionRejected || d.FailedCheck != CheckDurationBounded {
		t.Fatalf("got decision=%q failedCheck=%q, want REJECTED/%s", d.Decision, d.FailedCheck, CheckDurationBounded)
	}
}

func TestEngineRejectsProductNotAllowlisted(t *testing.T) {
	e := NewEngine(DefaultConfig())
	c := baseCampaign()
	c.ProductID = "some-unlisted-product"

	d := e.Evaluate(context.Background(), c, 0)
	if d.Decision != DecisionRejected || d.FailedCheck != CheckProductAllowlisted {
		t.Fatalf("got decision=%q failedCheck=%q, want REJECTED/%s", d.Decision, d.FailedCheck, CheckProductAllowlisted)
	}
}

func TestEngineRejectsInsufficientDemand(t *testing.T) {
	e := NewEngine(DefaultConfig())
	c := baseCampaign()
	c.RejectedDemandCount = 1 // < DefaultConfig().MinRejectedDemandCount (3)

	d := e.Evaluate(context.Background(), c, 0)
	if d.Decision != DecisionRejected || d.FailedCheck != CheckSufficientDemand {
		t.Fatalf("got decision=%q failedCheck=%q, want REJECTED/%s", d.Decision, d.FailedCheck, CheckSufficientDemand)
	}
}
