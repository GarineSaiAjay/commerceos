package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/garinesaiajay/commerceos/commerce/catalog"
	"github.com/garinesaiajay/commerceos/policy"
)

// newTestServer builds a full 11-tool server the same minimal way
// mcp_test.go's TestToolsList does -- RegisterTools only wires
// handlers closures at this point, it never calls them, so a
// Dependencies with just Catalog set (fakeCatalogRepo{}, defined in
// mcp_test.go, same package) is enough to register every tool.
func newTestServer() *Server {
	srv := NewServer()
	RegisterTools(srv, Dependencies{
		Catalog: catalog.NewService(fakeCatalogRepo{}),
	})
	return srv
}

// TestServerToolsSortedByName proves Tools() returns every registered
// tool (item 35's manifest handler depends on len(Tools()) == 11, the
// same count TestToolsList already asserts for tools/list) in
// deterministic, alphabetically sorted order -- not whatever order Go's
// randomized map iteration happens to produce.
func TestServerToolsSortedByName(t *testing.T) {
	srv := newTestServer()
	tools := srv.Tools()

	if len(tools) != 11 {
		t.Fatalf("expected 11 tools, got %d", len(tools))
	}

	names := make([]string, len(tools))
	for i, tl := range tools {
		names[i] = tl.Name
	}
	if !sort.StringsAreSorted(names) {
		t.Fatalf("Tools() not sorted by name: %v", names)
	}
}

// TestManifestHandlerServesExpectedShape proves GET
// /.well-known/agent-commerce.json (ManifestHandler) returns the full
// contract: all 11 MCP tools (read live off the same registry
// tools/list uses, per this file's own -- manifest.go's -- doc
// comment), the REST endpoint list, the structured policy/mandate
// model built from the live config the caller's configFn supplies, and
// at least one example flow. It does not re-assert individual tool
// schema contents -- TestToolsListHasRealSchemas in mcp_test.go already
// covers that for the exact same underlying Tool values.
func TestManifestHandlerServesExpectedShape(t *testing.T) {
	srv := newTestServer()
	cfg := policy.DefaultConfig()
	cfg.Ceiling = 1_234_500 // a value distinct from any other test's default, to prove configFn's return actually flows through rather than some hardcoded figure

	handler := ManifestHandler(srv, func() policy.PolicyConfig { return cfg })

	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-commerce.json", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}

	var out struct {
		MCP struct {
			Endpoint string `json:"endpoint"`
			Tools    []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"mcp"`
		RESTEndpoints []struct {
			Path string `json:"path"`
		} `json:"rest_endpoints"`
		Policy struct {
			Checks        []string `json:"checks"`
			CeilingPaise  int64    `json:"ceiling_paise"`
			MandateFields []struct {
				Name string `json:"name"`
			} `json:"mandate_fields"`
		} `json:"policy"`
		ExampleFlows []struct {
			Steps []string `json:"steps"`
		} `json:"example_flows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("manifest response is not valid JSON matching the expected shape: %v\n%s", err, rec.Body.String())
	}

	if out.MCP.Endpoint != "/mcp" {
		t.Errorf("expected mcp.endpoint /mcp, got %q", out.MCP.Endpoint)
	}
	if len(out.MCP.Tools) != 11 {
		t.Errorf("expected 11 mcp.tools, got %d", len(out.MCP.Tools))
	}
	if len(out.RESTEndpoints) == 0 {
		t.Errorf("expected at least one rest_endpoints entry")
	}
	if len(out.Policy.Checks) != 9 {
		t.Errorf("expected 9 policy.checks (the ones Engine.Evaluate actually runs, per policy/engine.go's checks slice -- "+
			"policy.CheckNoDuplicate is a declared-but-unused constant and is deliberately excluded, see manifest.go's comment), got %d", len(out.Policy.Checks))
	}
	if out.Policy.CeilingPaise != 1_234_500 {
		t.Errorf("expected policy.ceiling_paise to reflect configFn's live cfg (1234500), got %d -- manifest may be reading a stale/captured config instead of calling configFn per request", out.Policy.CeilingPaise)
	}
	foundMandateID := false
	for _, f := range out.Policy.MandateFields {
		if f.Name == "mandate_id" {
			foundMandateID = true
		}
	}
	if !foundMandateID {
		t.Errorf("expected policy.mandate_fields to include mandate_id (policy.Mandate.ID's json tag), got %+v", out.Policy.MandateFields)
	}
	if len(out.ExampleFlows) == 0 {
		t.Errorf("expected at least one example flow")
	}
	for _, flow := range out.ExampleFlows {
		if len(flow.Steps) == 0 {
			t.Errorf("example flow has zero steps: %+v", flow)
		}
	}
}

// TestManifestHandlerRejectsNonGET proves a POST (or any non-GET) to
// the manifest endpoint gets a 405, not a silent 200 -- this endpoint
// is deliberately read-only and unauthenticated, so method enforcement
// is the only thing standing between it and being (mis)used as
// something it isn't.
func TestManifestHandlerRejectsNonGET(t *testing.T) {
	srv := newTestServer()
	handler := ManifestHandler(srv, func() policy.PolicyConfig { return policy.DefaultConfig() })

	req := httptest.NewRequest(http.MethodPost, "/.well-known/agent-commerce.json", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for POST, got %d", rec.Code)
	}
}
