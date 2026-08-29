package agents

import (
	"context"
	"errors"
	"testing"
)

// stubExtractor is a scripted IntentExtractor for testing FallbackExtractor
// in isolation, with no real LLM/HTTP involved.
type stubExtractor struct {
	intent Intent
	err    error
	calls  int
}

func (s *stubExtractor) Extract(ctx context.Context, prompt string) (Intent, error) {
	s.calls++
	return s.intent, s.err
}

// TestFallbackExtractorUsesPrimaryOnSuccess proves the fallback is never
// consulted when the primary succeeds.
func TestFallbackExtractorUsesPrimaryOnSuccess(t *testing.T) {
	primary := &stubExtractor{intent: Intent{Budget: 25000, Category: "earbuds"}}
	fallback := &stubExtractor{intent: Intent{Budget: 1, Category: "should-not-be-used"}}

	f := NewFallbackExtractor(primary, fallback)
	intent, err := f.Extract(context.Background(), "earbuds under 25000")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if intent.Category != "earbuds" {
		t.Fatalf("expected the primary's intent, got %+v", intent)
	}
	if fallback.calls != 0 {
		t.Fatalf("expected fallback not to be called, was called %d times", fallback.calls)
	}
}

// TestFallbackExtractorFallsBackOnError proves any non-ambiguous error
// from the primary (timeout, network failure, bad response, ...) is
// recovered by the fallback instead of failing the whole request.
func TestFallbackExtractorFallsBackOnError(t *testing.T) {
	primary := &stubExtractor{err: errors.New("llm request failed: context deadline exceeded")}
	fallback := &stubExtractor{intent: Intent{Budget: 5000, Category: "laptop"}}

	f := NewFallbackExtractor(primary, fallback)
	intent, err := f.Extract(context.Background(), "laptop under 5000")
	if err != nil {
		t.Fatalf("expected the fallback's success to be returned with no error, got %v", err)
	}
	if intent.Category != "laptop" {
		t.Fatalf("expected the fallback's intent, got %+v", intent)
	}
	if fallback.calls != 1 {
		t.Fatalf("expected fallback to be called exactly once, got %d", fallback.calls)
	}
}

// TestFallbackExtractorPropagatesAmbiguous proves ErrAmbiguousIntent from
// the primary is treated as a valid answer, not a failure -- the
// fallback must not be consulted, and the error must propagate as-is so
// the caller still asks the buyer to clarify.
func TestFallbackExtractorPropagatesAmbiguous(t *testing.T) {
	primary := &stubExtractor{err: ErrAmbiguousIntent}
	fallback := &stubExtractor{intent: Intent{Budget: 1, Category: "should-not-be-used"}}

	f := NewFallbackExtractor(primary, fallback)
	_, err := f.Extract(context.Background(), "buy me something")
	if !errors.Is(err, ErrAmbiguousIntent) {
		t.Fatalf("expected ErrAmbiguousIntent, got %v", err)
	}
	if fallback.calls != 0 {
		t.Fatalf("expected fallback not to be called for an ambiguous primary answer, was called %d times", fallback.calls)
	}
}

// TestFallbackExtractorPropagatesFallbackError proves that if BOTH
// extractors fail, the fallback's own error is what the caller sees
// (there's nowhere further to fall back to).
func TestFallbackExtractorPropagatesFallbackError(t *testing.T) {
	primary := &stubExtractor{err: errors.New("primary down")}
	fallbackErr := errors.New("invalid intent: budget must be positive")
	fallback := &stubExtractor{err: fallbackErr}

	f := NewFallbackExtractor(primary, fallback)
	_, err := f.Extract(context.Background(), "gibberish")
	if !errors.Is(err, fallbackErr) {
		t.Fatalf("expected the fallback's own error, got %v", err)
	}
}

// TestNewFallbackExtractorPanicsOnNil proves both extractors are
// required -- constructing this wrapper with either arm missing would
// silently hide a nil-pointer bug behind a confusing later panic
// instead of failing loudly and immediately at construction.
func TestNewFallbackExtractorPanicsOnNil(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected NewFallbackExtractor(nil, fallback) to panic")
		}
	}()
	NewFallbackExtractor(nil, &stubExtractor{})
}
