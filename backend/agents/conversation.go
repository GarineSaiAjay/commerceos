package agents

import (
	"context"
	"time"
)

// ConversationTurn is one stored message in an agent conversation.
type ConversationTurn struct {
	Role      string // "user" or "assistant"
	Content   string
	CreatedAt time.Time
}

// ConversationStore persists agent chat turns per cart, so a follow-up
// prompt ("actually, make it cheaper" / "no, for my brother instead")
// can be understood in the context of what the buyer already said,
// instead of being extracted from scratch and immediately failing
// validation because the follow-up alone doesn't restate everything.
//
// conversation_id is deliberately the cart_id -- a buyer's cart already
// anchors their session, so no new identity system is needed (see
// PLAN-01-AGENTIC-CORE.md §3).
//
// A nil ConversationStore is a valid, supported state: BuyerAgent falls
// back to its original single-shot PlanCheckout behavior when no store
// is configured, exactly like growth.SuggestHandler's nil-safe
// DismissalStore. Conversation memory is an enhancement layered on top
// of the existing agent, never a new dependency it requires.
type ConversationStore interface {
	// AppendTurn records one turn. intent is optional (nil on assistant
	// turns, and on user turns where extraction produced nothing worth
	// remembering) -- when present, it is the merged Intent snapshot
	// after this turn, so the next turn's LastKnownIntent can build on
	// it. Store failures here must never abort the caller's request --
	// memory is an enhancement, not a correctness dependency.
	AppendTurn(ctx context.Context, cartID, role, content string, intent *Intent) error

	// History returns every stored turn for cartID, oldest first.
	History(ctx context.Context, cartID string) ([]ConversationTurn, error)

	// LastKnownIntent returns the most recently stored Intent snapshot
	// for cartID, if any. found is false when the conversation has no
	// prior turn with a stored intent (e.g. its very first message).
	LastKnownIntent(ctx context.Context, cartID string) (intent Intent, found bool, err error)
}

// mergeIntent layers a newly-extracted intent over the last known
// intent for the conversation: any field the new turn actually
// specifies wins; any field it left unset carries forward from the
// prior turn. This is what turns "no, for my brother instead" -- which,
// extracted in isolation, has no budget or category and would fail
// ValidateIntent on its own -- into a valid follow-up that keeps the
// budget and category the buyer already gave and only updates the
// recipient.
//
// Known, deliberately out-of-scope limitation: this is field-level slot
// filling, not semantic understanding. "make it cheaper" carries no
// parseable number for DeterministicExtractor (and no arithmetic
// relative to the prior budget for either extractor, since Extract
// only ever sees the new prompt in isolation) -- teaching the agent to
// revise a numeric field relative to its own prior answer needs real
// conversation-aware reasoning, which is PLAN-01-AGENTIC-CORE.md §2's
// bounded tool-calling loop (P1), not this scoped P0 fix.
func mergeIntent(prev Intent, next Intent) Intent {
	merged := prev

	if next.Budget > 0 {
		merged.Budget = next.Budget
	}
	if next.Category != "" {
		merged.Category = next.Category
	}
	if next.Priority != "" {
		merged.Priority = next.Priority
	}
	if next.Recipient != "" {
		merged.Recipient = next.Recipient
	}

	// Clarify is never part of merged state -- it is a per-turn signal
	// from the extractor, not something to carry forward or persist.
	merged.Clarify = ""

	return merged
}

// hasSignal reports whether the extractor recognized ANYTHING at all in
// this turn's prompt -- at least one of budget, category, priority, or
// recipient. It is deliberately blind to Clarify: DeterministicExtractor
// (and the LLM extractor's analogous case) sets Clarify precisely when it
// found nothing else either, so checking the four data fields alone is
// equivalent and keeps this function usable without first knowing which
// extractor produced the Intent.
//
// This is what PlanCheckoutInConversation uses to decide whether a new
// turn is a genuine follow-up worth merging into the prior intent, or a
// prompt the extractor couldn't parse at all -- e.g. "i want a pair of
// shoes" against a catalog with no shoes category. Folding a zero-signal
// turn into the previous intent via mergeIntent would silently answer an
// unrelated new request with a stale category and budget instead of
// admitting it wasn't understood (see
// files/AGENTIC-INTEGRITY-AUDIT-2026-09-04.md, Finding A).
func hasSignal(i Intent) bool {
	return i.Budget > 0 || i.Category != "" || i.Priority != "" || i.Recipient != ""
}
