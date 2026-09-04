package agents

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRejectionNarratorNilReceiverReturnsUnchanged proves a nil
// *RejectionNarrator (no OPENROUTER_API_KEY configured, same
// convention NewRejectionNarratorFromEnv establishes) is a safe no-op
// -- Narrate must never panic on a nil receiver, and must return the
// deterministic sentence completely unchanged.
func TestRejectionNarratorNilReceiverReturnsUnchanged(t *testing.T) {
	var n *RejectionNarrator
	const deterministic = "The amount ₹26,900 exceeds the configured ceiling of ₹25,000. I did not proceed, and no payment action was attempted."

	got := n.Narrate(context.Background(), deterministic)
	if got != deterministic {
		t.Errorf("Narrate on nil receiver = %q, want unchanged %q", got, deterministic)
	}
}

// TestRejectionNarratorEmptyInputReturnsUnchanged proves an empty
// deterministic string is returned as-is without ever making an LLM
// call (there's nothing meaningful to rephrase).
func TestRejectionNarratorEmptyInputReturnsUnchanged(t *testing.T) {
	n := NewRejectionNarrator("test-key", "http://unused.invalid", "test-model")
	got := n.Narrate(context.Background(), "")
	if got != "" {
		t.Errorf("Narrate(\"\") = %q, want \"\"", got)
	}
}

// TestRejectionNarratorRephrasesOnSuccess proves a real (fake-server)
// LLM response is returned as the rephrased sentence.
func TestRejectionNarratorRephrasesOnSuccess(t *testing.T) {
	const rephrased = "This purchase is a little over your ₹25,000 limit -- it comes to ₹26,900, so it wasn't processed."
	srv := serveChat(rephrased)
	defer srv.Close()

	n := NewRejectionNarrator("test-key", srv.URL, "test-model")
	got := n.Narrate(context.Background(), "The amount ₹26,900 exceeds the configured ceiling of ₹25,000. I did not proceed, and no payment action was attempted.")
	if got != rephrased {
		t.Errorf("Narrate() = %q, want the rephrased sentence %q", got, rephrased)
	}
}

// TestRejectionNarratorFallsBackOnLLMError proves the deterministic
// sentence ships unchanged when the LLM call fails outright (a non-200
// response here -- the same failure family PlanCheckoutLoop's infra-
// error mapping guards against elsewhere in this package) rather than
// ever surfacing an error, an empty string, or a panic to the caller.
func TestRejectionNarratorFallsBackOnLLMError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	const deterministic = "The amount ₹26,900 exceeds the configured ceiling of ₹25,000. I did not proceed, and no payment action was attempted."
	n := NewRejectionNarrator("test-key", srv.URL, "test-model")
	got := n.Narrate(context.Background(), deterministic)
	if got != deterministic {
		t.Errorf("Narrate() on LLM failure = %q, want unchanged %q", got, deterministic)
	}
}

// TestRejectionNarratorFallsBackWhenCostGuardTripped proves a tripped
// shared CostGuard blocks the rephrase the same way it blocks
// LLMExtractor.Extract and ToolCallingAgent.chat -- Narrate still
// returns the deterministic sentence unchanged rather than an error,
// since explain_decision has no error path to surface one through.
func TestRejectionNarratorFallsBackWhenCostGuardTripped(t *testing.T) {
	srv := serveChat("this should never be requested")
	defer srv.Close()

	guard := NewCostGuard(0.00000001) // trips on the very first Record
	guard.Record(1000, 1000)          // pre-spend past the tiny budget

	n := NewRejectionNarrator("test-key", srv.URL, "test-model").WithCostGuard(guard)
	const deterministic = "The merchant merchant_002 is not allowlisted. I did not proceed, and no payment action was attempted."
	got := n.Narrate(context.Background(), deterministic)
	if got != deterministic {
		t.Errorf("Narrate() with tripped cost guard = %q, want unchanged %q", got, deterministic)
	}
}
