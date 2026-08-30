package agents

import (
	"context"
	"errors"
	"log"
	"time"
)

// DefaultRaceWindow bounds how long RacingExtractor waits on the
// primary (LLM) extractor before returning the fallback's answer
// instead. Deliberately much shorter than the LLM extractor's own
// internal HTTP timeout (llmRequestTimeout, 60s, llm_extractor.go) --
// that 60s exists to tolerate real provider latency variance without
// failing the *request*; this bounds how long a human is made to stare
// at "Thinking..." before getting *an* answer, which is a completely
// different, much stricter budget. 3.5s is comfortably past typical
// gpt-4o-mini-class response times while still feeling responsive.
const DefaultRaceWindow = 3500 * time.Millisecond

// RacingExtractor runs a primary (LLM) extractor and a fast fallback
// (in practice, the deterministic rule-based one) concurrently, and
// returns whichever is usable first -- the primary if it answers within
// raceWindow, the fallback otherwise. This replaces FallbackExtractor's
// serial "wait up to 60s, then fall back" behavior for the specific
// failure mode that behavior didn't cover: the primary isn't erroring,
// it's just slow. FallbackExtractor itself is unchanged and still used
// for its own documented purpose (recovering from a real primary
// failure); RacingExtractor is a separate, additive wrapper that also
// recovers from a real failure (see Extract below) but additionally
// stops waiting on a merely-slow primary.
//
// Known, deliberate limitation: if the primary answers *after* the race
// window has already produced the fallback's result, that later answer
// is discarded -- there is no conversation state yet (see
// files/PLAN-01-AGENTIC-CORE.md §3) to revise an already-shown answer
// into "actually, here's a better match." Once that lands, this is the
// natural place to surface it as a follow-up turn instead of silently
// dropping it.
type RacingExtractor struct {
	primary    IntentExtractor
	fallback   IntentExtractor
	raceWindow time.Duration
}

// NewRacingExtractor builds the wrapper with DefaultRaceWindow. Panics
// if either argument is nil, matching NewFallbackExtractor's contract.
func NewRacingExtractor(primary, fallback IntentExtractor) *RacingExtractor {
	return NewRacingExtractorWithWindow(primary, fallback, DefaultRaceWindow)
}

// NewRacingExtractorWithWindow is NewRacingExtractor with an explicit
// race window, mainly so tests don't have to wait DefaultRaceWindow's
// full 3.5s to exercise the timeout path.
func NewRacingExtractorWithWindow(primary, fallback IntentExtractor, raceWindow time.Duration) *RacingExtractor {
	if primary == nil || fallback == nil {
		panic("agents: NewRacingExtractor requires non-nil primary and fallback")
	}
	return &RacingExtractor{primary: primary, fallback: fallback, raceWindow: raceWindow}
}

type racingResult struct {
	intent Intent
	err    error
}

// Extract starts the primary in its own goroutine and waits at most
// raceWindow for it. ErrAmbiguousIntent from the primary is a
// legitimate, correct answer (same as FallbackExtractor) and is
// returned as-is, not treated as a reason to fall back. Any other
// primary error, or the race window elapsing first, both resolve to the
// fallback's answer -- the caller can't distinguish "primary failed"
// from "primary was too slow" from its return value alone, which is
// intentional: both are "the buyer gets the deterministic answer now."
func (e *RacingExtractor) Extract(ctx context.Context, prompt string) (Intent, error) {
	resultCh := make(chan racingResult, 1)

	go func() {
		intent, err := e.primary.Extract(ctx, prompt)
		resultCh <- racingResult{intent: intent, err: err}
	}()

	select {
	case res := <-resultCh:
		if res.err == nil {
			return res.intent, nil
		}
		if errors.Is(res.err, ErrAmbiguousIntent) {
			return res.intent, res.err
		}
		log.Printf("[agents] primary intent extractor failed, using fallback: %v", res.err)
		return e.fallback.Extract(ctx, prompt)

	case <-time.After(e.raceWindow):
		log.Printf("[agents] primary intent extractor exceeded %s race window, using fallback now", e.raceWindow)
		return e.fallback.Extract(ctx, prompt)
	}
}
