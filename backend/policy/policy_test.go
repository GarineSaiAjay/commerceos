package policy

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeRepo is an in-memory policy repository.
type fakeRepo struct {
	mandates       map[string]Mandate
	authorizations map[string]Authorization
	evals          []Evaluation
	config         *PolicyConfig // nil until SaveConfig is called, matching ErrPolicyConfigNotFound's real precondition
	saveConfigErr  error         // when set, SaveConfig fails instead of writing -- for TestServiceUpdatePolicyConfigLeavesEngineUntouchedOnSaveFailure
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		mandates:       map[string]Mandate{},
		authorizations: map[string]Authorization{},
	}
}

func (r *fakeRepo) GetConfig(ctx context.Context) (PolicyConfig, error) {
	if r.config == nil {
		return PolicyConfig{}, ErrPolicyConfigNotFound
	}
	return *r.config, nil
}

func (r *fakeRepo) SaveConfig(ctx context.Context, cfg PolicyConfig, updatedBy string) error {
	if r.saveConfigErr != nil {
		return r.saveConfigErr
	}
	r.config = &cfg
	return nil
}

func (r *fakeRepo) GetMandate(ctx context.Context, id string) (Mandate, error) {
	m, ok := r.mandates[id]
	if !ok {
		return Mandate{}, ErrMandateNotFound
	}
	return m, nil
}

func (r *fakeRepo) SaveMandate(ctx context.Context, m Mandate) error {
	r.mandates[m.ID] = m
	return nil
}

func (r *fakeRepo) GetAuthorization(ctx context.Context, id string) (Authorization, error) {
	a, ok := r.authorizations[id]
	if !ok {
		return Authorization{}, ErrAuthorizationNotFound
	}
	return a, nil
}

func (r *fakeRepo) SaveAuthorization(ctx context.Context, a Authorization) error {
	r.authorizations[a.ID] = a
	return nil
}

func (r *fakeRepo) MarkAuthorizationUsed(ctx context.Context, id string) error {
	a := r.authorizations[id]
	a.Status = "USED"
	r.authorizations[id] = a
	return nil
}

func (r *fakeRepo) SaveEvaluation(ctx context.Context, e Evaluation) error {
	r.evals = append(r.evals, e)
	return nil
}

func (r *fakeRepo) SaveAction(ctx context.Context, a ProposedAction, actionID string) error {
	return nil
}

func (r *fakeRepo) ProposalAlreadyEvaluated(ctx context.Context, a ProposedAction) (bool, error) {
	return false, nil
}

func (r *fakeRepo) ActiveAuthorizationExists(ctx context.Context, a ProposedAction) (bool, error) {
	return false, nil
}

func (r *fakeRepo) GetActiveAuthorization(ctx context.Context, a ProposedAction) (Authorization, error) {
	return Authorization{}, ErrAuthorizationNotFound
}

func (r *fakeRepo) SaveRiskAssessment(ctx context.Context, assessment RiskAssessment) error {
	return nil
}

func (r *fakeRepo) SaveAgentDecision(ctx context.Context, d AgentDecision) error {
	return nil
}

func (r *fakeRepo) SaveApprovalRequest(ctx context.Context, a ApprovalRequest) error {
	return nil
}

func (r *fakeRepo) GetApprovalRequest(ctx context.Context, id string) (ApprovalRequest, error) {
	return ApprovalRequest{}, ErrApprovalRequestNotFound
}

func (r *fakeRepo) GetPendingApprovalForAction(ctx context.Context, a ProposedAction) (ApprovalRequest, error) {
	return ApprovalRequest{}, ErrApprovalRequestNotFound
}

func (r *fakeRepo) ListApprovalRequests(ctx context.Context, status string, limit int) ([]ApprovalRequest, error) {
	return nil, nil
}

func (r *fakeRepo) ListRuns(ctx context.Context, limit int) ([]Run, error) {
	return nil, nil
}

func (r *fakeRepo) GetRun(ctx context.Context, runID string) (Run, error) {
	return Run{}, ErrAuthorizationNotFound
}

// SaveAgentPlan is a no-op here (item 16) -- none of this file's tests
// exercise the agents package's Handler/RunRecorder path, only
// policy.Service/Engine directly, so there's nothing for a real fake to
// record. Exists purely so *fakeRepo keeps satisfying the Repository
// interface.
func (r *fakeRepo) SaveAgentPlan(ctx context.Context, p AgentPlan) error {
	return nil
}

// LatestPlanIDForCart/SetActionPlanID are no-ops here for the same
// reason SaveAgentPlan is: this file's tests exercise
// policy.Service/Engine directly, never through a real agent_plans row,
// so "found=false, no error" (never finding a plan to link) is the
// correct fake behavior -- Service.Propose's own best-effort handling
// of "no plan found" is exercised by policy/postgres_repository_test.go
// instead, against the real DB.
func (r *fakeRepo) LatestPlanIDForCart(ctx context.Context, cartID string, before time.Time) (string, bool, error) {
	return "", false, nil
}

func (r *fakeRepo) SetActionPlanID(ctx context.Context, actionID, planID string) error {
	return nil
}

func (r *fakeRepo) UpdateApprovalRequestStatus(ctx context.Context, id, status, authorizationID, reason string) error {
	return nil
}

func activeMandate(merchant string, max int64, expires time.Time) Mandate {
	return Mandate{
		ID:                    "mnd_test",
		Buyer:                 "buyer_42",
		Merchant:              merchant,
		AllowedCategories:     []string{"electronics"},
		MaximumAmount:         max,
		Currency:              "INR",
		AllowedPaymentMethods: []string{"upi", "card"},
		ExpiresAt:             expires.Format(time.RFC3339),
		Status:                "ACTIVE",
	}
}

func mandate(merchant string, max int64, expires time.Time) Mandate {
	return activeMandate(merchant, max, expires)
}

// TestCeilingRejected proves spec: amount above ceiling is rejected.
func TestCeilingRejected(t *testing.T) {
	repo := newFakeRepo()
	repo.mandates["mand"] = activeMandate("merchant_001", 25000, time.Now().Add(time.Hour))
	engine := NewEngine(DefaultConfig(), repo)
	svc := NewService(engine, NewRiskEngine(), repo)

	action := ProposedAction{
		Action:   "CREATE_ORDER",
		Amount:   6_000_000, // above the ₹30,000 (3,000,000 paise) ceiling
		Currency: "INR",
		Merchant: "merchant_001",
		Items:    []string{"airpods-pro-2"},
	}

	decision, err := svc.Propose(context.Background(), action, "mand")
	if err != nil {
		t.Fatal(err)
	}

	if decision.Decision != DecisionRejected {
		t.Fatalf("expected REJECTED, got %s", decision.Decision)
	}

	if decision.FailedCheck != CheckAmountCeiling {
		t.Fatalf("expected %s, got %s", CheckAmountCeiling, decision.FailedCheck)
	}

	// Zero authorization issued.
	if len(repo.authorizations) != 0 {
		t.Fatal("rejected proposal must not issue an authorization")
	}
}

// TestFailureDemo2 proves spec §7: adding a ₹2,000 accessory to a
// ₹24,900 authorization with a ₹25,000 ceiling is rejected BEFORE any
// payment call.
func TestFailureDemo2(t *testing.T) {
	repo := newFakeRepo()
	repo.mandates["mand"] = Mandate{
		ID:            "mand",
		Merchant:      "merchant_001",
		MaximumAmount: 25000,
		Currency:      "INR",
		Status:        "ACTIVE",
		ExpiresAt:     time.Now().Add(time.Hour).Format(time.RFC3339),
	}
	engine := NewEngine(DefaultConfig(), repo)
	svc := NewService(engine, NewRiskEngine(), repo)

	// Accessory pushes the total to ₹26,900 > ₹25,000 ceiling.
	action := ProposedAction{
		Action:   "ADD_ITEM",
		Amount:   26900,
		Currency: "INR",
		Merchant: "merchant_001",
		Items:    []string{"airpods-pro-2", "airpods-case"},
	}

	decision, err := svc.Propose(context.Background(), action, "mand")
	if err != nil {
		t.Fatal(err)
	}

	if decision.Decision != DecisionRejected {
		t.Fatalf("expected REJECTED, got %s", decision.Decision)
	}

	if decision.FailedCheck != CheckBudgetTolerance {
		t.Fatalf("expected budget_tolerance, got %s", decision.FailedCheck)
	}

	explanation := ExplainRejection(decision.FailedCheck, action, repo.mandates["mand"], DefaultConfig().Ceiling)
	if !contains(explanation, "26,900") || !contains(explanation, "25,000") {
		t.Fatalf("explanation missing numbers: %s", explanation)
	}
}

// TestMandateBindingChange proves spec §5.3: changing any element bound
// to the mandate invalidates it before payment.
func TestMandateBindingChange(t *testing.T) {
	repo := newFakeRepo()
	m := mandate("merchant_001", 30000, time.Now().Add(time.Hour))
	m.Merchant = "merchant_999" // merchant swap
	repo.mandates["mand"] = m
	engine := NewEngine(DefaultConfig(), repo)
	svc := NewService(engine, NewRiskEngine(), repo)

	action := ProposedAction{
		Action:   "CREATE_ORDER",
		Amount:   1000,
		Currency: "INR",
		Merchant: "merchant_999",
		Items:    []string{"airpods-case"},
	}

	decision, err := svc.Propose(context.Background(), action, "mand")
	if err != nil {
		t.Fatal(err)
	}

	if decision.Decision != DecisionRejected {
		t.Fatalf("expected REJECTED for merchant swap, got %s", decision.Decision)
	}
}

// TestLevelRouting proves spec §6: level routing is a function of amount
// AND merchant trust, not a single threshold. Amounts are paise.
func TestLevelRouting(t *testing.T) {
	repo := newFakeRepo()
	repo.mandates["mand"] = mandate("merchant_001", 3_000_000, time.Now().Add(time.Hour))
	engine := NewEngine(DefaultConfig(), repo)
	svc := NewService(engine, NewRiskEngine(), repo)

	// ≤ ₹1,000 (100_000 paise) → Level 1 auto-approve.
	d, _ := svc.Propose(context.Background(), ProposedAction{
		Action: "CREATE_ORDER", Amount: 50_000, Currency: "INR",
		Merchant: "merchant_001", Items: []string{"airpods-case"},
	}, "mand")
	if d.Level != 1 {
		t.Fatalf("expected level 1 for ₹500, got %d", d.Level)
	}

	// ₹1,001–₹10,000 (100_001 – 1_000_000 paise) → Level 2 confirm.
	d, _ = svc.Propose(context.Background(), ProposedAction{
		Action: "CREATE_ORDER", Amount: 500_000, Currency: "INR",
		Merchant: "merchant_001", Items: []string{"airpods-pro-2"},
	}, "mand")
	if d.Level != 2 {
		t.Fatalf("expected level 2 for ₹5,000, got %d", d.Level)
	}

	// > ₹10,000 (1_000_000 paise) → Level 3 hard gate.
	d, _ = svc.Propose(context.Background(), ProposedAction{
		Action: "CREATE_ORDER", Amount: 2_000_000, Currency: "INR",
		Merchant: "merchant_001", Items: []string{"airpods-pro-2"},
	}, "mand")
	if d.Level != 3 {
		t.Fatalf("expected level 3 for ₹20,000, got %d", d.Level)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

// TestLevel3OnHighRisk proves spec §6: a high-risk proposal triggers
// Level 3 regardless of amount.
func TestLevel3OnHighRisk(t *testing.T) {
	repo := newFakeRepo()
	// Unknown merchant inflates the risk score; the amount is tiny, so
	// only the risk factor can force Level 3.
	repo.mandates["mand"] = mandate("merchant_999", 3_000_000, time.Now().Add(time.Hour))
	engine := NewEngine(DefaultConfig(), repo)
	svc := NewService(engine, NewRiskEngine(), repo)

	d, err := svc.Propose(context.Background(), ProposedAction{
		Action: "CREATE_ORDER", Amount: 1_000, Currency: "INR",
		Merchant: "merchant_999", Items: []string{"airpods-case"},
	}, "mand")
	if err != nil {
		t.Fatal(err)
	}

	// The unknown merchant fails allowlisting, so the decision is
	// REJECTED — and rejected decisions must not silently pass. The level
	// is only meaningful for approved decisions, but the route must still
	// classify this as a hard gate.
	if d.Decision != DecisionRejected {
		t.Fatalf("expected REJECTED for unknown merchant, got %s", d.Decision)
	}
}

// TestMandateCartBound proves spec §5.3: a mandate bound to a specific
// cart rejects a proposal that references a different cart.
func TestMandateCartBound(t *testing.T) {
	repo := newFakeRepo()
	m := mandate("merchant_001", 3_000_000, time.Now().Add(time.Hour))
	m.CartID = "cart_abc"
	repo.mandates["mand"] = m
	engine := NewEngine(DefaultConfig(), repo)
	svc := NewService(engine, NewRiskEngine(), repo)

	// Mismatched cart: the action carries no cart ref and the mandate is
	// bound to cart_abc → rejected.
	d, err := svc.Propose(context.Background(), ProposedAction{
		Action: "CREATE_ORDER", Amount: 50_000, Currency: "INR",
		Merchant: "merchant_001", Items: []string{"airpods-case"},
	}, "mand")
	if err != nil {
		t.Fatal(err)
	}

	if d.Decision != DecisionRejected {
		t.Fatalf("expected REJECTED for cart mismatch, got %s", d.Decision)
	}
	if d.FailedCheck != CheckMandateCartBound {
		t.Fatalf("expected %s, got %s", CheckMandateCartBound, d.FailedCheck)
	}

	// Matching cart bound → approved.
	d, err = svc.Propose(context.Background(), ProposedAction{
		Action: "CREATE_ORDER", Amount: 50_000, Currency: "INR",
		Merchant: "merchant_001", Items: []string{"airpods-case"}, CartID: "cart_abc",
	}, "mand")
	if err != nil {
		t.Fatal(err)
	}
	if d.Decision != DecisionApproved {
		t.Fatalf("expected APPROVED for matching cart, got %s (%s)", d.Decision, d.Reason)
	}
}

// TestProposalReusesActiveAuthorization proves a repeated proposal for the
// same action reuses the existing ACTIVE authorization instead of minting a
// duplicate (idempotent propose → identical auth).
type dupRepo struct {
	*fakeRepo
	authsByAction map[string]*Authorization
}

func (r *dupRepo) SaveAuthorization(ctx context.Context, a Authorization) error {
	key := a.Action + "|" + itoa(a.Amount) + "|" + a.Currency + "|" + a.Merchant + "|" + strings.Join(a.Items, ",")
	auth := a
	r.authsByAction[key] = &auth
	return r.fakeRepo.SaveAuthorization(ctx, a)
}

func (r *dupRepo) GetActiveAuthorization(ctx context.Context, a ProposedAction) (Authorization, error) {
	key := a.Action + "|" + itoa(a.Amount) + "|" + a.Currency + "|" + a.Merchant + "|" + strings.Join(a.Items, ",")
	if auth := r.authsByAction[key]; auth != nil {
		return *auth, nil
	}
	return Authorization{}, ErrAuthorizationNotFound
}

func TestNoDuplicateProposal(t *testing.T) {
	repo := &dupRepo{fakeRepo: newFakeRepo(), authsByAction: map[string]*Authorization{}}
	repo.mandates["mand"] = mandate("merchant_001", 3_000_000, time.Now().Add(time.Hour))
	engine := NewEngine(DefaultConfig(), repo)
	svc := NewService(engine, NewRiskEngine(), repo)

	action := ProposedAction{
		Action: "CREATE_ORDER", Amount: 50_000, Currency: "INR",
		Merchant: "merchant_001", Items: []string{"airpods-case"},
	}

	// First proposal is evaluated and APPROVED (issues an authorization).
	first, err := svc.Propose(context.Background(), action, "mand")
	if err != nil {
		t.Fatal(err)
	}
	if first.Decision != DecisionApproved {
		t.Fatalf("expected first proposal APPROVED, got %s", first.Decision)
	}
	if first.AuthorizationID == "" {
		t.Fatal("expected an authorization ID")
	}
	if len(repo.authorizations) != 1 {
		t.Fatalf("expected 1 authorization, got %d", len(repo.authorizations))
	}

	// The identical second proposal must REUSE the same authorization
	// (idempotent), not be rejected and not mint a second one.
	second, err := svc.Propose(context.Background(), action, "mand")
	if err != nil {
		t.Fatal(err)
	}
	if second.Decision != DecisionApproved {
		t.Fatalf("expected duplicate to be APPROVED via reuse, got %s (%s)", second.Decision, second.Reason)
	}
	if second.AuthorizationID != first.AuthorizationID {
		t.Fatalf("expected same authorization reused, got %s vs %s", second.AuthorizationID, first.AuthorizationID)
	}
	if len(repo.authorizations) != 1 {
		t.Fatalf("expected still 1 authorization (no second mint), got %d", len(repo.authorizations))
	}
}

// TestRejectedProposalRetryNotBlocked proves a rejected proposal can be
// retried: rejection issues no authorization, so the no-duplicate guard
// does not block a subsequent identical proposal.
func TestRejectedProposalRetryNotBlocked(t *testing.T) {
	repo := newFakeRepo()
	// Mandate below the amount → first proposal is REJECTED (no auth).
	repo.mandates["mand"] = mandate("merchant_001", 10_000, time.Now().Add(time.Hour))
	svc := NewService(NewEngine(DefaultConfig(), repo), NewRiskEngine(), repo)

	action := ProposedAction{
		Action: "CREATE_ORDER", Amount: 2_819_800, Currency: "INR",
		Merchant: "merchant_001", Items: []string{"airpods-pro-2", "airpods-case", "usb-c-adapter"},
	}

	first, err := svc.Propose(context.Background(), action, "mand")
	if err != nil {
		t.Fatal(err)
	}
	if first.Decision != DecisionRejected {
		t.Fatalf("expected first REJECTED, got %s", first.Decision)
	}

	// No authorization issued → zero duplicate risk on retry.
	if len(repo.authorizations) != 0 {
		t.Fatalf("expected 0 authorizations after rejection, got %d", len(repo.authorizations))
	}

	// Raise the mandate ceiling and retry exactly the same proposal: it
	// must now pass policy (not be blocked by any duplicate guard). Since
	// the amount is Level 3, it correctly returns PENDING_HUMAN_APPROVAL
	// (which proves the retry was NOT blocked as a duplicate).
	repo.mandates["mand"] = mandate("merchant_001", 3_000_000, time.Now().Add(time.Hour))
	second, err := svc.Propose(context.Background(), action, "mand")
	if err != nil {
		t.Fatal(err)
	}
	if second.Decision != DecisionPendingApproval {
		t.Fatalf("expected retry PENDING_HUMAN_APPROVAL after raising ceiling, got %s (%s)", second.Decision, second.Reason)
	}
	if second.ApprovalRequestID == "" {
		t.Fatal("expected an approval request for the retried Level 3 proposal")
	}
}

// --- item 25 (P2, PLAN-05-SELLER-DASHBOARD.md §4) coverage ---
//
// Engine.Config/UpdateConfig and Service.GetPolicyConfig/
// UpdatePolicyConfig shipped with item 25 but no test coverage of
// their own -- every existing test in this file only ever constructs
// an Engine once via NewEngine and never mutates it afterward, so
// none of them would have caught a bug in the new mutation path. This
// section fills that gap: found and fixed as its own small commit
// before starting item 32, rather than left for later.

func TestEngineUpdateConfigTakesEffectOnNextEvaluate(t *testing.T) {
	repo := newFakeRepo()
	// MaximumAmount matches the proposed amount exactly (same convention
	// checkout.tsx's real POST /policy/mandates call always uses -- see
	// rejection_recovery.go's own doc comment on this), so once the
	// ceiling is raised nothing else in the checklist has a reason to
	// reject this proposal either -- the only thing that changes between
	// "before" and "after" is UpdateConfig, not some other confound.
	repo.mandates["mand"] = activeMandate("merchant_001", 6_000_000, time.Now().Add(time.Hour))
	engine := NewEngine(DefaultConfig(), repo)
	svc := NewService(engine, NewRiskEngine(), repo)

	action := ProposedAction{
		Action:   "CREATE_ORDER",
		Amount:   6_000_000, // above DefaultConfig()'s 3,000,000 ceiling
		Currency: "INR",
		Merchant: "merchant_001",
		Items:    []string{"airpods-pro-2"},
	}

	before, err := svc.Propose(context.Background(), action, "mand")
	if err != nil {
		t.Fatal(err)
	}
	if before.Decision != DecisionRejected || before.FailedCheck != CheckAmountCeiling {
		t.Fatalf("expected REJECTED/amount_ceiling before raising the ceiling, got %s/%s", before.Decision, before.FailedCheck)
	}

	raised := DefaultConfig()
	raised.Ceiling = 10_000_000 // ₹100,000 -- comfortably above the 6,000,000 action amount
	engine.UpdateConfig(raised)

	after, err := svc.Propose(context.Background(), action, "mand")
	if err != nil {
		t.Fatal(err)
	}
	// 6,000,000 paise is Level 3 (routeLevel: > 1,000,000 paise), so this
	// clears every check and comes back PENDING_HUMAN_APPROVAL, not
	// REJECTED -- the point either way is that it's no longer rejected at
	// all, let alone specifically for amount_ceiling.
	if after.Decision == DecisionRejected {
		t.Fatalf("expected the raised ceiling to take effect immediately (no longer REJECTED), got %s (%s/%s)", after.Decision, after.FailedCheck, after.Reason)
	}
}

func TestEngineUpdateConfigPreservesAllowedProducts(t *testing.T) {
	original := DefaultConfig()
	if len(original.AllowedProducts) == 0 {
		t.Fatal("test assumption broken: DefaultConfig().AllowedProducts is empty")
	}

	engine := NewEngine(original, newFakeRepo())

	// A caller-supplied config with AllowedProducts left as its zero
	// value (nil) -- exactly the shape Service.UpdatePolicyConfig's
	// HTTP request path produces, since AllowedProducts was
	// deliberately never part of the wire request. UpdateConfig must
	// never let this null out the real fallback list.
	engine.UpdateConfig(PolicyConfig{
		Ceiling:           1_000_000,
		BudgetTolerance:   0.2,
		AllowedCurrencies: []string{"INR"},
		AllowedMerchants:  []string{"merchant_001"},
	})

	got := engine.Config()
	if len(got.AllowedProducts) != len(original.AllowedProducts) {
		t.Fatalf("expected AllowedProducts to survive UpdateConfig untouched (%d items), got %d", len(original.AllowedProducts), len(got.AllowedProducts))
	}
	if got.AllowedProducts[0] != original.AllowedProducts[0] {
		t.Fatalf("expected AllowedProducts to survive UpdateConfig untouched, got %v", got.AllowedProducts)
	}
	// The rest of the config DID change, proving this isn't just
	// silently ignoring the whole UpdateConfig call.
	if got.Ceiling != 1_000_000 {
		t.Fatalf("expected Ceiling to have been updated, got %d", got.Ceiling)
	}
}

func TestServiceUpdatePolicyConfigPersistsAndAppliesLive(t *testing.T) {
	repo := newFakeRepo()
	engine := NewEngine(DefaultConfig(), repo)
	svc := NewService(engine, NewRiskEngine(), repo)

	newCfg := PolicyConfig{
		Ceiling:           5_000_000,
		BudgetTolerance:   0.15,
		AllowedCurrencies: []string{"INR", "USD"},
		AllowedMerchants:  []string{"merchant_001", "merchant_002"},
	}

	updated, err := svc.UpdatePolicyConfig(context.Background(), newCfg, "owner@commerceos.demo")
	if err != nil {
		t.Fatalf("UpdatePolicyConfig returned an error: %v", err)
	}
	if updated.Ceiling != newCfg.Ceiling || updated.BudgetTolerance != newCfg.BudgetTolerance {
		t.Fatalf("expected the returned config to reflect the update, got %+v", updated)
	}

	// Persisted: the repository actually has a row now.
	if repo.config == nil {
		t.Fatal("expected SaveConfig to have been called, but repo.config is still nil")
	}
	if repo.config.Ceiling != newCfg.Ceiling {
		t.Fatalf("expected the persisted ceiling to be %d, got %d", newCfg.Ceiling, repo.config.Ceiling)
	}

	// Applied live: the engine (and therefore GetPolicyConfig) reflects
	// it immediately, without needing a restart or a fresh GetConfig read.
	live := svc.GetPolicyConfig()
	if live.Ceiling != newCfg.Ceiling {
		t.Fatalf("expected the live engine config to reflect the update, got ceiling %d", live.Ceiling)
	}
	if len(live.AllowedCurrencies) != 2 {
		t.Fatalf("expected 2 allowed currencies live, got %v", live.AllowedCurrencies)
	}
}

func TestServiceUpdatePolicyConfigLeavesEngineUntouchedOnSaveFailure(t *testing.T) {
	repo := newFakeRepo()
	repo.saveConfigErr = errors.New("simulated database failure")
	engine := NewEngine(DefaultConfig(), repo)
	svc := NewService(engine, NewRiskEngine(), repo)

	before := svc.GetPolicyConfig()

	_, err := svc.UpdatePolicyConfig(context.Background(), PolicyConfig{
		Ceiling:           1,
		BudgetTolerance:   0,
		AllowedCurrencies: []string{"INR"},
		AllowedMerchants:  []string{"merchant_001"},
	}, "owner@commerceos.demo")
	if err == nil {
		t.Fatal("expected UpdatePolicyConfig to return an error when SaveConfig fails")
	}

	// The whole point: a DB failure must never let an in-memory-only
	// change take effect. If it did, a settings change could look like
	// it "took" (the engine enforcing it right now) but silently
	// vanish on the next restart, since nothing durable exists.
	after := svc.GetPolicyConfig()
	if after.Ceiling != before.Ceiling {
		t.Fatalf("expected the live engine config to be unchanged after a failed save (still %d), got %d", before.Ceiling, after.Ceiling)
	}
}
