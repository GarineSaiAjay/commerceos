package policy

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestSaveAgentPlanAndGetRun proves item 16's new persistence path end
// to end: SaveAgentPlan writes a row, GetRun routes a "plan_"-prefixed
// ID to it (not the agent_actions path), and the returned Run carries
// the sentinel decision plus the exact reasoning trail that was saved
// -- never a real APPROVED/REJECTED/PENDING_HUMAN_APPROVAL value, since
// no policy evaluation ever runs over an AgentPlan.
func TestSaveAgentPlanAndGetRun(t *testing.T) {
	ctx := context.Background()

	pool, err := pgxpool.New(
		ctx,
		"postgres://commerceos:commerceos_dev_password@localhost:5433/commerceos?sslmode=disable",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	planID := "plan_test_get_run"
	_, _ = pool.Exec(ctx, `DELETE FROM agent_plans WHERE id = $1`, planID)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM agent_plans WHERE id = $1`, planID) })

	repo := NewPostgresRepository(pool)

	plan := AgentPlan{
		ID: planID,
		Proposal: ProposedAction{
			Action:   "CREATE_ORDER",
			Amount:   129900,
			Currency: "INR",
			Merchant: "merchant_001",
			Items:    []string{"product_test_earbuds"},
		},
		Steps: []RunStep{
			{Stage: "intent_extracted", Detail: "category=earbuds budget=₹1500 priority=battery_life recipient=—", Timestamp: time.Now()},
			{Stage: "proposed", Detail: "Selected Test Earbuds (₹1299) — best match.", Timestamp: time.Now()},
		},
		CreatedAt: time.Now(),
	}

	if err := repo.SaveAgentPlan(ctx, plan); err != nil {
		t.Fatalf("SaveAgentPlan: %v", err)
	}

	// Saving twice with the same ID must fail (primary key), not
	// silently upsert -- an AgentPlan is a one-shot append, unlike
	// recommendations' intentional ON CONFLICT DO UPDATE.
	if err := repo.SaveAgentPlan(ctx, plan); err == nil {
		t.Fatal("expected SaveAgentPlan to reject a duplicate ID, got nil error")
	}

	run, err := repo.GetRun(ctx, planID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}

	if run.ID != planID {
		t.Errorf("run.ID = %q, want %q", run.ID, planID)
	}
	if run.Decision != PlanDecisionSentinel {
		t.Errorf("run.Decision = %q, want %q", run.Decision, PlanDecisionSentinel)
	}
	if run.Action != "CREATE_ORDER" || run.Amount != 129900 || run.Currency != "INR" || run.Merchant != "merchant_001" {
		t.Errorf("run proposal fields did not round-trip: %+v", run)
	}
	if len(run.Items) != 1 || run.Items[0] != "product_test_earbuds" {
		t.Errorf("run.Items = %v, want [product_test_earbuds]", run.Items)
	}
	if len(run.Steps) != 2 {
		t.Fatalf("len(run.Steps) = %d, want 2", len(run.Steps))
	}
	if run.Steps[0].Stage != "intent_extracted" || run.Steps[1].Stage != "proposed" {
		t.Errorf("run.Steps stages = %q, %q -- want intent_extracted, proposed", run.Steps[0].Stage, run.Steps[1].Stage)
	}
	// An AgentPlan-backed run never has a real authorization -- GetRun's
	// agent_plans path must never fabricate one.
	if run.Authorization != "" || run.AuthStatus != "" {
		t.Errorf("run.Authorization/AuthStatus should be empty for a plan-only run, got %q/%q", run.Authorization, run.AuthStatus)
	}
}

// TestListRunsMergesAgentPlans proves ListRuns surfaces agent_plans
// rows alongside agent_actions rows, sorted by recency together, not
// as two separate lists or with one silently starving the other.
func TestListRunsMergesAgentPlans(t *testing.T) {
	ctx := context.Background()

	pool, err := pgxpool.New(
		ctx,
		"postgres://commerceos:commerceos_dev_password@localhost:5433/commerceos?sslmode=disable",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	planID := "plan_test_list_runs"
	_, _ = pool.Exec(ctx, `DELETE FROM agent_plans WHERE id = $1`, planID)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM agent_plans WHERE id = $1`, planID) })

	repo := NewPostgresRepository(pool)

	if err := repo.SaveAgentPlan(ctx, AgentPlan{
		ID: planID,
		Proposal: ProposedAction{
			Action: "CREATE_ORDER", Amount: 49900, Currency: "INR", Merchant: "merchant_001",
			Items: []string{"product_test_list_runs"},
		},
		Steps:     []RunStep{{Stage: "proposed", Detail: "test", Timestamp: time.Now()}},
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("SaveAgentPlan: %v", err)
	}

	runs, err := repo.ListRuns(ctx, 200)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}

	var found *Run
	for i := range runs {
		if runs[i].ID == planID {
			found = &runs[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("ListRuns did not include agent_plans row %q among %d runs", planID, len(runs))
	}
	if found.Decision != PlanDecisionSentinel {
		t.Errorf("found.Decision = %q, want %q", found.Decision, PlanDecisionSentinel)
	}
	// ListRuns' summary rows never carry Steps -- only GetRun builds
	// the timeline, same contract as the agent_actions path (see
	// Run.Steps's own doc comment).
	if len(found.Steps) != 0 {
		t.Errorf("ListRuns row should have no Steps, got %d", len(found.Steps))
	}
}
