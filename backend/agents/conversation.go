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
	// Source reflects which extractor produced the merged intent's
	// newest information. This mirrors the other fields' "next wins
	// when present" rule rather than blindly overwriting: mergeIntent is
	// only ever called by PlanCheckoutInConversation when hasSignal(next)
	// is already true (see buyer_agent.go), so next.Source is always set
	// in practice -- the fallback to prev.Source just keeps this
	// function correct on its own terms if that ever changes.
	if next.Source != "" {
		merged.Source = next.Source
	}

	// Clarify is never part of merged state -- it is a per-turn signal
	// from the extractor, not something to carry forward or persist.
	merged.Clarify = ""

	return merged
}
