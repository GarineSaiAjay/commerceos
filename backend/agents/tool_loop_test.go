package agents

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/garinesaiajay/commerceos/commerce/catalog"
	"github.com/garinesaiajay/commerceos/tools"
)

// loopFakeCatalogRepo is a minimal catalog.Repository for tool_loop.go
// tests. Unlike agents_test.go's fakeCatalog (which only implements
// tools.CatalogReader's ListProducts, enough for Searcher),
// ToolCallingAgent.finalizeProposal/dispatch call through a real
// *catalog.Service -- tools.Dependencies.Catalog's concrete type, not
// an interface -- so a full Repository fake is needed here.
type loopFakeCatalogRepo struct{}

func (loopFakeCatalogRepo) CreateProduct(ctx context.Context, p catalog.Product) error { return nil }

func (loopFakeCatalogRepo) GetProduct(ctx context.Context, id string) (catalog.Product, error) {
	for _, p := range loopFakeCatalogProducts() {
		if p.ID == id {
			return p, nil
		}
	}
	return catalog.Product{}, catalog.ErrProductNotFound
}

func (loopFakeCatalogRepo) ListProducts(ctx context.Context) ([]catalog.Product, error) {
	return loopFakeCatalogProducts(), nil
}

func (loopFakeCatalogRepo) GetVariant(ctx context.Context, id string) (catalog.ProductVariant, error) {
	return catalog.ProductVariant{ID: id, ProductID: "airpods-pro-2", Price: catalog.Money{Amount: 2_490_000, Currency: "INR"}, Availability: 5}, nil
}

func (loopFakeCatalogRepo) ListVariantsByProduct(ctx context.Context, productID string) ([]catalog.ProductVariant, error) {
	return nil, nil
}

func (loopFakeCatalogRepo) UpdateProduct(ctx context.Context, p catalog.Product) error { return nil }

func (loopFakeCatalogRepo) DeleteProduct(ctx context.Context, id string) error { return nil }

func loopFakeCatalogProducts() []catalog.Product {
	return []catalog.Product{
		{
			ID:       "airpods-pro-2",
			Title:    "AirPods Pro",
			Price:    catalog.Money{Amount: 2_490_000, Currency: "INR"},
			Features: []string{"active_noise_cancellation"},
			UseCases: []string{"earbuds"},
		},
		{
			ID:       "airpods-case",
			Title:    "AirPods Case",
			Price:    catalog.Money{Amount: 199_900, Currency: "INR"},
			Features: []string{"protective"},
			UseCases: []string{"accessories"},
		},
	}
}

func loopTestDeps() tools.Dependencies {
	return tools.Dependencies{Catalog: catalog.NewService(loopFakeCatalogRepo{})}
}

// serveLoopChat returns an httptest server that plays back one raw
// chat-message JSON body per call, in order -- so a multi-turn
// tool-calling loop (search, then propose, then...) can be tested
// without a real LLM. Distinct from llm_extractor_test.go's serveChat,
// which only ever returns a fixed single-shot content string and has
// no notion of a call sequence or tool_calls.
func serveLoopChat(t *testing.T, messages ...string) *httptest.Server {
	t.Helper()
	calls := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if calls >= len(messages) {
			t.Fatalf("unexpected extra chat call %d (only %d responses configured)", calls+1, len(messages))
		}
		msg := messages[calls]
		calls++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":%s}]}`, msg)
	}))
}

// TestToolCallingAgentImmediateProposal proves the loop's terminal
// path: a propose_checkout tool call on the very first turn resolves
// into a real CheckoutPlan whose price/currency come from the catalog,
// never from the model's own arguments (propose_checkout's schema
// doesn't even carry a price).
func TestToolCallingAgentImmediateProposal(t *testing.T) {
	srv := serveLoopChat(t, `{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"propose_checkout","arguments":"{\"product_id\":\"airpods-pro-2\",\"reasoning\":\"Best ANC match under budget.\"}"}}]}`)
	defer srv.Close()

	agent := NewToolCallingAgent("test-key", srv.URL, "test-model", loopTestDeps())
	result, err := agent.Run(context.Background(), "wireless earbuds under 25000 for my sister", "merchant_001")
	if err != nil {
		t.Fatal(err)
	}
	if result.Plan == nil {
		t.Fatalf("expected a plan, got clarify=%q steps=%+v", result.Clarify, result.Steps)
	}
	if result.Plan.SelectedID != "airpods-pro-2" {
		t.Fatalf("expected airpods-pro-2, got %s", result.Plan.SelectedID)
	}
	if result.Plan.Proposal.Amount != 2_490_000 || result.Plan.Proposal.Currency != "INR" {
		t.Fatalf("expected catalog-authoritative price 2490000 INR, got %d %s", result.Plan.Proposal.Amount, result.Plan.Proposal.Currency)
	}
	if result.Plan.Proposal.Action != "CREATE_ORDER" || result.Plan.Proposal.Merchant != "merchant_001" {
		t.Fatalf("unexpected proposal: %+v", result.Plan.Proposal)
	}
}

// TestToolCallingAgentSearchThenPropose proves a real multi-step turn:
// the model calls search_products, gets real dispatch results back,
// then proposes from among them on its second turn.
func TestToolCallingAgentSearchThenPropose(t *testing.T) {
	srv := serveLoopChat(t,
		`{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"search_products","arguments":"{\"budget\":25000,\"category\":\"earbuds\"}"}}]}`,
		`{"role":"assistant","content":null,"tool_calls":[{"id":"call_2","type":"function","function":{"name":"propose_checkout","arguments":"{\"product_id\":\"airpods-pro-2\",\"reasoning\":\"Top search result.\"}"}}]}`,
	)
	defer srv.Close()

	agent := NewToolCallingAgent("test-key", srv.URL, "test-model", loopTestDeps())
	result, err := agent.Run(context.Background(), "wireless earbuds under 25000", "merchant_001")
	if err != nil {
		t.Fatal(err)
	}
	if result.Plan == nil || result.Plan.SelectedID != "airpods-pro-2" {
		t.Fatalf("expected airpods-pro-2 plan, got %+v (clarify=%q)", result.Plan, result.Clarify)
	}

	var sawSearch, sawResult, sawProposed bool
	for _, s := range result.Steps {
		switch {
		case s.Type == "tool_called" && strings.HasPrefix(s.Detail, "search_products"):
			sawSearch = true
		case s.Type == "tool_result":
			sawResult = true
		case s.Type == "proposed":
			sawProposed = true
		}
	}
	if !sawSearch || !sawResult || !sawProposed {
		t.Fatalf("expected search -> result -> proposed trace, got %+v", result.Steps)
	}
}

// TestToolCallingAgentUnknownProductRetries proves a hallucinated
// product_id doesn't fail the whole request: finalizeProposal rejects
// it, the loop reports that back as a tool result, and the model gets
// a second turn to retry with a real ID.
func TestToolCallingAgentUnknownProductRetries(t *testing.T) {
	srv := serveLoopChat(t,
		`{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"propose_checkout","arguments":"{\"product_id\":\"not-a-real-product\",\"reasoning\":\"guess\"}"}}]}`,
		`{"role":"assistant","content":null,"tool_calls":[{"id":"call_2","type":"function","function":{"name":"propose_checkout","arguments":"{\"product_id\":\"airpods-case\",\"reasoning\":\"real product this time\"}"}}]}`,
	)
	defer srv.Close()

	agent := NewToolCallingAgent("test-key", srv.URL, "test-model", loopTestDeps())
	result, err := agent.Run(context.Background(), "an airpods case", "merchant_001")
	if err != nil {
		t.Fatal(err)
	}
	if result.Plan == nil || result.Plan.SelectedID != "airpods-case" {
		t.Fatalf("expected the retried, real product to win, got %+v (clarify=%q)", result.Plan, result.Clarify)
	}
}

// TestToolCallingAgentClarify proves that when the model responds with
// plain text and no tool call, the loop treats it as a clarifying
// question -- same "never guess" contract as
// BuyerAgent.PlanCheckout's ErrAmbiguousIntent path, just mid-loop
// instead of a dead end.
func TestToolCallingAgentClarify(t *testing.T) {
	srv := serveLoopChat(t, `{"role":"assistant","content":"What is your budget, and what type of product are you looking for?"}`)
	defer srv.Close()

	agent := NewToolCallingAgent("test-key", srv.URL, "test-model", loopTestDeps())
	result, err := agent.Run(context.Background(), "buy me something", "merchant_001")
	if err != nil {
		t.Fatal(err)
	}
	if result.Plan != nil {
		t.Fatalf("expected no plan, got %+v", result.Plan)
	}
	if result.Clarify == "" {
		t.Fatal("expected a clarifying question")
	}
}

// TestToolCallingAgentEmptyPromptIsAmbiguous mirrors
// BuyerAgent.PlanCheckout: an empty prompt never reaches the LLM at
// all, it's rejected immediately as ErrAmbiguousIntent.
func TestToolCallingAgentEmptyPromptIsAmbiguous(t *testing.T) {
	agent := NewToolCallingAgent("test-key", "http://unused.invalid", "test-model", loopTestDeps())
	_, err := agent.Run(context.Background(), "   ", "merchant_001")
	if err != ErrAmbiguousIntent {
		t.Fatalf("expected ErrAmbiguousIntent, got %v", err)
	}
}

// TestToolCallingAgentNilIsSafe proves a nil *ToolCallingAgent (the
// result of NewToolCallingAgentFromEnv without an API key) fails
// clearly instead of panicking -- Handler.PlanCheckoutLoop relies on
// this to turn a nil loopAgent into a clean 503 rather than a crash.
func TestToolCallingAgentNilIsSafe(t *testing.T) {
	var agent *ToolCallingAgent
	_, err := agent.Run(context.Background(), "anything", "merchant_001")
	if err == nil {
		t.Fatal("expected an error from a nil agent")
	}
}

// TestNewToolCallingAgentFromEnvNilWithoutKey mirrors
// TestLLMExtractorNoKeyReturnsNil: no deterministic fallback exists for
// the tool-calling loop, so no key means no agent at all.
func TestNewToolCallingAgentFromEnvNilWithoutKey(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("LLM_BASE_URL", "")
	t.Setenv("LLM_MODEL", "")
	if got := NewToolCallingAgentFromEnv(loopTestDeps()); got != nil {
		t.Fatal("expected nil agent when no key is set")
	}
}

// TestNewToolCallingAgentFromEnvWithKey proves the env constructor
// wires the key and applies the same OpenRouter/gpt-4o-mini defaults
// NewLLMExtractorFromEnv does.
func TestNewToolCallingAgentFromEnvWithKey(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-test")
	t.Setenv("LLM_BASE_URL", "")
	t.Setenv("LLM_MODEL", "")
	got := NewToolCallingAgentFromEnv(loopTestDeps())
	if got == nil {
		t.Fatal("expected a non-nil agent")
	}
	if got.baseURL != "https://openrouter.ai/api/v1" || got.model != "openai/gpt-4o-mini" {
		t.Fatalf("unexpected defaults: baseURL=%s model=%s", got.baseURL, got.model)
	}
}
