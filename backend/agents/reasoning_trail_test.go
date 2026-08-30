package agents

import (
	"context"
	"testing"
)

// TestBuildReasoningTrailStages proves planFromIntent's audit trail
// (item 16) actually gets attached to the plan it describes: an
// intent_extracted step first, an alternatives_considered step when the
// searcher found other matches (this catalog and budget always
// surface at least one), and a final proposed step whose Detail is
// exactly the same sentence already returned as plan.Reasoning -- the
// trail must never say anything the buyer wasn't already told.
func TestBuildReasoningTrailStages(t *testing.T) {
	agent := NewBuyerAgent(NewDeterministicExtractor(), NewSearcher(fakeCatalog{}))

	plan, err := agent.PlanCheckout(
		context.Background(),
		"I need wireless earbuds for my sister. Budget ₹25,000. I want good noise cancellation.",
		"merchant_001",
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(plan.ReasoningTrail) == 0 {
		t.Fatal("expected a non-empty ReasoningTrail")
	}
	if plan.ReasoningTrail[0].Stage != "intent_extracted" {
		t.Fatalf("expected first stage intent_extracted, got %s", plan.ReasoningTrail[0].Stage)
	}

	last := plan.ReasoningTrail[len(plan.ReasoningTrail)-1]
	if last.Stage != "proposed" {
		t.Fatalf("expected last stage proposed, got %s", last.Stage)
	}
	if last.Detail != plan.Reasoning {
		t.Fatalf("proposed step detail %q does not match plan.Reasoning %q", last.Detail, plan.Reasoning)
	}

	if len(plan.Alternatives) > 0 {
		found := false
		for _, s := range plan.ReasoningTrail {
			if s.Stage == "alternatives_considered" {
				found = true
			}
		}
		if !found {
			t.Fatal("plan has alternatives but no alternatives_considered step")
		}
	}
}

// TestBuildReasoningTrailOmitsAlternativesWhenNone proves the
// alternatives_considered step is genuinely omitted (not present with
// an empty Detail) when the searcher found nothing else to mention --
// an empty step would just be noise on the timeline.
func TestBuildReasoningTrailOmitsAlternativesWhenNone(t *testing.T) {
	steps := buildReasoningTrail(Intent{Category: "earbuds", Budget: 5000, Priority: "battery_life"}, nil, "Selected X.")

	for _, s := range steps {
		if s.Stage == "alternatives_considered" {
			t.Fatal("expected no alternatives_considered step when alternatives is empty")
		}
	}
	if len(steps) != 2 {
		t.Fatalf("expected exactly 2 steps (intent_extracted, proposed), got %d", len(steps))
	}
}

// TestLoopResultReasoningTrailRelabelsToolResult proves reasoningTrail's
// one and only transformation: the live wire "tool_result" LoopStep
// type becomes "tool_result_summary" at this conversion boundary,
// every other step type passes through unchanged, and the original
// LoopResult.Steps slice itself is never mutated (the frontend and any
// other existing caller of /agent/loop must keep seeing "tool_result").
func TestLoopResultReasoningTrailRelabelsToolResult(t *testing.T) {
	result := LoopResult{
		Steps: []LoopStep{
			{Type: "tool_called", Detail: "search_products({})"},
			{Type: "tool_result", Detail: `{"results":[]}`},
			{Type: "proposed", Detail: "Selected X."},
		},
	}

	trail := result.reasoningTrail()
	if len(trail) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(trail))
	}
	wantStages := []string{"tool_called", "tool_result_summary", "proposed"}
	for i, want := range wantStages {
		if trail[i].Stage != want {
			t.Errorf("step %d: stage = %q, want %q", i, trail[i].Stage, want)
		}
		if trail[i].Detail != result.Steps[i].Detail {
			t.Errorf("step %d: detail = %q, want %q", i, trail[i].Detail, result.Steps[i].Detail)
		}
	}

	if result.Steps[1].Type != "tool_result" {
		t.Fatalf("reasoningTrail must not mutate the original LoopStep.Type, got %q", result.Steps[1].Type)
	}
}

// TestLoopResultReasoningTrailEmptyWhenNoSteps proves a LoopResult with
// no steps at all (the zero value) yields no reasoning trail to
// persist, rather than a slice of zero RunSteps.
func TestLoopResultReasoningTrailEmptyWhenNoSteps(t *testing.T) {
	var result LoopResult
	if trail := result.reasoningTrail(); trail != nil {
		t.Fatalf("expected nil, got %v", trail)
	}
}
