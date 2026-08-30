package agents

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/garinesaiajay/commerceos/policy"
)

// fakeRunRecorder is an in-memory RunRecorder double (item 16) --
// records every SaveAgentPlan call it received, and can be configured
// to fail so tests can prove a persistence failure never turns a
// working checkout proposal into a failed HTTP response.
type fakeRunRecorder struct {
	err   error
	calls []struct {
		id     string
		action policy.ProposedAction
		steps  []policy.RunStep
	}
}

func (f *fakeRunRecorder) SaveAgentPlan(ctx context.Context, id string, action policy.ProposedAction, steps []policy.RunStep) error {
	f.calls = append(f.calls, struct {
		id     string
		action policy.ProposedAction
		steps  []policy.RunStep
	}{id, action, steps})
	return f.err
}

// TestPlanCheckoutRecordsRunBestEffort proves POST /agent/checkout, once
// WithRunRecorder is configured, persists the plan's reasoning trail
// under a "plan_"-prefixed ID with the same proposal the buyer received.
func TestPlanCheckoutRecordsRunBestEffort(t *testing.T) {
	recorder := &fakeRunRecorder{}
	handler := NewHandler(NewBuyerAgent(NewDeterministicExtractor(), NewSearcher(fakeCatalog{}))).
		WithRunRecorder(recorder)

	body := strings.NewReader(`{"prompt":"I need wireless earbuds for my sister. Budget ₹25,000. I want good noise cancellation.","merchant":"merchant_001"}`)
	req := httptest.NewRequest(http.MethodPost, "/agent/checkout", body)
	w := httptest.NewRecorder()

	handler.PlanCheckout(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var plan CheckoutPlan
	if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(recorder.calls) != 1 {
		t.Fatalf("expected 1 SaveAgentPlan call, got %d", len(recorder.calls))
	}
	call := recorder.calls[0]
	if !strings.HasPrefix(call.id, "plan_") {
		t.Errorf("recorded plan id %q does not have the plan_ prefix", call.id)
	}
	if call.action.Merchant != "merchant_001" || len(call.action.Items) != 1 || call.action.Items[0] != plan.SelectedID {
		t.Errorf("recorded action %+v does not match plan (selected %s)", call.action, plan.SelectedID)
	}
	if len(call.steps) == 0 {
		t.Error("recorded steps should not be empty")
	}
}

// TestPlanCheckoutSucceedsWhenRunRecorderFails proves recordPlan is
// genuinely best-effort: a RunRecorder failure must never turn a
// working checkout proposal into a failed request.
func TestPlanCheckoutSucceedsWhenRunRecorderFails(t *testing.T) {
	recorder := &fakeRunRecorder{err: errors.New("db unavailable")}
	handler := NewHandler(NewBuyerAgent(NewDeterministicExtractor(), NewSearcher(fakeCatalog{}))).
		WithRunRecorder(recorder)

	body := strings.NewReader(`{"prompt":"I need wireless earbuds for my sister. Budget ₹25,000. I want good noise cancellation.","merchant":"merchant_001"}`)
	req := httptest.NewRequest(http.MethodPost, "/agent/checkout", body)
	w := httptest.NewRecorder()

	handler.PlanCheckout(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s -- a RunRecorder failure must not fail the request", w.Code, w.Body.String())
	}
	if len(recorder.calls) != 1 {
		t.Fatalf("expected the failing call to still be attempted once, got %d", len(recorder.calls))
	}
}

// TestPlanCheckoutWithoutRunRecorderStillSucceeds proves a Handler that
// never called WithRunRecorder (h.runs == nil, the zero value) behaves
// exactly as before item 16 -- no panic, no behavior change.
func TestPlanCheckoutWithoutRunRecorderStillSucceeds(t *testing.T) {
	handler := NewHandler(NewBuyerAgent(NewDeterministicExtractor(), NewSearcher(fakeCatalog{})))

	body := strings.NewReader(`{"prompt":"I need wireless earbuds for my sister. Budget ₹25,000. I want good noise cancellation.","merchant":"merchant_001"}`)
	req := httptest.NewRequest(http.MethodPost, "/agent/checkout", body)
	w := httptest.NewRecorder()

	handler.PlanCheckout(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}

// TestPlanCheckoutLoopRecordsRunBestEffort proves the same best-effort
// persistence happens on the POST /agent/loop path once it produces a
// Plan, using the same fake-LLM harness tool_loop_test.go established
// for testing ToolCallingAgent without a real LLM.
func TestPlanCheckoutLoopRecordsRunBestEffort(t *testing.T) {
	srv := serveLoopChat(t, `{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"propose_checkout","arguments":"{\"product_id\":\"airpods-pro-2\",\"reasoning\":\"Best ANC match under budget.\"}"}}]}`)
	defer srv.Close()

	recorder := &fakeRunRecorder{}
	loopAgent := NewToolCallingAgent("test-key", srv.URL, "test-model", loopTestDeps())
	handler := NewHandler(nil).WithLoopAgent(loopAgent).WithRunRecorder(recorder)

	body := strings.NewReader(`{"prompt":"wireless earbuds under 25000 for my sister","merchant":"merchant_001"}`)
	req := httptest.NewRequest(http.MethodPost, "/agent/loop", body)
	w := httptest.NewRecorder()

	handler.PlanCheckoutLoop(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if len(recorder.calls) != 1 {
		t.Fatalf("expected 1 SaveAgentPlan call, got %d", len(recorder.calls))
	}
	if !strings.HasPrefix(recorder.calls[0].id, "plan_") {
		t.Errorf("recorded plan id %q does not have the plan_ prefix", recorder.calls[0].id)
	}
}
