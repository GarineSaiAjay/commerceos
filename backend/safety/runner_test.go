package safety

import (
	"context"
	"testing"

	"github.com/garinesaiajay/commerceos/policy"
)

// fakeProposer always rejects proposals (like an evil-merchant attack would).
type fakeProposer struct{}

func (fakeProposer) Propose(ctx context.Context, action policy.ProposedAction, mandateID string) (policy.Decision, error) {
	return policy.Decision{
		Decision:    policy.DecisionRejected,
		FailedCheck: policy.CheckMerchantAllowlisted,
		Reason:      "merchant not allowlisted",
	}, nil
}

type fakeCounter struct{ n int64 }

func (c *fakeCounter) CallCount() int64 { return c.n }

// TestRunAttackBlocked proves an attack is blocked with zero provider calls.
func TestRunAttackBlocked(t *testing.T) {
	runner := NewRunner(fakeProposer{}, &fakeCounter{n: 3})
	attack, _ := GetAttack("att_01")
	res, err := runner.RunAttack(context.Background(), attack, "mnd_demo")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Blocked {
		t.Fatalf("expected attack blocked, got %s", res.Decision)
	}
	if res.ProviderCallDelta != 0 {
		t.Fatalf("expected zero provider call delta, got %d", res.ProviderCallDelta)
	}
}

// TestRunSuitePasses proves the full suite reports zero failures when every
// attack is blocked with zero provider calls.
func TestRunSuitePasses(t *testing.T) {
	runner := NewRunner(fakeProposer{}, &fakeCounter{n: 0})
	eval, err := runner.RunSuite(context.Background(), "mnd_demo")
	if err != nil {
		t.Fatal(err)
	}
	if eval.UnauthorizedPayments != 0 || eval.DuplicatePayments != 0 || eval.PolicyBypasses != 0 {
		t.Fatalf("expected zero failures, got %+v", eval)
	}
	if !eval.Passed {
		t.Fatal("expected suite to pass")
	}
	if eval.ScenarioCount != len(AttackLibrary) {
		t.Fatalf("expected %d scenarios, got %d", len(AttackLibrary), eval.ScenarioCount)
	}
}

// TestRunAttackApprovedFails proves an approved (unblocked) attack fails the
// suite as unauthorized — the runner must not paper over a bypass.
type approveProposer struct{}

func (approveProposer) Propose(ctx context.Context, action policy.ProposedAction, mandateID string) (policy.Decision, error) {
	return policy.Decision{Decision: policy.DecisionApproved}, nil
}

func TestRunAttackApprovedIsUnauthorized(t *testing.T) {
	runner := NewRunner(approveProposer{}, &fakeCounter{n: 0})
	eval, err := runner.RunSuite(context.Background(), "mnd_demo")
	if err != nil {
		t.Fatal(err)
	}
	if eval.UnauthorizedPayments == 0 {
		t.Fatal("expected unauthorized payment count > 0 when attacks are approved")
	}
	if eval.Passed {
		t.Fatal("expected suite to fail when attacks are approved")
	}
}
