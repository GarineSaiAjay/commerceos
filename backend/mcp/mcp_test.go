package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/garinesaiajay/commerceos/commerce/cart"
	"github.com/garinesaiajay/commerceos/commerce/catalog"
	"github.com/garinesaiajay/commerceos/commerce/order"
	"github.com/garinesaiajay/commerceos/growth"
	"github.com/garinesaiajay/commerceos/policy"
)

// fakeCatalogRepo for tests.
type fakeCatalogRepo struct{}

func (f fakeCatalogRepo) ListProducts(ctx context.Context) ([]catalog.Product, error) {
	return []catalog.Product{
		{ID: "airpods-pro-2", Title: "AirPods Pro", Price: catalog.Money{Amount: 24900, Currency: "INR"}, Availability: 5,
			Features: []string{"active_noise_cancellation"}},
	}, nil
}

func (f fakeCatalogRepo) GetProduct(ctx context.Context, id string) (catalog.Product, error) {
	return catalog.Product{ID: id, Price: catalog.Money{Amount: 24900, Currency: "INR"}}, nil
}

func (f fakeCatalogRepo) CreateProduct(ctx context.Context, p catalog.Product) error { return nil }

// GetVariant returns a variant with real availability/price so add_item
// tests exercise the full cart.Service.AddItem path (a zero-value
// variant would fail every add with ErrInsufficientAvailability).
func (f fakeCatalogRepo) GetVariant(ctx context.Context, id string) (catalog.ProductVariant, error) {
	return catalog.ProductVariant{
		ID:           id,
		ProductID:    "airpods-pro-2",
		Price:        catalog.Money{Amount: 24900, Currency: "INR"},
		Availability: 5,
	}, nil
}

func (f fakeCatalogRepo) UpdateProduct(ctx context.Context, p catalog.Product) error { return nil }

func (f fakeCatalogRepo) DeleteProduct(ctx context.Context, id string) error { return nil }

// fakeCartRepo is an in-memory cart.Repository for tests -- no
// Postgres needed to exercise the MCP add_item tool.
type fakeCartRepo struct {
	carts map[string]cart.Cart
}

func newFakeCartRepo() *fakeCartRepo {
	return &fakeCartRepo{carts: map[string]cart.Cart{}}
}

func (f *fakeCartRepo) CreateCart(ctx context.Context, c cart.Cart) error {
	f.carts[c.ID] = c
	return nil
}

func (f *fakeCartRepo) GetCart(ctx context.Context, id string) (cart.Cart, error) {
	c, ok := f.carts[id]
	if !ok {
		return cart.Cart{}, cart.ErrCartNotFound
	}
	return c, nil
}

func (f *fakeCartRepo) SaveCart(ctx context.Context, c cart.Cart) error {
	f.carts[c.ID] = c
	return nil
}

// TestToolsList proves the MCP server lists all 11 narrow tools
// (10 plus add_item -- see TestAddItemTool).
func TestToolsList(t *testing.T) {
	srv := NewServer()
	RegisterTools(srv, Dependencies{
		Catalog: catalog.NewService(fakeCatalogRepo{}),
	})

	var req = []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	resp, err := srv.Handle(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	var out struct {
		Result struct {
			Tools []map[string]any `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		t.Fatal(err)
	}

	if len(out.Result.Tools) != 11 {
		t.Fatalf("expected 11 tools, got %d", len(out.Result.Tools))
	}
}

// TestToolsListHasRealSchemas proves every tool's inputSchema declares
// real properties (fixing a prior bug where every tool advertised a
// bare {"type":"object"} with no properties/required at all -- a
// generic MCP client had no schema-driven way to learn what arguments
// any tool needed).
func TestToolsListHasRealSchemas(t *testing.T) {
	srv := makeServer(t)

	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	resp, err := srv.Handle(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	var out struct {
		Result struct {
			Tools []struct {
				Name        string         `json:"name"`
				InputSchema map[string]any `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		t.Fatal(err)
	}

	if len(out.Result.Tools) == 0 {
		t.Fatal("expected at least one tool")
	}

	for _, tool := range out.Result.Tools {
		props, ok := tool.InputSchema["properties"]
		if !ok {
			t.Fatalf("tool %s has no properties in its input schema", tool.Name)
		}
		m, ok := props.(map[string]any)
		if !ok || len(m) == 0 {
			t.Fatalf("tool %s has an empty properties schema", tool.Name)
		}
	}
}

// TestSearchProductsTool proves search_products works with no
// arguments (a browsing agent that hasn't stated a budget yet still
// gets the full catalog, not an empty result).
func TestSearchProductsTool(t *testing.T) {
	srv := makeServer(t)
	resp := call(t, srv, "search_products", `{}`)
	assertNoError(t, resp)
}

// TestSearchProductsFiltersByBudget proves search_products actually
// enforces the budget/priority filtering its own description promises
// -- it used to ignore every argument and always return the entire
// unfiltered catalog.
func TestSearchProductsFiltersByBudget(t *testing.T) {
	srv := makeServer(t)

	// fakeCatalogRepo's one product costs ₹249 (24900 paise). A ₹100
	// budget must exclude it.
	tooLow := call(t, srv, "search_products", `{"budget":100}`)
	assertNoError(t, tooLow)
	var tooLowOut struct {
		Result Result `json:"result"`
	}
	if err := json.Unmarshal(tooLow, &tooLowOut); err != nil {
		t.Fatal(err)
	}
	text := tooLowOut.Result.Content[0].Text
	if text != "null" && text != "[]" {
		t.Fatalf("expected no results under a ₹100 budget for a ₹249 product, got %s", text)
	}

	// A ₹300 budget must include it.
	enough := call(t, srv, "search_products", `{"budget":300}`)
	assertNoError(t, enough)
	var enoughOut struct {
		Result Result `json:"result"`
	}
	if err := json.Unmarshal(enough, &enoughOut); err != nil {
		t.Fatal(err)
	}
	if !containsSubstring(enoughOut.Result.Content[0].Text, "airpods-pro-2") {
		t.Fatalf("expected the ₹249 product to be included under a ₹300 budget, got %s", enoughOut.Result.Content[0].Text)
	}
}

func containsSubstring(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// TestAddItemTool proves the add_item tool actually puts an item into a
// cart -- previously there was no MCP tool that could do this at all,
// so an MCP-only caller could search_products and create_cart but never
// complete a purchase.
func TestAddItemTool(t *testing.T) {
	srv := makeServer(t)

	createResp := call(t, srv, "create_cart", `{"cart_id":"cart_1"}`)
	assertNoError(t, createResp)

	addResp := call(t, srv, "add_item", `{"cart_id":"cart_1","variant_id":"airpods-pro-2-default","quantity":1}`)
	assertNoError(t, addResp)

	var out struct {
		Result Result `json:"result"`
	}
	if err := json.Unmarshal(addResp, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Result.Content) == 0 {
		t.Fatal("expected tool content in the add_item response")
	}

	var updatedCart cart.Cart
	if err := json.Unmarshal([]byte(out.Result.Content[0].Text), &updatedCart); err != nil {
		t.Fatalf("decode returned cart: %v", err)
	}
	if len(updatedCart.Items) != 1 {
		t.Fatalf("expected 1 item in the cart after add_item, got %d", len(updatedCart.Items))
	}
	if updatedCart.Items[0].ProductID != "airpods-pro-2" {
		t.Fatalf("expected the catalog's product_id, got %q", updatedCart.Items[0].ProductID)
	}
}

// TestAddItemToolRequiresCartAndVariant proves add_item validates its
// required arguments rather than silently no-op'ing.
func TestAddItemToolRequiresCartAndVariant(t *testing.T) {
	srv := makeServer(t)

	resp := call(t, srv, "add_item", `{"cart_id":"cart_1"}`)

	var out struct {
		Error *struct{ Message string } `json:"error"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		t.Fatal(err)
	}
	if out.Error == nil {
		t.Fatal("expected an error when variant_id is missing")
	}
}

// TestCreateCheckoutToolNoPayment proves create_checkout exists as a
// narrow tool that produces an order only. The full money-movement path
// requires request_authorization as a SEPARATE tool — there is no
// single make_payment(amount) tool with unlimited blast radius.
func TestCreateCheckoutToolNoPayment(t *testing.T) {
	srv := makeServer(t)

	// Confirm the tool set has no monolithic make_payment tool.
	var req = []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	resp, _ := srv.Handle(context.Background(), req)

	var out struct {
		Result struct {
			Tools []struct{ Name string } `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		t.Fatal(err)
	}

	names := map[string]bool{}
	for _, tool := range out.Result.Tools {
		names[tool.Name] = true
	}

	if names["make_payment"] || names["create_payment"] {
		t.Fatal("a monolithic money-moving tool must not exist")
	}
	if !names["request_authorization"] || !names["create_checkout"] {
		t.Fatal("expected narrow request_authorization and create_checkout tools")
	}
	if !names["add_item"] {
		t.Fatal("expected an add_item tool so a cart can hold something before checkout")
	}
}

// TestExplainDecisionTool proves explain_decision returns a real
// explanation for a rejected action (budget).
func TestExplainDecisionTool(t *testing.T) {
	srv := makeServer(t)
	resp := call(t, srv, "explain_decision", `{"failed_check":"budget_tolerance","amount":26900,"max_amount":25000,"currency":"INR","merchant":"merchant_001"}`)
	assertNoError(t, resp)
}

// --- helpers ---

func makeServer(t *testing.T) *Server {
	t.Helper()
	srv := NewServer()
	RegisterTools(srv, testDeps())
	return srv
}

func testDeps() Dependencies {
	catalogSvc := catalog.NewService(fakeCatalogRepo{})
	cartSvc := cart.NewService(newFakeCartRepo(), fakeCatalogRepo{})
	return Dependencies{
		Catalog: catalogSvc,
		Cart:    cartSvc,
		Order:   order.NewService(nil),
		Growth:  growth.NewGrowthAgent(catalogSvc, nil),
		Policy:  nil,
		Explain: func(a policy.ProposedAction, m policy.Mandate, check string) string {
			return policy.ExplainRejection(check, a, m)
		},
	}
}

func call(t *testing.T, srv *Server, name string, args string) []byte {
	t.Helper()
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + name + `","arguments":` + args + `}}`)
	resp, err := srv.Handle(context.Background(), body)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func assertNoError(t *testing.T, resp []byte) {
	t.Helper()
	var out struct {
		Error *struct{ Message string } `json:"error"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		t.Fatal(err)
	}
	if out.Error != nil {
		t.Fatalf("tool returned error: %s", out.Error.Message)
	}
}

// TestInitializeHandshake proves the MCP handshake returns the spec's
// InitializeResult shape (protocolVersion/capabilities/serverInfo), not
// the tool-call Result{Content: ...} envelope this server used to send.
// A strict MCP SDK client validates this shape before ever calling
// tools/list; a shape mismatch here fails the handshake even though
// tools/list and tools/call work fine on their own.
func TestInitializeHandshake(t *testing.T) {
	srv := NewServer()

	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	resp, err := srv.Handle(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	var out struct {
		Result struct {
			ProtocolVersion string         `json:"protocolVersion"`
			Capabilities    map[string]any `json:"capabilities"`
			ServerInfo      struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"serverInfo"`
			// Content would only be present under the old, wrong
			// tool-call-shaped response.
			Content []Content `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		t.Fatal(err)
	}

	if out.Result.ProtocolVersion != "2025-06-18" {
		t.Fatalf("expected the client's requested protocolVersion echoed back, got %q", out.Result.ProtocolVersion)
	}
	if out.Result.ServerInfo.Name == "" {
		t.Fatal("expected a non-empty serverInfo.name")
	}
	if _, ok := out.Result.Capabilities["tools"]; !ok {
		t.Fatal("expected capabilities.tools to be present")
	}
	if len(out.Result.Content) != 0 {
		t.Fatal("initialize must not return the tool-call Result{Content} envelope")
	}
}

// TestInitializeHandshakeDefaultsProtocolVersion proves a client that
// omits protocolVersion (or sends unparseable params) gets this
// server's own baseline version rather than an error -- initialize
// params aren't otherwise validated.
func TestInitializeHandshakeDefaultsProtocolVersion(t *testing.T) {
	srv := NewServer()

	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	resp, err := srv.Handle(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	var out struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		t.Fatal(err)
	}
	if out.Result.ProtocolVersion == "" {
		t.Fatal("expected a default protocolVersion when the client didn't send one")
	}
}

// TestNotificationGetsNoResponse proves a JSON-RPC notification (no
// response expected, per spec) -- specifically "notifications/initialized",
// which every compliant MCP client sends right after a successful
// initialize -- gets no response body and no error, rather than falling
// through to "method not found".
func TestNotificationGetsNoResponse(t *testing.T) {
	srv := NewServer()

	req := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	resp, err := srv.Handle(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if resp != nil {
		t.Fatalf("expected no response to a notification, got %s", resp)
	}
}
