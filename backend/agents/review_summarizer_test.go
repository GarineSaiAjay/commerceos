package agents

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestReviewSummarizerNilReceiverReturnsError proves a nil
// *ReviewSummarizer (no OPENROUTER_API_KEY configured) never panics
// and returns the documented sentinel error rather than an empty
// string or a generic one -- the caller needs to distinguish "not
// configured" from "not enough reviews yet" to render the right
// "no summary available" state.
func TestReviewSummarizerNilReceiverReturnsError(t *testing.T) {
	var s *ReviewSummarizer
	_, err := s.Summarize(context.Background(), "airpods-pro-2", "AirPods Pro", []string{"Great sound", "Battery lasts all day", "Comfortable fit"})
	if !errors.Is(err, ErrReviewSummarizerUnavailable) {
		t.Errorf("Summarize on nil receiver err = %v, want ErrReviewSummarizerUnavailable", err)
	}
}

// TestReviewSummarizerNotEnoughReviews proves fewer than
// minReviewsForSummary non-empty comments returns ErrNotEnoughReviews
// WITHOUT ever making an LLM call -- the fake server here fails the
// test if it receives any request at all.
func TestReviewSummarizerNotEnoughReviews(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("Summarize should not call the LLM below minReviewsForSummary")
	}))
	defer srv.Close()

	s := NewReviewSummarizer("test-key", srv.URL, "test-model")
	// Two non-empty comments plus one blank (rating-only) one --
	// blanks must not count toward the minimum.
	_, err := s.Summarize(context.Background(), "airpods-pro-2", "AirPods Pro", []string{"Great sound", "  ", "Battery lasts all day"})
	if !errors.Is(err, ErrNotEnoughReviews) {
		t.Errorf("Summarize with 2 real comments err = %v, want ErrNotEnoughReviews", err)
	}
}

// TestReviewSummarizerSummarizesOnSuccess proves a real (fake-server)
// LLM response is returned as the summary.
func TestReviewSummarizerSummarizesOnSuccess(t *testing.T) {
	const summary = "Buyers consistently praise the sound quality and all-day battery life."
	srv := serveChat(summary)
	defer srv.Close()

	s := NewReviewSummarizer("test-key", srv.URL, "test-model")
	got, err := s.Summarize(context.Background(), "airpods-pro-2", "AirPods Pro", []string{"Great sound", "Battery lasts all day", "Very comfortable"})
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	if got != summary {
		t.Errorf("Summarize() = %q, want %q", got, summary)
	}
}

// TestReviewSummarizerCachesByReviewCount proves a second Summarize
// call with the SAME review count reuses the cached result instead of
// making a second LLM call -- the fake server here fails the test if
// it receives more than one request.
func TestReviewSummarizerCachesByReviewCount(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls > 1 {
			t.Fatal("Summarize should have served the second call from cache")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Buyers like the battery life."}}]}`))
	}))
	defer srv.Close()

	s := NewReviewSummarizer("test-key", srv.URL, "test-model")
	comments := []string{"Great sound", "Battery lasts all day", "Very comfortable"}

	first, err := s.Summarize(context.Background(), "airpods-pro-2", "AirPods Pro", comments)
	if err != nil {
		t.Fatalf("first Summarize() error = %v", err)
	}
	second, err := s.Summarize(context.Background(), "airpods-pro-2", "AirPods Pro", comments)
	if err != nil {
		t.Fatalf("second Summarize() error = %v", err)
	}
	if first != second {
		t.Errorf("cached Summarize() = %q, want the same as the first call %q", second, first)
	}
	if calls != 1 {
		t.Errorf("LLM was called %d times, want exactly 1 (second call should be cached)", calls)
	}
}

// TestReviewSummarizerReturnsErrorOnLLMFailure proves an LLM failure
// (a non-200 response) surfaces as a real error rather than a panic or
// a silently-empty string -- unlike RejectionNarrator, there is no
// deterministic text to fall back to here, so the caller must be able
// to tell "failed" apart from "succeeded with an empty answer."
func TestReviewSummarizerReturnsErrorOnLLMFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := NewReviewSummarizer("test-key", srv.URL, "test-model")
	_, err := s.Summarize(context.Background(), "airpods-pro-2", "AirPods Pro", []string{"Great sound", "Battery lasts all day", "Very comfortable"})
	if err == nil {
		t.Error("Summarize() error = nil, want a non-nil error on LLM failure")
	}
}
