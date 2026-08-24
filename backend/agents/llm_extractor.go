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
}

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
		client:  &http.Client{Timeout: 30 * time.Second},
	}
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
const intentSystemPrompt = `You extract purchase intent from a buyer's natural-language request.
Respond with ONLY a JSON object matching exactly this schema:
{"budget": number, "category": string, "priority": string, "recipient": string}
- budget: the buyer's max spend in INR (a positive whole number of rupees).
- category: one of "earbuds", "laptop", "accessories", or "" if unknown.
- priority: a feature priority like "active_noise_cancellation", "battery_life", or "".
- recipient: "sister", "brother", or "".
If the request is too vague to extract a budget AND a category, respond with
{"clarify": "What would you like to buy, and what is your budget?"}
Never guess an amount. Never invent a category. Output nothing but JSON.`

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
}

// Extract calls the LLM and validates the structured output.
func (e *LLMExtractor) Extract(ctx context.Context, prompt string) (Intent, error) {
	if strings.TrimSpace(prompt) == "" {
		return Intent{}, ErrAmbiguousIntent
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
		return Intent{}, ErrAmbiguousIntent
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
