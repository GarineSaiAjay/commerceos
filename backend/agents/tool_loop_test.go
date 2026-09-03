package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// CreateVariant, UpdateVariant and DeleteVariant are no-ops here, same
// as the rest of this fake's mutating methods -- no tool_loop.go test
// in this file exercises variant CRUD directly (that's covered by
// catalog/service_test.go's stateful fakeRepository instead).
func (loopFakeCatalogRepo) CreateVariant(ctx context.Context, v catalog.ProductVariant) error {
	return nil
}

func (loopFakeCatalogRepo) UpdateVariant(ctx context.Context, v catalog.ProductVariant) error {
	return nil
}

func (loopFakeCatalogRepo) DeleteVariant(ctx context.Context, id string) error { return nil }

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

// loopFakeConversationStore is a minimal in-memory ConversationStore for
// RunInConversation tests. No fake ConversationStore exists elsewhere in
// this package's tests (BuyerAgent's own PlanCheckoutInConversation has
// none either) -- written fresh here rather than reused, since
// BuyerAgent's Intent-merge tests (if any) would need LastKnownIntent
// behavior this type never exercises.
type loopFakeConversationStore struct {
	turns map[string][]ConversationTurn

	// appendErr, when set, makes every AppendTurn fail -- proves
	// RunInConversation's best-effort posture: a memory-write failure
	// must never fail the buyer-facing request it's layered on top of.
	appendErr error

	// historyErr, when set, makes every History call fail -- proves
	// RunInConversation degrades to plain Run, rather than failing the
	// whole request, when memory itself is unavailable.
	historyErr error
}

func newLoopFakeConversationStore() *loopFakeConversationStore {
	return &loopFakeConversationStore{turns: map[string][]ConversationTurn{}}
}

func (s *loopFakeConversationStore) AppendTurn(ctx context.Context, cartID, role, content string, intent *Intent) error {
	if s.appendErr != nil {
		return s.appendErr
	}
	s.turns[cartID] = append(s.turns[cartID], ConversationTurn{Role: role, Content: content})
	return nil
}

func (s *loopFakeConversationStore) History(ctx context.Context, cartID string) ([]ConversationTurn, error) {
	if s.historyErr != nil {
		return nil, s.historyErr
	}
	return s.turns[cartID], nil
}

func (s *loopFakeConversationStore) LastKnownIntent(ctx context.Context, cartID string) (Intent, bool, error) {
	// Never called by RunInConversation (it replays raw messages, not an
	// Intent snapshot -- see tool_loop.go's RunInConversation doc
	// comment) -- present only to satisfy the ConversationStore
	// interface.
	return Intent{}, false, nil
}

// loopChatCapture records each raw JSON request body a serveLoopChatCapture
// server receives, so a test can assert what messages -- in particular,
// any replayed conversation history -- RunInConversation actually sent
// to the model. serveLoopChat itself only cares what it returns, never
// what it received, so this is a separate helper rather than a change
// to it.
type loopChatCapture struct {
	requests []loopChatRequest
}

func serveLoopChatCapture(t *testing.T, capture *loopChatCapture, messages ...string) *httptest.Server {
	t.Helper()
	calls := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read captured request body: %v", err)
		}
		var req loopChatRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Fatalf("decode captured request body: %v", err)
		}
		capture.requests = append(capture.requests, req)

		if calls >= len(messages) {
			t.Fatalf("unexpected extra chat call %d (only %d responses configured)", calls+1, len(messages))
		}
		msg := messages[calls]
		calls++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":%s}]}`, msg)
	}))
}

// TestToolCallingAgentRunInConversationReplaysHistory proves
// RunInConversation's core contract: prior turns already stored for a
// cart_id are replayed as real chat history ahead of the new prompt
// (system prompt, then history, then the new user turn), and both the
// new user prompt and the resulting assistant reply are appended back
// -- exactly the raw-message-replay approach RunInConversation's doc
// comment describes, distinct from BuyerAgent's Intent-merge memory.
func TestToolCallingAgentRunInConversationReplaysHistory(t *testing.T) {
	store := newLoopFakeConversationStore()
	store.turns["cart_1"] = []ConversationTurn{
		{Role: "user", Content: "wireless earbuds under 25000"},
		{Role: "assistant", Content: "Best ANC match under budget."},
	}

	capture := &loopChatCapture{}
	srv := serveLoopChatCapture(t, capture,
		`{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"propose_checkout","arguments":"{\"product_id\":\"airpods-pro-2\",\"reasoning\":\"Confirmed pick.\"}"}}]}`,
	)
	defer srv.Close()

	agent := NewToolCallingAgent("test-key", srv.URL, "test-model", loopTestDeps()).
		WithConversationStore(store)
	result, err := agent.RunInConversation(context.Background(), "cart_1", "yes, that one", "merchant_001")
	if err != nil {
		t.Fatal(err)
	}
	if result.Plan == nil || result.Plan.SelectedID != "airpods-pro-2" {
		t.Fatalf("expected airpods-pro-2 plan, got %+v (clarify=%q)", result.Plan, result.Clarify)
	}

	if len(capture.requests) != 1 {
		t.Fatalf("expected exactly one chat call, got %d", len(capture.requests))
	}
	sent := capture.requests[0].Messages
	// system, replayed user, replayed assistant, new user = 4.
	if len(sent) != 4 {
		t.Fatalf("expected 4 messages (system + 2 replayed + new prompt), got %d: %+v", len(sent), sent)
	}
	if sent[0].Role != "system" {
		t.Fatalf("expected first message to be the system prompt, got role=%s", sent[0].Role)
	}
	if sent[1].Role != "user" || sent[1].Content == nil || *sent[1].Content != "wireless earbuds under 25000" {
		t.Fatalf("expected replayed user turn first, got %+v", sent[1])
	}
	if sent[2].Role != "assistant" || sent[2].Content == nil || *sent[2].Content != "Best ANC match under budget." {
		t.Fatalf("expected replayed assistant turn second, got %+v", sent[2])
	}
	if sent[3].Role != "user" || sent[3].Content == nil || *sent[3].Content != "yes, that one" {
		t.Fatalf("expected the new prompt last, got %+v", sent[3])
	}

	// Both the new user turn and the resulting assistant reply must be
	// appended back -- 2 pre-seeded + 2 new = 4.
	if got := len(store.turns["cart_1"]); got != 4 {
		t.Fatalf("expected 4 stored turns after the call, got %d: %+v", got, store.turns["cart_1"])
	}
	last := store.turns["cart_1"][3]
	if last.Role != "assistant" || last.Content != "Confirmed pick." {
		t.Fatalf("expected the plan's reasoning appended as the final assistant turn, got %+v", last)
	}
}

// TestToolCallingAgentRunInConversationCapsHistory proves the
// loopHistoryTurns cap: only the most recent 6 stored turns are
// replayed, older ones are simply dropped rather than sent to the
// model unbounded.
func TestToolCallingAgentRunInConversationCapsHistory(t *testing.T) {
	store := newLoopFakeConversationStore()
	for i := 0; i < 10; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		store.turns["cart_2"] = append(store.turns["cart_2"], ConversationTurn{
			Role: role, Content: fmt.Sprintf("turn-%d", i),
		})
	}

	capture := &loopChatCapture{}
	srv := serveLoopChatCapture(t, capture, `{"role":"assistant","content":"What is your budget?"}`)
	defer srv.Close()

	agent := NewToolCallingAgent("test-key", srv.URL, "test-model", loopTestDeps()).
		WithConversationStore(store)
	if _, err := agent.RunInConversation(context.Background(), "cart_2", "still deciding", "merchant_001"); err != nil {
		t.Fatal(err)
	}

	sent := capture.requests[0].Messages
	// system + 6 capped history turns + new prompt = 8.
	if len(sent) != 8 {
		t.Fatalf("expected 8 messages (system + capped 6 + new prompt), got %d", len(sent))
	}
	// The oldest 4 of the 10 stored turns (turn-0..turn-3) must have been
	// dropped -- the replayed window should start at turn-4.
	if sent[1].Content == nil || *sent[1].Content != "turn-4" {
		t.Fatalf("expected the oldest replayed turn to be turn-4, got %+v", sent[1])
	}
}

// TestToolCallingAgentRunInConversationFallsBackWithoutStoreOrCartID
// proves memory is strictly additive: no ConversationStore configured,
// or an empty cart_id, both degrade to plain Run with identical
// behavior -- no history lookup, no append attempt.
func TestToolCallingAgentRunInConversationFallsBackWithoutStoreOrCartID(t *testing.T) {
	srv := serveLoopChat(t, `{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"propose_checkout","arguments":"{\"product_id\":\"airpods-pro-2\",\"reasoning\":\"Best ANC match under budget.\"}"}}]}`)
	defer srv.Close()

	agent := NewToolCallingAgent("test-key", srv.URL, "test-model", loopTestDeps())
	result, err := agent.RunInConversation(context.Background(), "cart_3", "wireless earbuds under 25000", "merchant_001")
	if err != nil {
		t.Fatal(err)
	}
	if result.Plan == nil || result.Plan.SelectedID != "airpods-pro-2" {
		t.Fatalf("expected the same result plain Run would give, got %+v", result)
	}

	srv2 := serveLoopChat(t, `{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"propose_checkout","arguments":"{\"product_id\":\"airpods-pro-2\",\"reasoning\":\"Best ANC match under budget.\"}"}}]}`)
	defer srv2.Close()
	store := newLoopFakeConversationStore()
	agent2 := NewToolCallingAgent("test-key", srv2.URL, "test-model", loopTestDeps()).
		WithConversationStore(store)
	result2, err := agent2.RunInConversation(context.Background(), "", "wireless earbuds under 25000", "merchant_001")
	if err != nil {
		t.Fatal(err)
	}
	if result2.Plan == nil || result2.Plan.SelectedID != "airpods-pro-2" {
		t.Fatalf("expected the same result plain Run would give with an empty cart_id, got %+v", result2)
	}
	if len(store.turns) != 0 {
		t.Fatalf("expected no store interaction with an empty cart_id, got %+v", store.turns)
	}
}

// TestToolCallingAgentRunInConversationDegradesOnHistoryError proves a
// ConversationStore.History failure never fails the buyer-facing
// request -- RunInConversation falls back to plain Run for that call,
// same "memory is an enhancement, never a dependency" posture as
// BuyerAgent.PlanCheckoutInConversation.
func TestToolCallingAgentRunInConversationDegradesOnHistoryError(t *testing.T) {
	store := newLoopFakeConversationStore()
	store.historyErr = fmt.Errorf("db unavailable")

	srv := serveLoopChat(t, `{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"propose_checkout","arguments":"{\"product_id\":\"airpods-pro-2\",\"reasoning\":\"Best ANC match under budget.\"}"}}]}`)
	defer srv.Close()

	agent := NewToolCallingAgent("test-key", srv.URL, "test-model", loopTestDeps()).
		WithConversationStore(store)
	result, err := agent.RunInConversation(context.Background(), "cart_4", "wireless earbuds under 25000", "merchant_001")
	if err != nil {
		t.Fatal(err)
	}
	if result.Plan == nil || result.Plan.SelectedID != "airpods-pro-2" {
		t.Fatalf("expected a successful degrade-to-Run result, got %+v (clarify=%q)", result.Plan, result.Clarify)
	}
}

// TestToolCallingAgentRunInConversationSurvivesAppendFailure proves an
// AppendTurn failure is logged and swallowed, never surfaced to the
// caller -- the buyer already has their plan by the time memory is
// persisted, and a persistence hiccup must not turn that into a failed
// request.
func TestToolCallingAgentRunInConversationSurvivesAppendFailure(t *testing.T) {
	store := newLoopFakeConversationStore()
	store.appendErr = fmt.Errorf("write failed")

	srv := serveLoopChat(t, `{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"propose_checkout","arguments":"{\"product_id\":\"airpods-pro-2\",\"reasoning\":\"Best ANC match under budget.\"}"}}]}`)
	defer srv.Close()

	agent := NewToolCallingAgent("test-key", srv.URL, "test-model", loopTestDeps()).
		WithConversationStore(store)
	result, err := agent.RunInConversation(context.Background(), "cart_5", "wireless earbuds under 25000", "merchant_001")
	if err != nil {
		t.Fatal(err)
	}
	if result.Plan == nil || result.Plan.SelectedID != "airpods-pro-2" {
		t.Fatalf("expected a successful result despite the append failure, got %+v", result)
	}
	if len(store.turns["cart_5"]) != 0 {
		t.Fatalf("expected no turns actually stored given appendErr, got %+v", store.turns["cart_5"])
	}
}
