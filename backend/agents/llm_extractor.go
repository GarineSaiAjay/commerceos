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

// LLMExtractor is a real LLM-backed IntentExtractor using an
// OpenAI-compatible Chat Completions API (works with OpenRouter).
// It requests strict JSON output and validates it through ParseIntentJSON
// so nothing downstream trusts unvalidated free text.
type LLMExtractor struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client

	// costGuard is optional (nil until WithCostGuard is called) --
	// see CostGuard's own doc comment for why one shared instance
	// guards this AND ToolCallingAgent.chat.
	costGuard *CostGuard
}

// llmRequestTimeout bounds how long Extract waits on the whole chat
// completion round trip (connect + full response body). 60s rather than
// a shorter ceiling because OpenRouter response time varies with model
// load and the caller's own network -- a real (if intermittent) failure
// mode is "the model was just slow, not actually down", and 30s left too
// little headroom for that on a slower connection. Safe to keep this
// generous now that main.go wraps this extractor in a FallbackExtractor
// (fallback_extractor.go): a timeout here just means a slower recovery
// to the deterministic extractor, not a failed request, so there's no
// longer a reason to trade away a legitimately-slow-but-good LLM answer
// for a snappier failure -- see fallback_extractor.go's own doc comment.
const llmRequestTimeout = 60 * time.Second

// NewLLMExtractor builds an extractor. baseURL defaults to OpenRouter.
// Use NewLLMExtractorFromEnv to read OPENROUTER_API_KEY/LLM_BASE_URL/LLM_MODEL.
func NewLLMExtractor(apiKey, baseURL, model string) *LLMExtractor {
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}
	if model == "" {
		model = "openai/gpt-4o-mini"
	}
	return &LLMExtractor{
		apiKey:  apiKey,
		baseURL: strings.TrimSuffix(baseURL, "/"),
		model:   model,
		client:  &http.Client{Timeout: llmRequestTimeout},
	}
}

// WithCostGuard wires the shared daily cost guard (PLAN-01-AGENTIC-
// CORE.md §2's "max-cost/day guard"), same fluent-setter, nil-
// receiver-safe convention as ToolCallingAgent.WithConversationStore.
// Leaving this unset is fully supported: Extract works identically,
// just without the daily budget enforced.
func (e *LLMExtractor) WithCostGuard(g *CostGuard) *LLMExtractor {
	if e == nil {
		return nil
	}
	e.costGuard = g
	return e
}

// NewLLMExtractorFromEnv reads the standard env vars. Returns nil if no
// API key is configured so the caller can fall back to the deterministic
// extractor.
func NewLLMExtractorFromEnv() *LLMExtractor {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		return nil
	}
	return NewLLMExtractor(
		apiKey,
		os.Getenv("LLM_BASE_URL"),
		os.Getenv("LLM_MODEL"),
	)
}

// The system prompt enforces the strict output schema. The model may only
// emit JSON matching Intent; ambiguous input must set "clarify".
//
// The catalog this shops against (db/seeds/001_catalog.sql) is Apple/Beats
// audio accessories plus AirTag trackers -- the category list and examples
// below must be kept in sync with it by hand (same staleness risk already
// hit three times elsewhere in this codebase for hardcoded product/category
// lists: growth/simulator.go, policy/model.go, campaign/model.go). Without
// "charging"/"tracking" listed here, and without the product-name examples,
// a real request like "beats fit pro for my sister, budget below 30k" was
// coming back with category "" -- the model had nowhere valid to put a
// product it could clearly identify as earbuds, so it left the field
// blank rather than guess, which then failed validation.
const intentSystemPrompt = `You extract purchase intent from a buyer's natural-language request.
Respond with ONLY a JSON object matching exactly this schema:
{"budget": number, "category": string, "priority": string, "recipient": string}
- budget: the buyer's max spend in INR (a positive whole number of rupees).
  Shorthand like "30k" or "under 30k" means 30000; a bare "30" without any
  "k"/"thousand"/lakh wording means 30 rupees, not 30000 -- convert
  shorthand yourself, don't leave it for someone else to interpret.
- category: one of "earbuds", "laptop", "charging", "accessories",
  "tracking", or "" if unknown. Buyers name specific products, not
  categories -- map the product to its category yourself:
  "AirPods" (any generation), "AirPods Pro", "AirPods Max", or "Beats Fit
  Pro" -> "earbuds"; "AirTag" -> "tracking"; a charger, charging pad,
  MagSafe charger, or Lightning/USB-C cable -> "charging"; a case,
  AppleCare, an adapter, or ear tips -> "accessories".
- priority: a feature priority like "active_noise_cancellation", "battery_life", or "".
- recipient: "sister", "brother", or "". Casual shorthand still counts --
  "bro"/"brotha" -> "brother", "sis" -> "sister". Anyone else (mom, dad,
  friend, self, ...) -> "".
A request can express its budget without ever using the word "budget"
("under 40k", "below 5000", "max 2000 for my bro") -- extract it anyway;
don't treat the absence of that literal word as reason to ask for
clarification.
If the request is too vague to extract a budget AND a category, respond with
{"clarify": "What would you like to buy, and what is your budget?"}
Never guess an amount. Never invent a category outside the list above --
but DO map a named real product to the category it actually belongs to
rather than leaving category empty.`

// intentUserPrompt builds the per-request message.
func intentUserPrompt(prompt string) string {
	return fmt.Sprintf("Buyer request: %q", prompt)
}

// chatMessage is one Chat Completions message.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest is the body we send to the API.
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	// response_format encourages strict JSON on OpenAI-compatible hosts.
	ResponseFormat map[string]any `json:"response_format,omitempty"`
	Temperature    float64        `json:"temperature,omitempty"`
}

// chatChoice is one completion choice.
type chatChoice struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

// chatResponse is the API response envelope.
type chatResponse struct {
	Choices []chatChoice `json:"choices"`
	Usage   llmUsage     `json:"usage"`
}

// Extract calls the LLM and validates the structured output.
func (e *LLMExtractor) Extract(ctx context.Context, prompt string) (Intent, error) {
	if strings.TrimSpace(prompt) == "" {
		return Intent{}, ErrAmbiguousIntent
	}
	if err := e.costGuard.Check(ctx); err != nil {
		return Intent{}, err
	}

	body, err := json.Marshal(chatRequest{
		Model: e.model,
		Messages: []chatMessage{
			{Role: "system", Content: intentSystemPrompt},
			{Role: "user", Content: intentUserPrompt(prompt)},
		},
		ResponseFormat: map[string]any{"type": "json_object"},
		Temperature:    0,
	})
	if err != nil {
		return Intent{}, fmt.Errorf("marshal chat request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Intent{}, fmt.Errorf("build chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	res, err := e.client.Do(req)
	if err != nil {
		return Intent{}, fmt.Errorf("llm request failed: %w", err)
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return Intent{}, fmt.Errorf("read llm response: %w", err)
	}

	if res.StatusCode != http.StatusOK {
		return Intent{}, fmt.Errorf("llm api error %d: %s", res.StatusCode, truncate(string(raw), 400))
	}

	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Intent{}, fmt.Errorf("decode llm response: %w", err)
	}
	// Recorded regardless of what's decoded below -- OpenRouter bills
	// for tokens actually returned, not for whether this extractor goes
	// on to make sense of them.
	e.costGuard.Record(parsed.Usage.PromptTokens, parsed.Usage.CompletionTokens)
	if len(parsed.Choices) == 0 {
		return Intent{}, fmt.Errorf("llm returned no choices")
	}
	content := strings.TrimSpace(parsed.Choices[0].Message.Content)

	// Strip markdown fences if the host wraps JSON in ```json ... ```.
	content = stripFences(content)

	intent, err := ParseIntentJSON([]byte(content))
	if err != nil {
		return Intent{}, err
	}
	if intent.Clarify != "" {
		// The parsed intent (including any partial fields the model DID
		// supply alongside its clarify request, per ParseIntentJSON's
		// doc comment) is returned alongside the sentinel error, not
		// discarded -- a caller that only checks err != nil (the
		// original, still-supported behavior) is unaffected; one that
		// also wants those partial fields when err is exactly
		// ErrAmbiguousIntent (BuyerAgent.PlanCheckoutInConversation's
		// conversation-memory merge, PLAN-01-AGENTIC-CORE.md §3) can
		// use them instead of starting from nothing.
		return intent, ErrAmbiguousIntent
	}
	return intent, nil
}

// stripFences removes ```json ... ``` wrappers some providers add.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
