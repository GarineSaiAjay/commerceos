package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// serveChat returns an httptest server that responds with the provided
// chat content so the extractor can be tested without a real API call.
func serveChat(content string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Authorization header must be sent.
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": content}},
			},
		})
	}))
}

// TestLLMExtractorValidIntent proves a real LLM-shaped JSON response is
// parsed and validated into an Intent.
func TestLLMExtractorValidIntent(t *testing.T) {
	srv := serveChat(`{"budget": 25000, "category": "earbuds", "priority": "active_noise_cancellation", "recipient": "sister"}`)
	defer srv.Close()

	ext := NewLLMExtractor("test-key", srv.URL, "test-model")
	intent, err := ext.Extract(context.Background(), "I need wireless earbuds for my sister. Budget ₹25,000.")
	if err != nil {
		t.Fatal(err)
	}
	if intent.Budget != 25000 || intent.Category != "earbuds" || intent.Priority != "active_noise_cancellation" || intent.Recipient != "sister" {
		t.Fatalf("unexpected intent: %+v", intent)
	}
}

// TestLLMExtractorFencedJSON proves ```json fences are stripped.
func TestLLMExtractorFencedJSON(t *testing.T) {
	srv := serveChat("```json\n{\"budget\": 5000, \"category\": \"laptop\", \"priority\": \"battery_life\", \"recipient\": \"brother\"}\n```")
	defer srv.Close()

	ext := NewLLMExtractor("test-key", srv.URL, "test-model")
	intent, err := ext.Extract(context.Background(), "buy my brother a laptop under 5000 with good battery")
	if err != nil {
		t.Fatal(err)
	}
	if intent.Budget != 5000 || intent.Category != "laptop" || intent.Priority != "battery_life" {
		t.Fatalf("unexpected intent: %+v", intent)
	}
}

// TestLLMExtractorClarify proves a vague prompt yields ErrAmbiguousIntent.
func TestLLMExtractorClarify(t *testing.T) {
	srv := serveChat(`{"clarify": "What would you like to buy, and what is your budget?"}`)
	defer srv.Close()

	ext := NewLLMExtractor("test-key", srv.URL, "test-model")
	_, err := ext.Extract(context.Background(), "buy me something")
	if err != ErrAmbiguousIntent {
		t.Fatalf("expected ErrAmbiguousIntent, got %v", err)
	}
}

// TestLLMExtractorMalformed proves bad JSON is rejected (never trusted).
func TestLLMExtractorMalformed(t *testing.T) {
	srv := serveChat(`{"budget": "not-a-number"}`)
	defer srv.Close()

	ext := NewLLMExtractor("test-key", srv.URL, "test-model")
	_, err := ext.Extract(context.Background(), "some prompt")
	if err == nil {
		t.Fatal("expected error for malformed intent JSON")
	}
}

// TestLLMExtractorNoKeyReturnsNil proves NewLLMExtractorFromEnv is nil
// without a key (falling back to the deterministic extractor).
func TestLLMExtractorNoKeyReturnsNil(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("LLM_BASE_URL", "")
	t.Setenv("LLM_MODEL", "")
	if got := NewLLMExtractorFromEnv(); got != nil {
		t.Fatal("expected nil extractor when no key is set")
	}
}

// TestLLMExtractorFromEnvWithKey proves the env constructor wires the key.
func TestLLMExtractorFromEnvWithKey(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-test")
	t.Setenv("LLM_BASE_URL", "")
	t.Setenv("LLM_MODEL", "")
	got := NewLLMExtractorFromEnv()
	if got == nil {
		t.Fatal("expected an extractor when a key is set")
	}
	if got.baseURL != "https://openrouter.ai/api/v1" {
		t.Fatalf("expected default openrouter base URL, got %q", got.baseURL)
	}
	if got.model != "openai/gpt-4o-mini" {
		t.Fatalf("expected default model, got %q", got.model)
	}
}
