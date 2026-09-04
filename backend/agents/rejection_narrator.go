package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// narratorRaceWindow bounds how long Narrate waits on the LLM rephrase
// before giving up and returning the deterministic sentence unchanged --
// same discipline as RacingExtractor.raceWindow (racing_extractor.go),
// deliberately much shorter than llmRequestTimeout for the identical
// reason: a buyer/operator reading a rejection explanation should never
// be made to wait meaningfully longer for a nicer sentence than they'd
// have waited for the correct one they were always going to get anyway.
const narratorRaceWindow = 3500 * time.Millisecond

// RejectionNarrator is idea #4 from files/agent-ai-integration-ideas.md
// (LLM-Narrated Rejection Explanations): an optional rephrasing pass
// over policy.ExplainRejection's already-computed, already-correct
// sentence, applied at the explain_decision MCP tool
// (backend/mcp/tools.go). It never sees the ProposedAction, the
// Mandate, or the failed-check name -- only the finished sentence
// crosses the boundary, and its only allowed output is prose derived
// from exactly that string. This is the same one-way trust boundary
// files/trust-boundary.md already draws around the Policy Engine:
// nothing generative gets a vote in what was decided, only in how the
// already-decided outcome is phrased. A REFUND/CREATE_ORDER decision,
// an amount, a merchant name -- none of it is ever regenerated or
// second-guessed here.
//
// Same fallback discipline as RacingExtractor and the rest of this
// package: on any LLM error, non-200 response, empty completion, a nil
// receiver, or the race window elapsing first, Narrate returns the
// original deterministic sentence completely unchanged. This can only
// ever make explain_decision more readable -- it is never allowed to
// make it less reliable than the deterministic template that ships
// today without an OPENROUTER_API_KEY at all.
type RejectionNarrator struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client

	// costGuard is optional (nil until WithCostGuard is called) --
	// same nil-is-a-valid-state convention as LLMExtractor.costGuard
	// and ToolCallingAgent.costGuard. See CostGuard's own doc comment
	// for why one shared instance guards every OpenRouter call site in
	// this package, this one now included -- explain_decision is
	// unauthenticated (see backend/safety's own doc comments on the
	// publicly-surfaced trust endpoints), so it especially must not be
	// a way to run up LLM spend outside the shared daily budget.
	costGuard *CostGuard
}

// NewRejectionNarrator builds a narrator. baseURL defaults to
// OpenRouter, same convention as NewLLMExtractor/NewToolCallingAgent.
func NewRejectionNarrator(apiKey, baseURL, model string) *RejectionNarrator {
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}
	if model == "" {
		model = "openai/gpt-4o-mini"
	}
	return &RejectionNarrator{
		apiKey:  apiKey,
		baseURL: strings.TrimSuffix(baseURL, "/"),
		model:   model,
		client:  &http.Client{Timeout: llmRequestTimeout},
	}
}

// NewRejectionNarratorFromEnv reads the same OPENROUTER_API_KEY/
// LLM_BASE_URL/LLM_MODEL env vars LLMExtractor and ToolCallingAgent
// do. Returns nil without an API key -- a nil *RejectionNarrator is a
// documented, supported no-op (see Narrate below), the same
// nil-receiver convention NewToolCallingAgentFromEnv already
// establishes for this codebase's other optional LLM-backed types.
func NewRejectionNarratorFromEnv() *RejectionNarrator {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		return nil
	}
	return NewRejectionNarrator(apiKey, os.Getenv("LLM_BASE_URL"), os.Getenv("LLM_MODEL"))
}

// WithCostGuard wires the shared daily cost guard, same fluent-setter,
// nil-receiver-safe convention as LLMExtractor.WithCostGuard.
func (n *RejectionNarrator) WithCostGuard(g *CostGuard) *RejectionNarrator {
	if n == nil {
		return nil
	}
	n.costGuard = g
	return n
}

// narratorSystemPrompt constrains the rewrite to phrasing only --
// "preserve every fact/number exactly" is the load-bearing instruction
// here, not politeness. Narrate does not validate that the model
// actually obeyed it (there is no cheap, reliable way to verify a
// rephrase preserved every number without essentially re-extracting
// and diffing them, which is more machinery than a buildathon-scoped
// narration layer over an already-correct answer needs) -- what makes
// this safe regardless is that the ORIGINAL deterministic sentence is
// always what's ultimately in the audit trail and in
// policy.ExplainRejection's own return value; this rephrase is a
// presentation-layer addition on top of it, seen only through
// explain_decision, never a replacement for it anywhere else in the
// system.
const narratorSystemPrompt = `You rephrase a single sentence that explains why an e-commerce purchase was declined by an automated policy engine. Rewrite it to sound natural and helpful, in one or two short sentences. You MUST preserve every fact, number, and amount in the original exactly -- do not change, round, add, or omit any rupee amount, product name, merchant name, or reason. Do not apologize excessively, invent a cause, or add any information not present in the original. Respond with ONLY the rewritten sentence: no preamble, no quotation marks.`

// Narrate rephrases deterministic (the output of policy.ExplainRejection)
// into more natural language, or returns it completely unchanged on a
// nil receiver, an empty input, an LLM failure, or the race window
// elapsing first. deterministic is never mutated -- every return path
// either returns it verbatim or a string derived only from it.
func (n *RejectionNarrator) Narrate(ctx context.Context, deterministic string) string {
	if n == nil || strings.TrimSpace(deterministic) == "" {
		return deterministic
	}

	resultCh := make(chan string, 1)
	go func() {
		rephrased, err := n.rephrase(ctx, deterministic)
		if err != nil {
			resultCh <- ""
			return
		}
		resultCh <- rephrased
	}()

	select {
	case rephrased := <-resultCh:
		if rephrased == "" {
			return deterministic
		}
		return rephrased
	case <-time.After(narratorRaceWindow):
		return deterministic
	}
}

// rephrase makes one Chat Completions round trip, reusing the same
// chatMessage/chatRequest/chatResponse wire types llm_extractor.go's
// LLMExtractor.Extract already speaks -- chatRequest's ResponseFormat
// field is left unset (it's `omitempty`), since this call wants free
// text, not the strict JSON intent extraction needs.
func (n *RejectionNarrator) rephrase(ctx context.Context, deterministic string) (string, error) {
	if err := n.costGuard.Check(ctx); err != nil {
		return "", err
	}

	body, err := json.Marshal(chatRequest{
		Model: n.model,
		Messages: []chatMessage{
			{Role: "system", Content: narratorSystemPrompt},
			{Role: "user", Content: deterministic},
		},
		Temperature: 0.3,
	})
	if err != nil {
		return "", fmt.Errorf("marshal narrator request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build narrator request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+n.apiKey)

	res, err := n.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("narrator request failed: %w", err)
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("read narrator response: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("narrator api error %d: %s", res.StatusCode, truncate(string(raw), 400))
	}

	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("decode narrator response: %w", err)
	}
	// Recorded regardless of what's decoded below -- same reasoning as
	// LLMExtractor.Extract's identical comment on its own Record call.
	n.costGuard.Record(parsed.Usage.PromptTokens, parsed.Usage.CompletionTokens)
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("narrator returned no choices")
	}
	rephrased := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if rephrased == "" {
		return "", fmt.Errorf("narrator returned empty content")
	}
	return rephrased, nil
}
