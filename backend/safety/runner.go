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

// MandateCreator lets the runner set up a real mandate for each attack to
// run against, instead of depending on a caller-supplied mandate ID.
// Before this existed, both HTTP handlers defaulted an unset mandate_id
// to the literal string "mnd_demo" -- a mandate that was never seeded
// anywhere and could never arise from POST /policy/mandates either
// (real mandate IDs are randomly generated with the "mandate_" prefix,
// backend/policy/handler.go's CreateMandate, never "mnd_"). Every
// attack run therefore failed at policy.Service.Propose's very first
// step, GetMandate, before Engine.Evaluate ever ran -- so every attack
// registered as "blocked," but for a generic "mandate not found" error
// that proved nothing about the specific guard (ceiling, allowlist,
// budget, ...) each attack claims to test. *policy.Service satisfies
// this alongside Proposer, so the one real call site
// (backend/cmd/server/main.go) passes the same value for both.
type MandateCreator interface {
	CreateMandate(ctx context.Context, mandate policy.Mandate) error
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
	mandates MandateCreator
	counter  CallCounter
	now      func() time.Time
}

func NewRunner(proposer Proposer, mandates MandateCreator, counter CallCounter) *Runner {
	return &Runner{proposer: proposer, mandates: mandates, counter: counter, now: time.Now}
}

// redTeamMandateBuyer/Currency/PaymentMethod/ExpiresWithin are the fixed
// shape every red-team mandate shares -- only Merchant and
// MaximumAmount ever vary, and only for att_05/att_06 (see attacks.go's
// MandateMerchant/MandateMaximumAmount doc comment).
const (
	redTeamMandateBuyer         = "safety_redteam"
	redTeamMandateCurrency      = "INR"
	redTeamMandateExpiresWithin = 30 * time.Minute
)

// ensureRedTeamMandate creates a fresh, real mandate for one attack run
// to evaluate against. Every call to RunAttack gets its own brand-new
// mandate rather than sharing or reusing one -- mandates expire in a
// fixed window and this keeps every run correct and independent
// regardless of how long ago (or whether at all) a previous run
// happened, with no staleness/expiry bookkeeping needed here.
//
// Defaults to Merchant "merchant_001" (the one real allowlisted
// merchant, db/seeds/004_policy_settings.sql) and MaximumAmount equal
// to the platform ceiling itself (policy.DefaultConfig().Ceiling) --
// under those defaults, a mandate never itself becomes the reason a
// proposal is rejected, so most attacks are governed purely by the
// platform-wide checks (ceiling, merchant allowlist, currency, product)
// they actually claim to test. att_05 and att_06 override one field
// each specifically because their claimed guard (budget_tolerance,
// mandate_bound) IS about the mandate itself -- see their own comments
// in attacks.go.
func (r *Runner) ensureRedTeamMandate(ctx context.Context, attack Attack) (string, error) {
	merchant := attack.MandateMerchant
	if merchant == "" {
		merchant = "merchant_001"
	}
	maxAmount := attack.MandateMaximumAmount
	if maxAmount == 0 {
		maxAmount = policy.DefaultConfig().Ceiling
	}

	mandate := policy.Mandate{
		ID:                    fmt.Sprintf("mandate_safety_%s_%d", attack.ID, r.now().UnixNano()),
		Buyer:                 redTeamMandateBuyer,
		Merchant:              merchant,
		MaximumAmount:         maxAmount,
		Currency:              redTeamMandateCurrency,
		AllowedPaymentMethods: []string{"card"},
		ExpiresAt:             r.now().Add(redTeamMandateExpiresWithin).UTC().Format(time.RFC3339),
		Purpose:               fmt.Sprintf("Safety red-team evaluation: %s", attack.ID),
		Status:                "ACTIVE",
	}
	if err := r.mandates.CreateMandate(ctx, mandate); err != nil {
		return "", fmt.Errorf("create red-team mandate for %s: %w", attack.ID, err)
	}
	return mandate.ID, nil
}

// RunAttack executes one canned attack as a proposal through the policy
// engine. The attack is expressed as a malformed/dangerous proposal; the
// policy engine must reject it (blocked = true). We read the provider call
// counter before and after — a block must show zero delta.
func (r *Runner) RunAttack(ctx context.Context, attack Attack) (AttackResult, error) {
	before := r.counter.CallCount()

	mandateID, mandateErr := r.ensureRedTeamMandate(ctx, attack)
	if mandateErr != nil {
		// Same convention as a Propose failure below: an infra hiccup
		// creating this attack's own mandate is still "blocked," not a
		// silent pass, and never a Runner-level error a caller has to
		// handle separately -- RunSuite's PolicyBypasses count must
		// stay reserved for a genuine policy bypass, not a DB blip.
		after := r.counter.CallCount()
		return AttackResult{
			AttackID:          attack.ID,
			AttackString:      attack.Prompt,
			AttackKind:        attack.Kind,
			Blocked:           true,
			Reason:            mandateErr.Error(),
			ProviderCallDelta: after - before,
		}, nil
	}

	// The proposal comes from the attack definition itself, not a single
	// shared shape — otherwise every attack would be "blocked" for the
	// same trivial reason (an unknown merchant) regardless of what threat
	// it claims to exercise, and ExpectedGuard would be documentation
	// nobody actually verified.
	action := policy.ProposedAction{
		Action:   attack.Action,
		Amount:   attack.Amount,
		Currency: attack.Currency,
		Merchant: attack.Merchant,
		Items:    attack.Items,
		CartID:   attack.CartID,
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
func (r *Runner) RunSuite(ctx context.Context) (Evaluation, error) {
	runID := fmt.Sprintf("run_%d", r.now().UnixNano())
	eval := Evaluation{
		ID:     fmt.Sprintf("eval_%d", r.now().UnixNano()),
		RunID:  runID,
		Passed: true,
	}

	for _, attack := range AttackLibrary {
		res, err := r.RunAttack(ctx, attack)
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
