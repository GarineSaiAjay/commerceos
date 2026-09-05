package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Intent is the strict, validated output schema of intent extraction.
type Intent struct {
	Budget    int64  `json:"budget"`
	Category  string `json:"category"`
	Priority  string `json:"priority"`
	Recipient string `json:"recipient"`
	Clarify   string `json:"clarify,omitempty"`
	// Exclude holds literal phrases the buyer explicitly ruled out --
	// "not X" / "not the X" -- e.g. "i want a airtag not airtag anti
	// lost strap" excludes "airtag anti lost strap". Populated by
	// BuyerAgent (ParseExclusions, prompt_signals.go) from the RAW
	// prompt after extraction, not by DeterministicExtractor or
	// LLMExtractor themselves: negation is a catalog-agnostic text
	// operation on the buyer's own words, the same for every extractor,
	// so it deliberately isn't part of rawIntent's JSON schema. Consumed
	// as a hard filter by tools.SearchFilter.Exclude -- an excluded
	// product is removed from candidates entirely, never merely
	// down-ranked, because an explicit correction must always win over
	// whatever the soft category/priority signals alone would pick.
	Exclude []string `json:"exclude,omitempty"`
	// Terms holds the buyer's own significant words from the raw prompt
	// (ExtractTerms, prompt_signals.go), also stamped by BuyerAgent for
	// the same reason as Exclude above. Consumed as a soft grounding
	// signal by tools.SearchFilter.Terms -- see accessoryQualifiers'
	// doc comment in tools/search.go for what it's for: telling an
	// accessory FOR a product (a case, a strap) apart from the product
	// itself, which bare category/use_cases matching alone cannot do
	// (both are tagged the same category, and the accessory is
	// typically cheaper, so price-proximity scoring always preferred it
	// over the product every buyer actually meant).
	Terms []string `json:"terms,omitempty"`
	// Source identifies which extractor actually produced this Intent --
	// "llm" or "deterministic". Set by the concrete extractors
	// (DeterministicExtractor, LLMExtractor) themselves, never by
	// ParseIntentJSON: it describes which CODE PATH answered, so it must
	// never be something an LLM's own JSON output can claim to be. A
	// wrapper (RacingExtractor, FallbackExtractor) just returns whichever
	// concrete extractor's Intent it picked, unchanged, so this
	// propagates automatically without those wrappers needing to know
	// about it. Threaded through to the buyer-facing reasoning trail and
	// API response so a judge (or the buyer) can tell whether an answer
	// came from a real LLM call or the keyword-matching fallback --
	// previously indistinguishable (files/AGENTIC-INTEGRITY-AUDIT-2026-09-04.md,
	// Finding C).
	Source string `json:"source,omitempty"`
}

// ErrAmbiguousIntent is returned when the prompt is too vague to act on.
var ErrAmbiguousIntent = errors.New("ambiguous intent: clarification required")

// Intent.Source values -- see Intent.Source's doc comment. Named
// constants so DeterministicExtractor/LLMExtractor can't typo the string
// two different ways.
const (
	sourceLLM           = "llm"
	sourceDeterministic = "deterministic"
)

// ValidateIntent strictly validates the schema. Malformed output is
// rejected before anything else touches it.
func ValidateIntent(i Intent) error {
	if i.Clarify != "" {
		return nil // clarification request is a valid safe no-op
	}

	if i.Budget <= 0 {
		return fmt.Errorf("invalid intent: budget must be positive")
	}
	if i.Category == "" {
		return fmt.Errorf("invalid intent: category required")
	}
	return nil
}

// IntentExtractor turns a natural-language prompt into a validated
// Intent. Implementations must return ErrAmbiguousIntent for vague
// prompts rather than guessing.
type IntentExtractor interface {
	Extract(ctx context.Context, prompt string) (Intent, error)
}

// rawIntent is the wire shape before validation.
type rawIntent struct {
	Budget    *int64  `json:"budget"`
	Category  *string `json:"category"`
	Priority  *string `json:"priority"`
	Recipient *string `json:"recipient"`
	Clarify   *string `json:"clarify,omitempty"`
}

// ParseIntentJSON validates the JSON shape and field types strictly.
func ParseIntentJSON(data []byte) (Intent, error) {
	var raw rawIntent

	if err := json.Unmarshal(data, &raw); err != nil {
		return Intent{}, fmt.Errorf("malformed intent JSON: %w", err)
	}

	if raw.Clarify != nil {
		// Whatever fields the model DID provide alongside a clarify
		// request are preserved, not discarded -- a single-shot caller
		// never looks past Clarify != "" so this is behavior-neutral
		// there; BuyerAgent.PlanCheckoutInConversation (conversation
		// memory, PLAN-01-AGENTIC-CORE.md §3) is what uses them, the
		// same reasoning as DeterministicExtractor's analogous clarify
		// return in deterministic_extractor.go.
		return Intent{
			Budget:    derefInt64(raw.Budget),
			Category:  deref(raw.Category),
			Priority:  deref(raw.Priority),
			Recipient: deref(raw.Recipient),
			Clarify:   *raw.Clarify,
		}, nil
	}

	if raw.Budget == nil || raw.Category == nil {
		return Intent{}, ErrAmbiguousIntent
	}

	intent := Intent{
		Budget:    *raw.Budget,
		Category:  *raw.Category,
		Priority:  deref(raw.Priority),
		Recipient: deref(raw.Recipient),
	}

	if err := ValidateIntent(intent); err != nil {
		return Intent{}, err
	}

	return intent, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefInt64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}
