package agents

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/garinesaiajay/commerceos/policy"
	"github.com/garinesaiajay/commerceos/tools"
)

// BuyerAgent assembles checkout proposals. It NEVER calls the Payment
// Service — it only produces a policy.ProposedAction and hands it to
// the Policy Engine. It has no financial authority.
type BuyerAgent struct {
	extractor     IntentExtractor
	searcher      *Searcher
	conversations ConversationStore
}

func NewBuyerAgent(extractor IntentExtractor, searcher *Searcher) *BuyerAgent {
	return &BuyerAgent{
		extractor: extractor,
		searcher:  searcher,
	}
}

// WithConversationStore opts a BuyerAgent into conversation memory
// (PLAN-01-AGENTIC-CORE.md §3) and returns the same agent for chaining
// at construction time. Leaving this unset is fully supported —
// PlanCheckoutInConversation falls back to plain PlanCheckout when no
// store is configured, and PlanCheckout itself never depends on one.
func (a *BuyerAgent) WithConversationStore(store ConversationStore) *BuyerAgent {
	a.conversations = store
	return a
}

// CheckoutPlan is the agent's proposal plus the reasoning.
type CheckoutPlan struct {
	Intent     Intent                `json:"intent"`
	Proposal   policy.ProposedAction `json:"proposal"`
	SelectedID string                `json:"selected_product_id"`
	Reasoning  string                `json:"reasoning"`
	// Alternatives are the next-best matches the searcher also found for
	// this intent (Searcher.Search already ranks every match; previously
	// everything past results[0] was silently discarded the moment it was
	// computed). Never re-scored or re-ranked here -- same order
	// Search returned them in. Omitted, not an empty array, when there
	// were no other matches at all.
	Alternatives []AlternativeProduct `json:"alternatives,omitempty"`
	// ReasoningTrail is this plan's step-by-step audit trail (item 16,
	// PLAN-01-AGENTIC-CORE.md §4) -- the same policy.RunStep shape the
	// existing agent_actions-backed runs use. Handler persists this
	// best-effort via RunRecorder after a successful plan; it also
	// rides along on the wire response itself so a caller never has to
	// make a second request just to see how the pick was made.
	ReasoningTrail []policy.RunStep `json:"reasoning_trail,omitempty"`
}

// AlternativeProduct carries just enough catalog detail to render an
// alternative choice without a second round trip -- same shape as
// growth.SuggestedProduct for the same reason.
type AlternativeProduct struct {
	ProductID string `json:"product_id"`
	Title     string `json:"title"`
	Price     int64  `json:"price"`
	Currency  string `json:"currency"`
}

// maxAlternatives bounds how many next-best matches ride along with a
// plan. 2 is deliberately small: this is "here are a couple of other
// options," not a second catalog browse inside the agent panel.
const maxAlternatives = 2

// ErrNoSuitableProduct is returned when nothing matches the intent.
var ErrNoSuitableProduct = errors.New("no suitable product for intent")

// PlanCheckout turns a natural-language prompt into a Proposed Action.
// The agent names a product_id; it never writes price/quantity itself.
//
// This is the original, memoryless entry point: each call is extracted
// and searched in isolation, with no reference to any prior turn. It is
// kept unchanged (same signature, same behavior) for backward
// compatibility with any caller that doesn't have a conversation_id
// (cart_id) to key memory off of. PlanCheckoutInConversation below is
// the memory-aware entry point and delegates to the same planFromIntent
// helper this uses once it has a validated Intent.
func (a *BuyerAgent) PlanCheckout(
	ctx context.Context,
	prompt string,
	merchant string,
) (CheckoutPlan, error) {
	intent, err := a.extractor.Extract(ctx, prompt)
	if err != nil {
		return CheckoutPlan{}, err
	}

	// Ambiguous intent → safe no-op (clarification), never a guess.
	if intent.Clarify != "" {
		return CheckoutPlan{}, ErrAmbiguousIntent
	}

	return a.planFromIntent(ctx, intent, merchant)
}

// PlanCheckoutInConversation is PlanCheckout with conversation memory
// layered on top (PLAN-01-AGENTIC-CORE.md §3, ROADMAP-PRIORITIZED.md P0
// item 6). cartID doubles as the conversation_id: a buyer's cart already
// anchors their session, so no new identity system is needed.
//
// The new prompt is still extracted in isolation (a.extractor.Extract
// never sees prior turns directly), but the resulting Intent is then
// merged over the last known Intent stored for this cartID (see
// mergeIntent's doc comment for exactly what "merge" means and its
// limits) before validation. This is what turns "no, for my brother
// instead" — which alone has no budget or category and would otherwise
// fail validation from scratch — into a valid follow-up.
//
// If cartID is empty or no ConversationStore is configured, this
// degrades to plain PlanCheckout with zero behavior change — memory is
// strictly additive. A ConversationStore failure while reading history
// also degrades to plain PlanCheckout for this call, rather than
// failing the request: memory is an enhancement, not a dependency the
// agent's core job should ever be blocked on.
func (a *BuyerAgent) PlanCheckoutInConversation(
	ctx context.Context,
	cartID string,
	prompt string,
	merchant string,
) (CheckoutPlan, error) {
	if a.conversations == nil || cartID == "" {
		return a.PlanCheckout(ctx, prompt, merchant)
	}

	newIntent, err := a.extractor.Extract(ctx, prompt)
	// ErrAmbiguousIntent is a legitimate answer, not a hard failure --
	// same treatment FallbackExtractor/RacingExtractor already give it
	// one layer down. The intent returned alongside it may still carry
	// partial fields (see llm_extractor.go and deterministic_extractor.go's
	// analogous clarify returns), which is exactly what memory needs: a
	// standalone "no, for my brother instead" is ambiguous on its own,
	// but still worth merging into what the buyer already said instead
	// of aborting the whole conversation. Any other error (timeout,
	// network failure, malformed response) is still a hard failure,
	// unchanged from PlanCheckout's behavior.
	if err != nil && !errors.Is(err, ErrAmbiguousIntent) {
		return CheckoutPlan{}, err
	}

	prevIntent, hadPrev, histErr := a.conversations.LastKnownIntent(ctx, cartID)
	if histErr != nil {
		log.Printf("[agents] conversation history unavailable for cart %s, continuing without memory: %v", cartID, histErr)
		return a.PlanCheckout(ctx, prompt, merchant)
	}

	// Only fold this turn into the previous one when it actually
	// contributed some recognizable signal (budget, category, priority,
	// or recipient) -- see hasSignal's doc comment. A prompt the
	// extractor couldn't parse at all (e.g. "i want a pair of shoes"
	// against a catalog with no shoes category) must never silently
	// inherit the *entire* previous intent just because ValidateIntent
	// happens to pass on those stale values -- that previously made an
	// unrelated new request look like a confidently-answered
	// continuation of the old one instead of asking for clarification
	// (files/AGENTIC-INTEGRITY-AUDIT-2026-09-04.md, Finding A).
	merged := newIntent
	if hadPrev && hasSignal(newIntent) {
		merged = mergeIntent(prevIntent, newIntent)
	} else {
		merged.Clarify = ""
	}

	if err := ValidateIntent(merged); err != nil {
		// Still incomplete even with prior context -- same safe no-op
		// as PlanCheckout's clarify path, just recorded in history so
		// the buyer's next attempt has this turn to build on too.
		//
		// saveIntent is keyed on whether THIS turn contributed a field,
		// not on hadPrev alone: a zero-signal prompt (this whole guard's
		// reason for existing, see above) must never overwrite a good
		// previous snapshot with this turn's empty one, or the buyer's
		// next, on-topic follow-up would have nothing real left to
		// build on.
		a.recordTurn(ctx, cartID, prompt, merged, hasSignal(newIntent))
		a.recordAssistantTurn(ctx, cartID, clarifyText(newIntent))
		return CheckoutPlan{}, ErrAmbiguousIntent
	}

	plan, err := a.planFromIntent(ctx, merged, merchant)

	a.recordTurn(ctx, cartID, prompt, merged, true)

	if err != nil {
		a.recordAssistantTurn(ctx, cartID, err.Error())
		return CheckoutPlan{}, err
	}

	a.recordAssistantTurn(ctx, cartID, plan.Reasoning)

	return plan, nil
}

// clarifyText prefers the extractor's own clarify question (it may name
// specifically what's missing) and falls back to a generic prompt for
// the rare case an extractor returns Clarify without a message.
func clarifyText(intent Intent) string {
	if intent.Clarify != "" {
		return intent.Clarify
	}
	return "I need a bit more information — what would you like to buy, and what's the budget?"
}

// recordTurn persists the buyer's message and, when saveIntent is true,
// the merged Intent snapshot alongside it. Store failures are logged,
// not returned -- conversation memory must never fail the buyer-facing
// request it's layered on top of.
func (a *BuyerAgent) recordTurn(ctx context.Context, cartID, prompt string, intent Intent, saveIntent bool) {
	var snapshot *Intent
	if saveIntent {
		snapshot = &intent
	}
	if err := a.conversations.AppendTurn(ctx, cartID, "user", prompt, snapshot); err != nil {
		log.Printf("[agents] failed to record user turn for cart %s: %v", cartID, err)
	}
}

func (a *BuyerAgent) recordAssistantTurn(ctx context.Context, cartID, content string) {
	if err := a.conversations.AppendTurn(ctx, cartID, "assistant", content, nil); err != nil {
		log.Printf("[agents] failed to record assistant turn for cart %s: %v", cartID, err)
	}
}

// planFromIntent is the shared tail of both PlanCheckout and
// PlanCheckoutInConversation: given an already-validated Intent, search
// the catalog, pick the top match, and build the ProposedAction plus
// reasoning. Neither entry point writes price/quantity itself here —
// the agent only ever names a product_id.
func (a *BuyerAgent) planFromIntent(ctx context.Context, intent Intent, merchant string) (CheckoutPlan, error) {
	// Search's parameter is tools.SearchFilter, not agents.Intent (see
	// backend/agents/search.go's doc comment) -- Intent's Clarify field
	// has no meaning to a search, so this only ever carries the four
	// fields the two types actually share.
	results, err := a.searcher.Search(ctx, tools.SearchFilter{
		Budget:    intent.Budget,
		Category:  intent.Category,
		Priority:  intent.Priority,
		Recipient: intent.Recipient,
	})
	if err != nil {
		return CheckoutPlan{}, err
	}

	if len(results) == 0 {
		return CheckoutPlan{}, ErrNoSuitableProduct
	}

	// The agent selects the top-ranked product by name only.
	top := results[0].Product

	var alternatives []AlternativeProduct
	for _, r := range results[1:] {
		if len(alternatives) >= maxAlternatives {
			break
		}
		alternatives = append(alternatives, AlternativeProduct{
			ProductID: r.Product.ID,
			Title:     r.Product.Title,
			Price:     r.Product.Price.Amount,
			Currency:  r.Product.Price.Currency,
		})
	}

	proposal := policy.ProposedAction{
		Action:   "CREATE_ORDER",
		Amount:   top.Price.Amount,
		Currency: top.Price.Currency,
		Merchant: merchant,
		Items:    []string{top.ID},
	}

	// intent.Priority is optional (only budget + category are required --
	// see ValidateIntent), so the sentence needs two honest shapes: most
	// requests never name a specific priority, and "matching priority
	// within budget ₹X" (empty %s) read as a broken sentence rather than
	// explaining anything.
	var reasoning string
	if intent.Priority != "" {
		reasoning = fmt.Sprintf(
			"Selected %s (₹%d) — best match for your %s priority in %s, within budget ₹%d.",
			top.Title,
			top.Price.Amount/100, // paise → rupees for display
			intent.Priority,
			intent.Category,
			intent.Budget, // already rupees
		)
	} else {
		reasoning = fmt.Sprintf(
			"Selected %s (₹%d) — best-priced match in %s within your ₹%d budget.",
			top.Title,
			top.Price.Amount/100,
			intent.Category,
			intent.Budget,
		)
	}

	return CheckoutPlan{
		Intent:         intent,
		Proposal:       proposal,
		SelectedID:     top.ID,
		Reasoning:      reasoning,
		Alternatives:   alternatives,
		ReasoningTrail: buildReasoningTrail(intent, alternatives, reasoning),
	}, nil
}

// buildReasoningTrail assembles planFromIntent's audit trail (item 16):
// intent_extracted (what the extractor/merge produced), an optional
// alternatives_considered step (omitted entirely when the searcher found
// nothing else -- an empty step would just be noise), and proposed (the
// same sentence already shown to the buyer as Reasoning, so the trail
// never says anything the buyer wasn't already told). All three stages
// happen within a single synchronous function call with no meaningful
// gap between them, so -- unlike the multi-second, real-wall-clock gaps
// GetRun's existing agent_actions timeline reconstructs -- they share
// one timestamp rather than fabricating distinct ones.
func buildReasoningTrail(intent Intent, alternatives []AlternativeProduct, reasoning string) []policy.RunStep {
	now := time.Now()

	steps := []policy.RunStep{{
		Stage: "intent_extracted",
		Detail: fmt.Sprintf(
			"category=%s budget=₹%d priority=%s recipient=%s source=%s",
			orDash(intent.Category), intent.Budget, orDash(intent.Priority), orDash(intent.Recipient), orDash(intent.Source),
		),
		Timestamp: now,
	}}

	if len(alternatives) > 0 {
		names := make([]string, len(alternatives))
		for i, alt := range alternatives {
			names[i] = alt.Title
		}
		steps = append(steps, policy.RunStep{
			Stage:     "alternatives_considered",
			Detail:    "also considered: " + strings.Join(names, ", "),
			Timestamp: now,
		})
	}

	steps = append(steps, policy.RunStep{
		Stage:     "proposed",
		Detail:    reasoning,
		Timestamp: now,
	})

	return steps
}

// orDash renders an optional Intent field for a reasoning-trail detail
// string -- "" reads as a broken sentence fragment, "—" reads as
// honestly absent.
func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
