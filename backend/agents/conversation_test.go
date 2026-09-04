package agents

import (
	"context"
	"errors"
	"testing"
)

// memoryConversationStore is a minimal in-memory ConversationStore test
// double -- no database, just enough to exercise
// BuyerAgent.PlanCheckoutInConversation's memory logic directly.
type memoryConversationStore struct {
	turns  map[string][]ConversationTurn
	intent map[string]Intent
	found  map[string]bool
}

func newMemoryConversationStore() *memoryConversationStore {
	return &memoryConversationStore{
		turns:  make(map[string][]ConversationTurn),
		intent: make(map[string]Intent),
		found:  make(map[string]bool),
	}
}

func (s *memoryConversationStore) AppendTurn(ctx context.Context, cartID, role, content string, intent *Intent) error {
	s.turns[cartID] = append(s.turns[cartID], ConversationTurn{Role: role, Content: content})
	if intent != nil {
		s.intent[cartID] = *intent
		s.found[cartID] = true
	}
	return nil
}

func (s *memoryConversationStore) History(ctx context.Context, cartID string) ([]ConversationTurn, error) {
	return s.turns[cartID], nil
}

func (s *memoryConversationStore) LastKnownIntent(ctx context.Context, cartID string) (Intent, bool, error) {
	return s.intent[cartID], s.found[cartID], nil
}

// TestHasSignal documents hasSignal's contract directly: true iff the
// extractor recognized at least one field, regardless of Clarify.
func TestHasSignal(t *testing.T) {
	cases := []struct {
		name   string
		intent Intent
		want   bool
	}{
		{"all empty", Intent{}, false},
		{"clarify set but nothing else", Intent{Clarify: "what do you want?"}, false},
		{"budget only", Intent{Budget: 3000}, true},
		{"category only", Intent{Category: "laptop"}, true},
		{"priority only", Intent{Priority: "battery_life"}, true},
		{"recipient only", Intent{Recipient: "sister"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasSignal(tc.intent); got != tc.want {
				t.Fatalf("hasSignal(%+v) = %v, want %v", tc.intent, got, tc.want)
			}
		})
	}
}

// TestPlanCheckoutInConversation_UnparseableFollowUpDoesNotReuseStaleIntent
// is the regression test for Finding A of
// files/AGENTIC-INTEGRITY-AUDIT-2026-09-04.md: a live test of "i want a
// pair of shoes" right after a valid laptop-accessory request returned a
// confident, wrong proposal ("Laptop Screen Cleaning Kit ... best-priced
// match in laptop within your ₹3000 budget") because mergeIntent silently
// inherited the entire previous intent when the new prompt's extractor
// found nothing recognizable at all. It must now ask for clarification
// instead -- and must not erase the good prior intent while doing so.
func TestPlanCheckoutInConversation_UnparseableFollowUpDoesNotReuseStaleIntent(t *testing.T) {
	store := newMemoryConversationStore()
	agent := NewBuyerAgent(NewDeterministicExtractor(), NewSearcher(fakeCatalog{})).
		WithConversationStore(store)

	const cartID = "cart_shoes_regression"

	// First turn: a genuine, valid request. Establishes a real prior
	// intent (category=laptop budget=3000) for the bug to (mis)use.
	first, err := agent.PlanCheckoutInConversation(
		context.Background(), cartID,
		"I need a laptop stand, budget ₹3000", "merchant_001",
	)
	if err != nil {
		t.Fatalf("first turn: unexpected error: %v", err)
	}
	if first.Intent.Category != "laptop" || first.Intent.Budget != 3000 {
		t.Fatalf("first turn: expected category=laptop budget=3000, got %+v", first.Intent)
	}

	// Second turn: completely unrelated, and nothing in the catalog
	// resembles "shoes" -- the extractor recognizes NOTHING here (no
	// budget, no category). Before the fix, this silently answered with
	// the entire prior intent (laptop/3000) instead of admitting it
	// didn't understand "shoes."
	_, err = agent.PlanCheckoutInConversation(
		context.Background(), cartID,
		"i want a pair of shoes", "merchant_001",
	)
	if !errors.Is(err, ErrAmbiguousIntent) {
		t.Fatalf("second turn: expected ErrAmbiguousIntent, got %v", err)
	}

	// The zero-signal turn must not have clobbered the good prior
	// snapshot -- a later, genuine follow-up still has something real
	// to build on.
	stillKnown, found, err := store.LastKnownIntent(context.Background(), cartID)
	if err != nil {
		t.Fatalf("LastKnownIntent: unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected the first turn's intent to still be remembered")
	}
	if stillKnown.Category != "laptop" || stillKnown.Budget != 3000 {
		t.Fatalf("expected memory to still hold category=laptop budget=3000, got %+v", stillKnown)
	}

	// And a real follow-up on the ORIGINAL topic still merges correctly
	// -- this guard doesn't break legitimate memory, it only refuses to
	// use it for a turn that shares nothing with it.
	third, err := agent.PlanCheckoutInConversation(
		context.Background(), cartID,
		"actually make it under 2000", "merchant_001",
	)
	if err != nil {
		t.Fatalf("third turn: unexpected error: %v", err)
	}
	if third.Intent.Category != "laptop" || third.Intent.Budget != 2000 {
		t.Fatalf("third turn: expected category=laptop budget=2000, got %+v", third.Intent)
	}
}
