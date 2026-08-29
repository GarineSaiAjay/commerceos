package agents

import (
	"context"
	"errors"
	"log"
)

// FallbackExtractor wraps a primary IntentExtractor (in practice, the
// LLM-backed one) and falls back to a secondary one (in practice, the
// deterministic rule-based one) whenever the primary fails for any
// reason other than a legitimate "too vague, please clarify" answer.
//
// Without this, POST /agent/checkout had exactly one path: call the
// configured LLM, and if that call is slow, rate-limited, unreachable,
// or returns something ParseIntentJSON rejects, the whole request fails
// and the buyer sees "the assistant is temporarily unavailable" -- even
// though DeterministicExtractor is fully capable of answering plenty of
// ordinary requests ("earbuds for my sister under 25000, battery life")
// on its own, instantly, with no network dependency at all. This keeps
// the shopping agent responsive and available even when the LLM
// provider is having a bad day, instead of the buyer's experience being
// hostage to a third-party API's uptime.
type FallbackExtractor struct {
	primary  IntentExtractor
	fallback IntentExtractor
}

// NewFallbackExtractor builds the wrapper. Panics if either argument is
// nil -- both are required for this to make sense; callers should only
// construct it once they have a real primary (see NewLLMExtractorFromEnv)
// to fall back *from*.
func NewFallbackExtractor(primary, fallback IntentExtractor) *FallbackExtractor {
	if primary == nil || fallback == nil {
		panic("agents: NewFallbackExtractor requires non-nil primary and fallback")
	}
	return &FallbackExtractor{primary: primary, fallback: fallback}
}

// Extract tries the primary extractor first. ErrAmbiguousIntent is a
// legitimate, correct answer (the primary understood the request just
// fine and correctly decided it needs clarification) -- that is returned
// as-is, never treated as a failure to recover from. Any other error
// (timeout, network failure, an API error, a malformed/invalid response)
// falls back to the secondary extractor instead of failing the request.
func (f *FallbackExtractor) Extract(ctx context.Context, prompt string) (Intent, error) {
	intent, err := f.primary.Extract(ctx, prompt)
	if err == nil {
		return intent, nil
	}
	if errors.Is(err, ErrAmbiguousIntent) {
		return intent, err
	}

	log.Printf("[agents] primary intent extractor failed, falling back to deterministic: %v", err)
	return f.fallback.Extract(ctx, prompt)
}
