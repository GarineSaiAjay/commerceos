package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ErrNotEnoughReviews is returned when a product doesn't yet have
// enough real review comments to responsibly synthesize into a "buyers
// say ..." summary -- below minReviewsForSummary, a one-or-two-line
// blurb would just be restating a single opinion as though it were a
// consensus, which is worse than showing nothing at all.
var ErrNotEnoughReviews = errors.New("agents: not enough review comments to summarize")

// ErrReviewSummarizerUnavailable is returned by a nil *ReviewSummarizer
// (no OPENROUTER_API_KEY configured) -- unlike RejectionNarrator, there
// is no deterministic sentence to fall back to here, so Summarize
// returns a real error instead of silently substituting something. The
// caller (this package's HTTP-facing wiring in cmd/server/main.go) is
// expected to translate both this and ErrNotEnoughReviews into the same
// "no summary available right now" shape growth.GrowthAgent's
// SuggestResponse.available already establishes for "the AI feature had
// nothing to offer this time" -- never a request failure.
var ErrReviewSummarizerUnavailable = errors.New("agents: review summarizer not configured")

// minReviewsForSummary is the fewest non-empty review comments
// Summarize will synthesize from.
const minReviewsForSummary = 3

// maxReviewCommentsInPrompt caps how many individual comments go into
// one summarization call -- bounds both prompt size/cost and how much
// a single pathologically-long review thread can spend from the shared
// CostGuard budget on one product.
const maxReviewCommentsInPrompt = 20

// reviewSummaryCacheTTL is a safety-valve expiry on top of this type's
// real invalidation mechanism (see the cache key built in Summarize
// below) -- not the primary cache-freshness lever the way ListProducts'
// ProductsCache TTL is. A cached summary here is keyed by the exact
// count of non-empty comments it was generated from, so a NEW review
// changes the key and is a guaranteed cache miss with zero explicit
// invalidation plumbing -- this TTL only bounds how long a dead entry
// (a product whose review count will never change again in this demo
// session) lingers in memory, so it can be generous.
const reviewSummaryCacheTTL = 30 * time.Minute

// ReviewSummarizer is idea #5 from files/agent-ai-integration-ideas.md
// (Review Summarization Agent): a short "buyers say ..." synthesis of
// a product's real review comments. Bounded and low-risk by
// construction -- Summarize is only ever given review text that's
// already public on the product page, and its system prompt forbids
// inventing any claim the reviews don't support; it can leave a claim
// out, it can never add one.
type ReviewSummarizer struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client

	// costGuard is optional (nil until WithCostGuard is called), same
	// nil-is-a-valid-state convention as every other OpenRouter call
	// site in this package -- see CostGuard's own doc comment for why
	// one shared instance guards all of them.
	costGuard *CostGuard

	mu    sync.Mutex
	cache map[string]reviewSummaryCacheEntry
}

type reviewSummaryCacheEntry struct {
	summary string
	expires time.Time
}

// NewReviewSummarizer builds a summarizer. baseURL defaults to
// OpenRouter, same convention as NewLLMExtractor/NewRejectionNarrator.
func NewReviewSummarizer(apiKey, baseURL, model string) *ReviewSummarizer {
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}
	if model == "" {
		model = "openai/gpt-4o-mini"
	}
	return &ReviewSummarizer{
		apiKey:  apiKey,
		baseURL: strings.TrimSuffix(baseURL, "/"),
		model:   model,
		client:  &http.Client{Timeout: llmRequestTimeout},
		cache:   make(map[string]reviewSummaryCacheEntry),
	}
}

// NewReviewSummarizerFromEnv reads the same OPENROUTER_API_KEY/
// LLM_BASE_URL/LLM_MODEL env vars every other LLM-backed type in this
// package does. Returns nil without an API key -- a nil
// *ReviewSummarizer is a documented, supported state (Summarize
// returns ErrReviewSummarizerUnavailable on one), same convention as
// NewToolCallingAgentFromEnv/NewRejectionNarratorFromEnv.
func NewReviewSummarizerFromEnv() *ReviewSummarizer {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		return nil
	}
	return NewReviewSummarizer(apiKey, os.Getenv("LLM_BASE_URL"), os.Getenv("LLM_MODEL"))
}

// WithCostGuard wires the shared daily cost guard, same fluent-setter,
// nil-receiver-safe convention as LLMExtractor.WithCostGuard.
func (s *ReviewSummarizer) WithCostGuard(g *CostGuard) *ReviewSummarizer {
	if s == nil {
		return nil
	}
	s.costGuard = g
	return s
}

// reviewSummarySystemPrompt's "MUST NOT invent" instruction is the
// load-bearing one -- same "bounded, low-risk" posture the proposal
// doc calls for: this only ever synthesizes real review text that's
// already public on the product page, never a claim the reviews don't
// support.
const reviewSummarySystemPrompt = `You write a short "buyers say ..." summary of real customer reviews for an e-commerce product. Read the numbered reviews given and write ONE OR TWO short sentences synthesizing what buyers actually said, in a neutral, factual tone. You MUST NOT invent, assume, or add any claim, fact, or opinion that isn't actually expressed in the reviews given. If the reviews disagree with each other, say so briefly instead of picking a side. Do not mention the number of reviews or that you are an AI. Respond with ONLY the summary sentence(s): no preamble, no quotation marks.`

// Summarize returns a short synthesis of comments for productTitle, or
// ErrNotEnoughReviews / ErrReviewSummarizerUnavailable / an LLM error.
// Empty/whitespace-only comments are dropped before counting or
// prompting -- a rating-only review with no comment text contributes
// nothing to synthesize. Cached per (productID, count of non-empty
// comments) -- see reviewSummaryCacheTTL's doc comment for why that
// key is itself the invalidation mechanism.
func (s *ReviewSummarizer) Summarize(ctx context.Context, productID, productTitle string, comments []string) (string, error) {
	if s == nil {
		return "", ErrReviewSummarizerUnavailable
	}

	nonEmpty := make([]string, 0, len(comments))
	for _, c := range comments {
		c = strings.TrimSpace(c)
		if c != "" {
			nonEmpty = append(nonEmpty, c)
		}
	}
	if len(nonEmpty) < minReviewsForSummary {
		return "", ErrNotEnoughReviews
	}

	cacheKey := fmt.Sprintf("%s:%d", productID, len(nonEmpty))
	if cached, ok := s.cacheGet(cacheKey); ok {
		return cached, nil
	}

	summary, err := s.summarize(ctx, productTitle, nonEmpty)
	if err != nil {
		return "", err
	}

	s.cacheSet(cacheKey, summary)
	return summary, nil
}

func (s *ReviewSummarizer) cacheGet(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.cache[key]
	if !ok || time.Now().After(entry.expires) {
		return "", false
	}
	return entry.summary, true
}

func (s *ReviewSummarizer) cacheSet(key, summary string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[key] = reviewSummaryCacheEntry{summary: summary, expires: time.Now().Add(reviewSummaryCacheTTL)}
}

// summarize makes one Chat Completions round trip, reusing
// llm_extractor.go's chatMessage/chatRequest/chatResponse wire types
// exactly like RejectionNarrator.rephrase does.
func (s *ReviewSummarizer) summarize(ctx context.Context, productTitle string, comments []string) (string, error) {
	if err := s.costGuard.Check(ctx); err != nil {
		return "", err
	}

	capped := comments
	if len(capped) > maxReviewCommentsInPrompt {
		capped = capped[:maxReviewCommentsInPrompt]
	}
	var listing strings.Builder
	for i, c := range capped {
		fmt.Fprintf(&listing, "%d. %s\n", i+1, truncate(c, 300))
	}

	userPrompt := fmt.Sprintf("Product: %s\n\nReviews:\n%s", productTitle, listing.String())

	body, err := json.Marshal(chatRequest{
		Model: s.model,
		Messages: []chatMessage{
			{Role: "system", Content: reviewSummarySystemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: 0.3,
	})
	if err != nil {
		return "", fmt.Errorf("marshal summarizer request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build summarizer request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	res, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("summarizer request failed: %w", err)
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("read summarizer response: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("summarizer api error %d: %s", res.StatusCode, truncate(string(raw), 400))
	}

	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("decode summarizer response: %w", err)
	}
	// Recorded regardless of what's decoded below -- same reasoning as
	// LLMExtractor.Extract's identical comment on its own Record call.
	s.costGuard.Record(parsed.Usage.PromptTokens, parsed.Usage.CompletionTokens)
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("summarizer returned no choices")
	}
	summary := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if summary == "" {
		return "", fmt.Errorf("summarizer returned empty content")
	}
	return summary, nil
}
