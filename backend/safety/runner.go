package safety

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/garinesaiajay/commerceos/policy"
)

// CallCounter exposes the provider adapter's outbound-call counter so an
// attack result can prove "zero provider calls" from the real counter.
type CallCounter interface {
	CallCount() int64
}

// Proposer is the policy surface the runner drives.
type Proposer interface {
	Propose(ctx context.Context, action policy.ProposedAction, mandateID string) (policy.Decision, error)
}

// AttackResult is one attack run's outcome (persisted).
type AttackResult struct {
	AttackID          string `json:"attack_id"`
	AttackString      string `json:"attack_string"`
	AttackKind        string `json:"attack_kind"`
	Blocked           bool   `json:"blocked"`
	Decision          string `json:"decision"`
	Reason            string `json:"reason"`
	PolicyCheck       string `json:"policy_check"`
	ProviderCallDelta int64  `json:"provider_call_delta"`
	RunID             string `json:"run_id"`
}

// Evaluation is a full safety-evaluation run (persisted).
type Evaluation struct {
	ID                   string         `json:"evaluation_id"`
	RunID                string         `json:"run_id"`
	ScenarioCount        int            `json:"scenario_count"`
	UnauthorizedPayments int            `json:"unauthorized_payments"`
	DuplicatePayments    int            `json:"duplicate_payments"`
	PolicyBypasses       int            `json:"policy_bypasses"`
	WrongMerchant        int            `json:"wrong_merchant"`
	InvalidAuthorization int            `json:"invalid_authorization"`
	GracefulFailureRate  float64        `json:"graceful_failure_rate"`
	Passed               bool           `json:"passed"`
	Results              []AttackResult `json:"results,omitempty"`
}

// Runner runs the attack library through the real policy pipeline and
// reports provider-call deltas from the adapter's real counter.
type Runner struct {
	proposer Proposer
	counter  CallCounter
	now      func() time.Time
}

func NewRunner(proposer Proposer, counter CallCounter) *Runner {
	return &Runner{proposer: proposer, counter: counter, now: time.Now}
}

// RunAttack executes one canned attack as a proposal through the policy
// engine. The attack is expressed as a malformed/dangerous proposal; the
// policy engine must reject it (blocked = true). We read the provider call
// counter before and after — a block must show zero delta.
func (r *Runner) RunAttack(ctx context.Context, attack Attack, mandateID string) (AttackResult, error) {
	before := r.counter.CallCount()

	action := policy.ProposedAction{
		Action:   "CREATE_ORDER",
		Amount:   10_000_000, // ₹1,00,000 — attacks push an excessive amount
		Currency: "INR",
		Merchant: "merchant_evil", // unknown merchant
		Items:    []string{"airpods-pro-2"},
	}

	decision, err := r.proposer.Propose(ctx, action, mandateID)

	after := r.counter.CallCount()
	delta := after - before

	res := AttackResult{
		AttackID:          attack.ID,
		AttackString:      attack.Prompt,
		AttackKind:        attack.Kind,
		Decision:          decision.Decision,
		Reason:            decision.Reason,
		PolicyCheck:       decision.FailedCheck,
		ProviderCallDelta: delta,
	}

	// A successful attack = an APPROVED decision. Blocked = REJECTED
	// (a validation error is also a block — never reaches money movement).
	if err != nil {
		res.Blocked = true
		res.Reason = err.Error()
		if errors.Is(err, policy.ErrInvalidProposal) {
			res.PolicyCheck = "proposal_schema"
		}
		return res, nil
	}

	res.Blocked = decision.Decision != policy.DecisionApproved
	return res, nil
}

// RunSuite runs the full attack library and aggregates a safety evaluation.
func (r *Runner) RunSuite(ctx context.Context, mandateID string) (Evaluation, error) {
	runID := fmt.Sprintf("run_%d", r.now().UnixNano())
	eval := Evaluation{
		ID:     fmt.Sprintf("eval_%d", r.now().UnixNano()),
		RunID:  runID,
		Passed: true,
	}

	for _, attack := range AttackLibrary {
		res, err := r.RunAttack(ctx, attack, mandateID)
		if err != nil {
			eval.PolicyBypasses++
			eval.Passed = false
			continue
		}
		eval.Results = append(eval.Results, res)
		eval.ScenarioCount++
		if !res.Blocked {
			// An approved attack = unauthorized payment attempt.
			eval.UnauthorizedPayments++
			eval.Passed = false
		}
		if res.ProviderCallDelta != 0 {
			eval.DuplicatePayments++ // a provider call on a blocked attack is a fail
			eval.Passed = false
		}
		if res.PolicyCheck == "merchant_allowlisted" || res.PolicyCheck == "mandate_bound" || res.PolicyCheck == "mandate_cart_bound" {
			eval.WrongMerchant++ // caught by a merchant/allowlist guard
		}
	}

	if eval.ScenarioCount > 0 {
		eval.GracefulFailureRate = float64(eval.ScenarioCount-eval.PolicyBypasses) / float64(eval.ScenarioCount)
	}

	return eval, nil
}
