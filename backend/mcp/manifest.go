package mcp

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"

	"github.com/garinesaiajay/commerceos/policy"
)

// ManifestHandler serves the agent-readable catalog manifest at
// GET /.well-known/agent-commerce.json (PLAN-06-ADDITIONAL-OPPORTUNITIES.md
// §2 -- "Agent-readable catalog" is one of the track brief's own named
// example directions). It's the machine-readable counterpart to
// files/agent-commerce-contract.md's prose: the same REST endpoints, the
// same 11 MCP tools, the same mandate/policy model, the same example
// flows -- but structured JSON an external agent (or a judge's own
// tooling) can fetch and act on directly, rather than a human reading
// markdown.
//
// The tool list comes from s.Tools() -- the live registry RegisterTools
// (tools.go) populated at startup -- never hand-typed here, so it can't
// drift from the real tools/list response the way
// policy.PolicyConfig.AllowedProducts drifted from the seed catalog
// three times before item 32's generator fixed that for good (see
// policy/model.go's DefaultConfig doc comment). The mandate and
// proposed-action field lists below are built the same way, by
// reflecting over policy.Mandate and policy.ProposedAction's own json
// tags, for the identical reason: a hand-typed second copy of either
// struct's shape is exactly the kind of thing that goes stale unnoticed
// (item 32's catalog/allowlist generator is the other example of this
// project deliberately building against that failure mode instead of
// documenting around it).
//
// configFn is called once per request, not captured at handler-
// construction time, so the ceiling/tolerance/allowed-merchant/allowed-
// currency figures below always reflect the policy engine's *live*
// config -- matching main.go's own request_authorization Explain
// closure, which reads policyEngine.Config().Ceiling live for the same
// reason: item 25 (PLAN-05-SELLER-DASHBOARD.md §4) made the ceiling
// operator-editable at runtime via /dashboard/settings/policy, and a
// manifest that cached the startup value would go stale the moment an
// operator changed it.
//
// Deliberately unauthenticated (no RequireOperator wrapper, unlike every
// /dashboard/* route): the entire point of this endpoint is that an
// external agent or judge, holding no operator credentials, can fetch it
// before ever calling anything else. Nothing it discloses is secret --
// every field is either already public (the catalog, the MCP tool
// schemas, POST /mcp itself) or a policy *shape* (check names,
// authorization-level thresholds) that this project's own README and
// audit docs already publish.
func ManifestHandler(s *Server, configFn func() policy.PolicyConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(buildManifest(s, configFn()))
	}
}

func buildManifest(s *Server, cfg policy.PolicyConfig) manifest {
	registered := s.Tools()
	tools := make([]manifestTool, 0, len(registered))
	for _, tl := range registered {
		tools = append(tools, manifestTool{
			Name:        tl.Name,
			Description: tl.Description,
			InputSchema: tl.InputSchema,
		})
	}

	return manifest{
		Name:        "commerceos",
		Description: "Agentic commerce backend for the Razorpay Buildathon: a policy-gated checkout an AI agent can drive end to end, buyer- and seller-side, without ever holding unmediated payment authority.",
		GoverningRule: "Every money-moving call requires an authorization_id from the Policy Engine (POST /policy/propose, or the request_authorization MCP tool), re-verified inside the Payment Service before any Razorpay call. There is no alternate entry point.",
		MCP: manifestMCP{
			Endpoint:  "/mcp",
			Protocol:  "JSON-RPC 2.0",
			Transport: "Plain HTTP POST request/response only -- the Streamable-HTTP transport's server-initiated SSE stream is not implemented, so an MCP client that requires that half of the spec will not work against this endpoint.",
			Handshake: "\"initialize\" returns the spec's InitializeResult shape (protocolVersion, capabilities, serverInfo), echoing back the client's requested protocolVersion when present.",
			Tools:     tools,
		},
		RESTEndpoints: []manifestRESTEndpoint{
			{Method: "GET", Path: "/products", Description: "List the full seeded catalog (features, attributes, use_cases, price, availability). Read-only."},
			{Method: "GET", Path: "/products/{id}", Description: "Read a single product. Read-only."},
			{Method: "GET", Path: "/variants/{id}", Description: "Read a single product variant. Read-only."},
			{Method: "POST", Path: "/agent/checkout", Description: "Fused intent-extraction + catalog-search + Proposed Action assembly for a natural-language prompt. Never moves money or creates a Razorpay order; the caller must still route the resulting ProposedAction through POST /policy/propose."},
			{Method: "POST", Path: "/policy/propose", Description: "Submit a ProposedAction to the deterministic Policy Engine. Returns APPROVED/REJECTED/PENDING_HUMAN_APPROVAL plus an authorization_id when approved, or the specific failed_check and reason when rejected. No money moves here."},
			{Method: "POST", Path: "/orders/{id}/payment", Description: "Execute an approved authorization through the Payment Service. The only endpoint that moves money, and it requires a valid Authorization-Id header issued by /policy/propose."},
			{Method: "GET", Path: "/orders/{id}", Description: "Read order status / detail. Read-only."},
			{Method: "GET", Path: "/orders", Description: "List order history, e.g. ?merchant_id=merchant_001. Read-only."},
			{Method: "POST", Path: "/mcp", Description: "The same capabilities above, exposed as the 11 narrow MCP tools below over JSON-RPC 2.0, for an MCP-speaking agent rather than a direct HTTP client."},
		},
		Policy: manifestPolicy{
			Version:   policy.PolicyVersion,
			Decisions: []string{policy.DecisionApproved, policy.DecisionRejected, policy.DecisionPendingApproval},
			// Only the 9 checks Engine.Evaluate actually runs (policy/engine.go's
			// checks slice) -- policy.CheckNoDuplicate is declared in
			// policy/model.go's Check* const block but is dead: nothing in
			// engine.go ever returns a CheckResult with that name. Listing it
			// here anyway would tell an external agent to expect a check this
			// engine doesn't perform, which is worse than the manifest simply
			// not mentioning it -- found and deliberately excluded while
			// building this manifest (item 35), not fixed here: removing an
			// unused exported constant, or actually wiring a duplicate-action
			// check into Evaluate, is a separate, real change to policy.Engine
			// behavior this file has no business making as a side effect of
			// documenting it.
			Checks: []string{
				policy.CheckMerchantAllowlisted,
				policy.CheckCurrencyAllowed,
				policy.CheckAmountCeiling,
				policy.CheckProductPermitted,
				policy.CheckBudgetTolerance,
				policy.CheckUserConsent,
				policy.CheckMandateNotExpired,
				policy.CheckMandateBound,
				policy.CheckMandateCartBound,
			},
			AllowedMerchants:  cfg.AllowedMerchants,
			AllowedCurrencies: cfg.AllowedCurrencies,
			CeilingPaise:      cfg.Ceiling,
			BudgetTolerance:   cfg.BudgetTolerance,
			AuthorizationLevels: []manifestLevel{
				{Level: 1, Description: "amount <= 100000 paise (Rs 1,000) and risk_score < 0.3: auto-approved, authorization_id issued immediately."},
				{Level: 2, Description: "amount <= 1000000 paise (Rs 10,000), trusted merchant, risk_score < 0.7: PENDING_HUMAN_APPROVAL -- a durable approval request is created and must be approved before an authorization_id is issued."},
				{Level: 3, Description: "amount > 1000000 paise (Rs 10,000), OR an unrecognized merchant, OR risk_score >= 0.7 regardless of amount: hard-gated, PENDING_HUMAN_APPROVAL."},
			},
			MandateFields:        jsonFields(policy.Mandate{}),
			ProposedActionFields: jsonFields(policy.ProposedAction{}),
		},
		ExampleFlows: []manifestFlow{
			{
				Name:        "Guided purchase within a mandate",
				Description: "The standard buyer-agent path from browsing to a completed, policy-authorized payment.",
				Steps: []string{
					"search_products (or GET /products) -- browse the catalog by budget/category/priority",
					"create_cart -- open an empty cart",
					"add_item -- add a chosen variant and quantity (the only way to put anything into the cart)",
					"calculate_total -- optional, pure read of the running total",
					"request_authorization (or POST /policy/propose) -- submit the proposed action against a mandate_id; returns an authorization_id when approved",
					"create_checkout -- turn the cart into an order (still moves no money)",
					"execute_authorized_checkout (or POST /orders/{id}/payment) -- pay using the authorization_id; the backend re-verifies it before any Razorpay call",
					"get_payment_status (or GET /orders/{id}) -- confirm the payment settled",
				},
			},
			{
				Name:        "Cross-sell recommendation mid-checkout",
				Description: "Suggest one additional item without ever committing to it.",
				Steps: []string{
					"recommend_bundle -- score a candidate product as a cross-sell against the current cart, budget, and tolerance; still just a recommendation, no money moves",
				},
			},
			{
				Name:        "Explaining a rejected or pending decision",
				Description: "Turn a failed_check into a plain-language reason for the buyer.",
				Steps: []string{
					"explain_decision -- given the failed_check name and the proposal's amount/merchant/items, returns a human-readable explanation",
				},
			},
		},
	}
}

// jsonFields reflects over a struct value's exported fields and returns
// their JSON wire names and Go types, in declaration order. It exists so
// the manifest can publish policy.Mandate and policy.ProposedAction's
// shapes without a hand-typed second copy of either struct that could
// silently drift from the real one -- see this file's own top-level doc
// comment for why that specific failure mode is something this project
// now deliberately builds against rather than just documents around.
func jsonFields(v any) []manifestField {
	t := reflect.TypeOf(v)
	fields := make([]manifestField, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		fields = append(fields, manifestField{Name: name, Type: f.Type.String()})
	}
	return fields
}

type manifest struct {
	Name          string                 `json:"name"`
	Description   string                 `json:"description"`
	GoverningRule string                 `json:"governing_rule"`
	MCP           manifestMCP            `json:"mcp"`
	RESTEndpoints []manifestRESTEndpoint `json:"rest_endpoints"`
	Policy        manifestPolicy         `json:"policy"`
	ExampleFlows  []manifestFlow         `json:"example_flows"`
}

type manifestMCP struct {
	Endpoint  string         `json:"endpoint"`
	Protocol  string         `json:"protocol"`
	Transport string         `json:"transport"`
	Handshake string         `json:"handshake"`
	Tools     []manifestTool `json:"tools"`
}

type manifestTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type manifestRESTEndpoint struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	Description string `json:"description"`
}

type manifestPolicy struct {
	Version              string          `json:"policy_version"`
	Decisions            []string        `json:"decisions"`
	Checks               []string        `json:"checks"`
	AllowedMerchants     []string        `json:"allowed_merchants"`
	AllowedCurrencies    []string        `json:"allowed_currencies"`
	CeilingPaise         int64           `json:"ceiling_paise"`
	BudgetTolerance      float64         `json:"budget_tolerance"`
	AuthorizationLevels  []manifestLevel `json:"authorization_levels"`
	MandateFields        []manifestField `json:"mandate_fields"`
	ProposedActionFields []manifestField `json:"proposed_action_fields"`
}

type manifestLevel struct {
	Level       int    `json:"level"`
	Description string `json:"description"`
}

type manifestField struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type manifestFlow struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Steps       []string `json:"steps"`
}
