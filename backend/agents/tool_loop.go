package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/garinesaiajay/commerceos/policy"
	"github.com/garinesaiajay/commerceos/tools"
)

// loopMaxToolCalls bounds how many tool-call round trips one Run makes
// before it must degrade to a clarifying question rather than loop
// indefinitely. loopTimeout is the hard wall-clock ceiling for the
// entire Run call (PLAN-01-AGENTIC-CORE.md §2's "max 4 iterations, hard
// timeout budget e.g. 12s total").
const (
	loopMaxToolCalls = 4
	loopTimeout      = 12 * time.Second
)

// ToolCallingAgent is the bounded, tool-calling shopping agent
// (PLAN-01-AGENTIC-CORE.md §2, ROADMAP-PRIORITIZED.md P1 item 18).
// Unlike BuyerAgent's fixed extract -> search -> propose pipeline, this
// lets the model itself decide, turn by turn, which of a small fixed
// tool palette to call -- search_products, get_product, create_cart,
// add_item, calculate_total, recommend_bundle, exactly the shared
// backend/tools package built for this in item 17 -- before proposing
// a final pick via the loop-only propose_checkout function.
//
// The loop can NEVER reach request_authorization, create_checkout,
// execute_authorized_checkout, get_payment_status, or explain_decision:
// those simply aren't Go functions this type has any way to call (they
// live only in backend/mcp/tools.go, behind the separate external MCP
// surface). This is what makes "the agent proposes, it never decides
// whether money moves" a structural fact about this type rather than a
// prompt instruction the model could be talked around -- the same
// invariant BuyerAgent already upholds, extended to a real multi-step
// loop instead of a fixed single-shot pipeline.
//
// This is deliberately wired to its own new endpoint (POST /agent/loop,
// see Handler.PlanCheckoutLoop) rather than replacing BuyerAgent's
// existing /agent/checkout path. Both stay available: /agent/checkout
// is the fast, already-demoed, already-tested single-shot path;
// /agent/loop is the new, genuinely multi-step agentic path. Swapping
// the frontend over to prefer the loop (or gating it behind a flag) is
// a deliberate follow-up, not done here, so this addition carries zero
// risk to the working demo path.
type ToolCallingAgent struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
	deps    tools.Dependencies

	// conversations opts RunInConversation into memory
	// (PLAN-01-AGENTIC-CORE.md §3), same field name and same
	// nil-is-a-valid-state convention as BuyerAgent.conversations.
	// Unset by default -- Run and RunInConversation both work
	// identically without it.
	conversations ConversationStore
}

// NewToolCallingAgent builds a loop agent. baseURL defaults to
// OpenRouter, same as NewLLMExtractor.
func NewToolCallingAgent(apiKey, baseURL, model string, deps tools.Dependencies) *ToolCallingAgent {
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}
	if model == "" {
		model = "openai/gpt-4o-mini"
	}
	return &ToolCallingAgent{
		apiKey:  apiKey,
		baseURL: strings.TrimSuffix(baseURL, "/"),
		model:   model,
		client:  &http.Client{Timeout: loopTimeout},
		deps:    deps,
	}
}

// NewToolCallingAgentFromEnv reads the same env vars LLMExtractor does
// (OPENROUTER_API_KEY/LLM_BASE_URL/LLM_MODEL). Returns nil without an
// API key -- the loop has no deterministic fallback (unlike
// IntentExtractor's FallbackExtractor/RacingExtractor), since there's
// no rule-based equivalent of multi-step tool-calling reasoning; a nil
// *ToolCallingAgent means /agent/loop is simply unavailable, exactly
// like /agent/checkout falls back to DeterministicExtractor-only
// behavior without an API key.
func NewToolCallingAgentFromEnv(deps tools.Dependencies) *ToolCallingAgent {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		return nil
	}
	return NewToolCallingAgent(apiKey, os.Getenv("LLM_BASE_URL"), os.Getenv("LLM_MODEL"), deps)
}

// WithConversationStore opts a ToolCallingAgent into conversation
// memory (PLAN-01-AGENTIC-CORE.md §3) for RunInConversation, mirroring
// BuyerAgent.WithConversationStore exactly -- same interface, same
// return-the-receiver-for-chaining shape, and the same nil-safety: a
// nil receiver (e.g. cmd/server/main.go chaining this straight onto
// NewToolCallingAgentFromEnv's result, which is nil without
// OPENROUTER_API_KEY) is a documented, supported no-op rather than a
// panic. Leaving this unset is fully supported: Run and
// RunInConversation both work identically without it.
func (a *ToolCallingAgent) WithConversationStore(store ConversationStore) *ToolCallingAgent {
	if a == nil {
		return nil
	}
	a.conversations = store
	return a
}

// loopSystemPrompt instructs the model on the tool palette and the two
// valid ways to end a turn: propose_checkout, or a plain-text
// clarifying question. The catalog this shops against
// (db/seeds/001_catalog.sql) is Apple/Beats audio accessories plus
// AirTag trackers -- same staleness caveat as llm_extractor.go's
// intentSystemPrompt (kept in sync by hand).
const loopSystemPrompt = `You are a shopping assistant for an Indian e-commerce catalog of Apple/Beats audio accessories and AirTag trackers (earbuds, chargers, cases, AirTags).

Use the available tools to find the right product for the buyer's request:
- search_products: browse/filter the catalog by budget (rupees), category, and priority.
- get_product: look up one product's full detail by its exact product_id.
- calculate_total: check arithmetic on a set of {unit_price, quantity} line items.
- create_cart / add_item: build a real cart to sanity-check a bundle idea. These never move money and never require the buyer's confirmation -- they're for your own reasoning only.
- recommend_bundle: score a potential cross-sell add-on with an expected-value estimate.

Once you've found the right product, call propose_checkout with its EXACT product_id from a search_products/get_product result -- never invent a product_id or a price, the catalog is the only source of truth for both. Give one sentence of reasoning. You may name up to 2 other product_ids as alternatives if you found genuinely close matches.

If the request is too vague to search at all (no usable budget and no usable category, even after trying), respond with a plain-text clarifying question instead of calling any tool -- ask what they want and their budget. Never guess.

You have at most a few tool calls before you must either call propose_checkout or ask a clarifying question -- don't keep exploring indefinitely.`

// loopMessage is one Chat Completions message for the tool-calling loop.
// Kept separate from llm_extractor.go's chatMessage (which has no
// tool-calling fields at all) so nothing about the existing, tested
// single-shot extractor is touched by this addition. Content is a
// pointer because an assistant message that calls a tool has
// "content": null on the wire, distinct from an empty string.
type loopMessage struct {
	Role       string         `json:"role"`
	Content    *string        `json:"content"`
	ToolCalls  []loopToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type loopToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function loopToolCallFunc `json:"function"`
}

type loopToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type loopToolDef struct {
	Type     string          `json:"type"`
	Function loopFunctionDef `json:"function"`
}

type loopFunctionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type loopChatRequest struct {
	Model       string        `json:"model"`
	Messages    []loopMessage `json:"messages"`
	Tools       []loopToolDef `json:"tools,omitempty"`
	ToolChoice  string        `json:"tool_choice,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
}

type loopChatChoice struct {
	Message loopMessage `json:"message"`
}

type loopChatResponse struct {
	Choices []loopChatChoice `json:"choices"`
}

func loopSchema(properties map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func loopProp(kind, description string) map[string]any {
	return map[string]any{"type": kind, "description": description}
}

// loopToolPalette is the fixed set of functions offered to the model --
// the same 6 shared tools item 17 built (search_products, get_product,
// calculate_total, create_cart, add_item, recommend_bundle) plus the
// loop-only propose_checkout terminal function. Schemas intentionally
// mirror backend/mcp/tools.go's own tool descriptions for the shared
// ones, since they describe the exact same underlying functions.
func loopToolPalette() []loopToolDef {
	return []loopToolDef{
		{Type: "function", Function: loopFunctionDef{
			Name:        "search_products",
			Description: "Search the catalog by budget (rupees) and priority/category. Deterministic; never bypasses budget/availability. Omit budget to browse the full catalog unfiltered.",
			Parameters: loopSchema(map[string]any{
				"budget":    loopProp("integer", "Maximum price in rupees (not paise). Omit or 0 to browse unfiltered."),
				"category":  loopProp("string", "Product category to prefer, e.g. \"earbuds\"."),
				"priority":  loopProp("string", "A feature tag to prioritize, e.g. \"active_noise_cancellation\", \"battery_life\"."),
				"recipient": loopProp("string", "Who the product is for, e.g. \"my brother\" (informational only)."),
			}),
		}},
		{Type: "function", Function: loopFunctionDef{
			Name:        "get_product",
			Description: "Get one product's full detail by ID.",
			Parameters:  loopSchema(map[string]any{"product_id": loopProp("string", "Catalog product ID.")}, "product_id"),
		}},
		{Type: "function", Function: loopFunctionDef{
			Name:        "calculate_total",
			Description: "Compute a total from a list of line items. Pure; no side effects.",
			Parameters: loopSchema(map[string]any{
				"items": map[string]any{
					"type":        "array",
					"description": "Line items to total.",
					"items": loopSchema(map[string]any{
						"unit_price": loopProp("integer", "Unit price in paise."),
						"quantity":   loopProp("integer", "Quantity."),
					}),
				},
			}, "items"),
		}},
		{Type: "function", Function: loopFunctionDef{
			Name:        "create_cart",
			Description: "Create a cart to sanity-check a bundle idea. No money moves.",
			Parameters: loopSchema(map[string]any{
				"cart_id":     loopProp("string", "A new, caller-chosen unique cart ID."),
				"merchant_id": loopProp("string", "Merchant ID. Defaults to \"merchant_001\" if omitted."),
				"currency":    loopProp("string", "ISO 4217 currency code. Defaults to \"INR\" if omitted."),
			}, "cart_id"),
		}},
		{Type: "function", Function: loopFunctionDef{
			Name:        "add_item",
			Description: "Add a product variant to a cart created with create_cart. No money moves.",
			Parameters: loopSchema(map[string]any{
				"cart_id":    loopProp("string", "ID of a cart from create_cart."),
				"variant_id": loopProp("string", "Catalog variant ID to add. See get_product for a product's variants."),
				"quantity":   loopProp("integer", "Number of units. Defaults to 1."),
			}, "cart_id", "variant_id"),
		}},
		{Type: "function", Function: loopFunctionDef{
			Name:        "recommend_bundle",
			Description: "Score a potential cross-sell add-on with an expected-value estimate. No money moves.",
			Parameters: loopSchema(map[string]any{
				"cart_id":              loopProp("string", "Cart this recommendation is evaluated for."),
				"cart_total":           loopProp("integer", "Current cart subtotal, in paise."),
				"budget":               loopProp("integer", "Buyer's spending ceiling, in paise."),
				"tolerance":            loopProp("number", "Fractional budget tolerance allowed above the ceiling, e.g. 0.10 for 10%."),
				"product_id":           loopProp("string", "Candidate product to evaluate as a cross-sell."),
				"purchase_probability": loopProp("number", "Estimated probability (0..1) the buyer accepts this candidate."),
				"incremental_margin":   loopProp("integer", "Expected incremental gross margin if accepted, in paise."),
				"confidence":           loopProp("number", "Confidence (0..1) in the probability/margin estimates."),
				"risk_cost":            loopProp("integer", "Expected downside cost (e.g. return risk), in paise."),
			}, "product_id"),
		}},
		{Type: "function", Function: loopFunctionDef{
			Name:        "propose_checkout",
			Description: "Finalize your recommendation. Call this once you've found the right product.",
			Parameters: loopSchema(map[string]any{
				"product_id": loopProp("string", "The catalog product_id you recommend -- must be one returned by search_products/get_product, never invented."),
				"reasoning":  loopProp("string", "One sentence explaining the pick."),
				"alternative_product_ids": map[string]any{
					"type":        "array",
					"description": "Up to 2 other catalog product_ids worth mentioning as alternatives.",
					"items":       map[string]any{"type": "string"},
				},
			}, "product_id", "reasoning"),
		}},
	}
}

// LoopStep is one entry in a Run's reasoning trace -- what tool it
// called, a summary of what came back, or how it ended. Not persisted
// anywhere yet (that's PLAN-01 §4 / ROADMAP item 16, not part of this
// change); returned to the caller so the multi-step reasoning this loop
// actually does is visible, not just its final answer.
type LoopStep struct {
	Type   string `json:"type"` // "tool_called" | "tool_result" | "clarify" | "proposed"
	Detail string `json:"detail"`
}

// LoopResult is what one Run call produces: exactly one of Plan or
// Clarify is set, mirroring BuyerAgent.PlanCheckout's
// propose-or-ask-for-clarification contract, plus the step-by-step
// trace of how the loop got there.
type LoopResult struct {
	Plan    *CheckoutPlan `json:"plan,omitempty"`
	Clarify string        `json:"clarify,omitempty"`
	Steps   []LoopStep    `json:"steps"`
}

// reasoningTrail converts a LoopResult's live wire trace (Steps, the
// "tool_called"/"tool_result"/"clarify"/"proposed" values the frontend
// and any existing caller already consume unchanged) into the
// policy.RunStep shape item 16 persists. This is a display-time
// relabeling only, at this one conversion boundary -- "tool_result"
// becomes the more descriptive "tool_result_summary" (matching
// PLAN-01-AGENTIC-CORE.md §4's stage name) here, never on the wire
// LoopStep itself, so nothing about the already-shipped /agent/loop
// response shape changes. Returns nil (not an empty slice) when there's
// no plan to record -- reasoningTrail is only ever called once Run has
// produced a Plan.
func (r LoopResult) reasoningTrail() []policy.RunStep {
	if len(r.Steps) == 0 {
		return nil
	}

	now := time.Now()
	steps := make([]policy.RunStep, 0, len(r.Steps))
	for _, s := range r.Steps {
		stage := s.Type
		if stage == "tool_result" {
			stage = "tool_result_summary"
		}
		steps = append(steps, policy.RunStep{
			Stage:     stage,
			Detail:    s.Detail,
			Timestamp: now,
		})
	}
	return steps
}

// Run executes the bounded tool-calling loop for one buyer prompt, with
// no memory of any prior turn. See RunInConversation for the
// cart_id-scoped memory variant.
func (a *ToolCallingAgent) Run(ctx context.Context, prompt, merchant string) (LoopResult, error) {
	if a == nil {
		return LoopResult{}, fmt.Errorf("tool-calling agent not configured")
	}
	if strings.TrimSpace(prompt) == "" {
		return LoopResult{}, ErrAmbiguousIntent
	}

	messages := []loopMessage{
		{Role: "system", Content: strPtr(loopSystemPrompt)},
		{Role: "user", Content: strPtr(prompt)},
	}

	return a.runLoop(ctx, messages, merchant)
}

// loopHistoryTurns bounds how many prior conversation turns
// RunInConversation replays into the model's context: three
// back-and-forth exchanges (six turns). Older turns are simply
// dropped, not summarized -- a deliberately simple cap so a long-running
// cart conversation can't grow the request sent to the model (and this
// package's own fixed loopTimeout/loopMaxToolCalls budget,
// PLAN-01-AGENTIC-CORE.md §2) without bound. Matches this package's
// existing "simple, not sophisticated" latitude for a buildathon demo
// (see e.g. the login-lockout or ratelimit.Limiter doc comments
// elsewhere in this codebase for the same posture).
const loopHistoryTurns = 6

// RunInConversation is Run plus conversation memory
// (PLAN-01-AGENTIC-CORE.md §3), scoped to cartID exactly like
// BuyerAgent.PlanCheckoutInConversation -- in cmd/server/main.go, the
// exact same *PostgresConversationStore instance backs both. Unlike
// PlanCheckoutInConversation, which merges a single Intent snapshot
// turn over turn, this replays the raw prior messages as real chat
// history ahead of the new prompt: ConversationTurn already stores
// plain role+content, which maps directly onto this package's own
// loopMessage, whereas the loop has no Intent object to merge in the
// first place -- it reasons over free text and tool results directly,
// not a structured slot-filled intent.
//
// Degrades to plain Run with zero behavior change when cartID is empty
// or no ConversationStore is configured (WithConversationStore was
// never called) -- memory is strictly additive, same convention as
// BuyerAgent's. A ConversationStore failure while reading history also
// degrades to plain Run for this call rather than failing the request:
// memory is an enhancement here too, never a dependency the loop's core
// job should ever be blocked on.
//
// Known, deliberately out-of-scope limitation shared with
// BuyerAgent.recordTurn: if a mutation doesn't change the conversation
// (e.g. this exact prompt was already the most recent turn, which
// shouldn't happen in practice since every real call carries a new
// buyer prompt), nothing here deduplicates it -- same trade-off, not a
// new one introduced by this method.
func (a *ToolCallingAgent) RunInConversation(ctx context.Context, cartID, prompt, merchant string) (LoopResult, error) {
	if a == nil {
		return LoopResult{}, fmt.Errorf("tool-calling agent not configured")
	}
	if a.conversations == nil || cartID == "" {
		return a.Run(ctx, prompt, merchant)
	}
	if strings.TrimSpace(prompt) == "" {
		return LoopResult{}, ErrAmbiguousIntent
	}

	history, histErr := a.conversations.History(ctx, cartID)
	if histErr != nil {
		log.Printf("[agents] loop conversation history unavailable for cart %s, continuing without memory: %v", cartID, histErr)
		return a.Run(ctx, prompt, merchant)
	}
	if len(history) > loopHistoryTurns {
		history = history[len(history)-loopHistoryTurns:]
	}

	messages := make([]loopMessage, 0, len(history)+2)
	messages = append(messages, loopMessage{Role: "system", Content: strPtr(loopSystemPrompt)})
	for _, turn := range history {
		messages = append(messages, loopMessage{Role: turn.Role, Content: strPtr(turn.Content)})
	}
	messages = append(messages, loopMessage{Role: "user", Content: strPtr(prompt)})

	result, runErr := a.runLoop(ctx, messages, merchant)

	// Best-effort from here, same convention as BuyerAgent.recordTurn/
	// recordAssistantTurn: a memory-write failure must never fail the
	// buyer-facing request it's layered on top of. No Intent snapshot to
	// store here (unlike BuyerAgent) -- the loop produces none.
	if appendErr := a.conversations.AppendTurn(ctx, cartID, "user", prompt, nil); appendErr != nil {
		log.Printf("[agents] failed to record loop user turn for cart %s: %v", cartID, appendErr)
	}
	if runErr == nil {
		reply := result.Clarify
		if result.Plan != nil {
			reply = result.Plan.Reasoning
		}
		if appendErr := a.conversations.AppendTurn(ctx, cartID, "assistant", reply, nil); appendErr != nil {
			log.Printf("[agents] failed to record loop assistant turn for cart %s: %v", cartID, appendErr)
		}
	}

	return result, runErr
}

// runLoop is Run and RunInConversation's shared tail: given an initial
// message list (system prompt, any replayed history, and the new user
// prompt all already appended), drive the bounded tool-calling loop to
// completion.
func (a *ToolCallingAgent) runLoop(ctx context.Context, messages []loopMessage, merchant string) (LoopResult, error) {
	ctx, cancel := context.WithTimeout(ctx, loopTimeout)
	defer cancel()

	var result LoopResult

	for i := 0; i < loopMaxToolCalls; i++ {
		respMsg, err := a.chat(ctx, messages)
		if err != nil {
			return LoopResult{}, err
		}
		messages = append(messages, respMsg)

		if len(respMsg.ToolCalls) == 0 {
			// No tool call at all -- the model is asking a clarifying
			// question (or simply didn't call propose_checkout). Either
			// way this is a safe no-op, never a guess -- same contract
			// as BuyerAgent.PlanCheckout's ErrAmbiguousIntent path.
			content := ""
			if respMsg.Content != nil {
				content = *respMsg.Content
			}
			result.Clarify = content
			result.Steps = append(result.Steps, LoopStep{Type: "clarify", Detail: content})
			return result, nil
		}

		proposed := false

		for _, call := range respMsg.ToolCalls {
			result.Steps = append(result.Steps, LoopStep{
				Type:   "tool_called",
				Detail: fmt.Sprintf("%s(%s)", call.Function.Name, truncate(call.Function.Arguments, 200)),
			})

			if call.Function.Name == "propose_checkout" {
				plan, finalizeErr := a.finalizeProposal(ctx, call.Function.Arguments, merchant)
				if finalizeErr != nil {
					// Tell the model its proposal was rejected (e.g. an
					// unknown product_id) so it can retry with a real
					// one on the next turn, instead of failing the
					// whole request over a single bad tool call.
					messages = append(messages, loopMessage{
						Role: "tool", ToolCallID: call.ID,
						Content: strPtr(toText(nil, finalizeErr)),
					})
					continue
				}
				result.Plan = &plan
				result.Steps = append(result.Steps, LoopStep{Type: "proposed", Detail: plan.Reasoning})
				// Built after the "proposed" step lands in result.Steps
				// so the persisted trail's final entry mirrors it too.
				result.Plan.ReasoningTrail = result.reasoningTrail()
				proposed = true
				break
			}

			toolResult, toolErr := a.dispatch(ctx, call.Function.Name, call.Function.Arguments)
			resultText := toText(toolResult, toolErr)
			result.Steps = append(result.Steps, LoopStep{
				Type: "tool_result", Detail: truncate(resultText, 300),
			})
			messages = append(messages, loopMessage{
				Role: "tool", ToolCallID: call.ID, Content: strPtr(resultText),
			})
		}

		if proposed {
			return result, nil
		}
	}

	// Exhausted the loop budget without a final proposal or a
	// clarifying question -- degrade gracefully rather than hang or
	// fail the whole request.
	result.Clarify = "I wasn't able to settle on a recommendation in time -- try rephrasing, or browse the catalog below."
	result.Steps = append(result.Steps, LoopStep{Type: "clarify", Detail: result.Clarify})
	return result, nil
}

// proposeCheckoutArgs is propose_checkout's argument shape.
type proposeCheckoutArgs struct {
	ProductID             string   `json:"product_id"`
	Reasoning             string   `json:"reasoning"`
	AlternativeProductIDs []string `json:"alternative_product_ids"`
}

// finalizeProposal turns a propose_checkout call into a real
// CheckoutPlan. The model names a product_id only -- price and
// currency always come from deps.Catalog, never from the model's own
// arguments, the same "the agent proposes, it never invents a price"
// invariant BuyerAgent.planFromIntent upholds.
func (a *ToolCallingAgent) finalizeProposal(ctx context.Context, argsJSON, merchant string) (CheckoutPlan, error) {
	var args proposeCheckoutArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return CheckoutPlan{}, fmt.Errorf("malformed propose_checkout arguments: %w", err)
	}
	if args.ProductID == "" {
		return CheckoutPlan{}, fmt.Errorf("product_id required")
	}

	product, err := a.deps.Catalog.GetProduct(ctx, args.ProductID)
	if err != nil {
		return CheckoutPlan{}, fmt.Errorf("unknown product_id %q -- use one returned by search_products or get_product: %w", args.ProductID, err)
	}

	var alternatives []AlternativeProduct
	for _, altID := range args.AlternativeProductIDs {
		if len(alternatives) >= maxAlternatives {
			break
		}
		alt, err := a.deps.Catalog.GetProduct(ctx, altID)
		if err != nil {
			// Skip a hallucinated alternative rather than fail the
			// whole proposal over it -- the primary pick is still
			// real and verified.
			continue
		}
		alternatives = append(alternatives, AlternativeProduct{
			ProductID: alt.ID, Title: alt.Title, Price: alt.Price.Amount, Currency: alt.Price.Currency,
		})
	}

	reasoning := args.Reasoning
	if reasoning == "" {
		reasoning = fmt.Sprintf("Selected %s (₹%d).", product.Title, product.Price.Amount/100)
	}

	return CheckoutPlan{
		Proposal: policy.ProposedAction{
			Action:   "CREATE_ORDER",
			Amount:   product.Price.Amount,
			Currency: product.Price.Currency,
			Merchant: merchant,
			Items:    []string{product.ID},
		},
		SelectedID:   product.ID,
		Reasoning:    reasoning,
		Alternatives: alternatives,
	}, nil
}

// dispatch routes one tool call to the shared backend/tools package.
// Each case unmarshals into a local, json-tagged wire struct (mirroring
// backend/mcp/tools.go's own handlers exactly) before mapping into the
// tools package's typed request -- those request types have no json
// tags of their own since they were designed to be built from an
// already-parsed struct, not unmarshaled directly.
func (a *ToolCallingAgent) dispatch(ctx context.Context, name, argsJSON string) (any, error) {
	raw := []byte(argsJSON)

	switch name {
	case "search_products":
		var req struct {
			Budget    int64  `json:"budget"`
			Category  string `json:"category"`
			Priority  string `json:"priority"`
			Recipient string `json:"recipient"`
		}
		_ = json.Unmarshal(raw, &req)
		return tools.SearchProducts(ctx, a.deps, tools.SearchProductsRequest{
			Budget: req.Budget, Category: req.Category, Priority: req.Priority, Recipient: req.Recipient,
		})

	case "get_product":
		var req struct {
			ProductID string `json:"product_id"`
		}
		_ = json.Unmarshal(raw, &req)
		return tools.GetProduct(ctx, a.deps, tools.GetProductRequest{ProductID: req.ProductID})

	case "create_cart":
		var req struct {
			CartID     string `json:"cart_id"`
			MerchantID string `json:"merchant_id"`
			Currency   string `json:"currency"`
		}
		_ = json.Unmarshal(raw, &req)
		return tools.CreateCart(ctx, a.deps, tools.CreateCartRequest{
			ID: req.CartID, MerchantID: req.MerchantID, Currency: req.Currency,
		})

	case "add_item":
		var req struct {
			CartID    string `json:"cart_id"`
			VariantID string `json:"variant_id"`
			Quantity  int    `json:"quantity"`
		}
		_ = json.Unmarshal(raw, &req)
		return tools.AddItem(ctx, a.deps, tools.AddItemRequest{
			CartID: req.CartID, VariantID: req.VariantID, Quantity: req.Quantity,
		})

	case "calculate_total":
		var req struct {
			Items []struct {
				UnitPrice int64 `json:"unit_price"`
				Quantity  int   `json:"quantity"`
			} `json:"items"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, fmt.Errorf("items required")
		}
		items := make([]tools.CalculateTotalItem, len(req.Items))
		for i, it := range req.Items {
			items[i] = tools.CalculateTotalItem{UnitPrice: it.UnitPrice, Quantity: it.Quantity}
		}
		return tools.CalculateTotal(ctx, tools.CalculateTotalRequest{Items: items})

	case "recommend_bundle":
		var req struct {
			CartID       string  `json:"cart_id"`
			CartTotal    int64   `json:"cart_total"`
			Budget       int64   `json:"budget"`
			Tolerance    float64 `json:"tolerance"`
			ProductID    string  `json:"product_id"`
			PurchaseProb float64 `json:"purchase_probability"`
			Margin       int64   `json:"incremental_margin"`
			Confidence   float64 `json:"confidence"`
			RiskCost     int64   `json:"risk_cost"`
		}
		_ = json.Unmarshal(raw, &req)
		return tools.RecommendBundle(ctx, a.deps, tools.RecommendBundleRequest{
			CartID: req.CartID, CartTotal: req.CartTotal, Budget: req.Budget, Tolerance: req.Tolerance,
			ProductID: req.ProductID, PurchaseProb: req.PurchaseProb, Margin: req.Margin,
			Confidence: req.Confidence, RiskCost: req.RiskCost,
		})

	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

// chat makes one Chat Completions round trip with the tool palette
// attached. Reuses the same OpenAI-compatible wire format
// llm_extractor.go speaks, extended with tools/tool_choice.
func (a *ToolCallingAgent) chat(ctx context.Context, messages []loopMessage) (loopMessage, error) {
	body, err := json.Marshal(loopChatRequest{
		Model:       a.model,
		Messages:    messages,
		Tools:       loopToolPalette(),
		ToolChoice:  "auto",
		Temperature: 0,
	})
	if err != nil {
		return loopMessage{}, fmt.Errorf("marshal chat request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return loopMessage{}, fmt.Errorf("build chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)

	res, err := a.client.Do(req)
	if err != nil {
		return loopMessage{}, fmt.Errorf("llm request failed: %w", err)
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return loopMessage{}, fmt.Errorf("read llm response: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return loopMessage{}, fmt.Errorf("llm api error %d: %s", res.StatusCode, truncate(string(raw), 400))
	}

	var parsed loopChatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return loopMessage{}, fmt.Errorf("decode llm response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return loopMessage{}, fmt.Errorf("llm returned no choices")
	}
	return parsed.Choices[0].Message, nil
}

// toText serializes a tool result (or its error) into the JSON string a
// "tool" role message carries as content.
func toText(v any, err error) string {
	if err != nil {
		b, marshalErr := json.Marshal(map[string]string{"error": err.Error()})
		if marshalErr != nil {
			return fmt.Sprintf(`{"error":%q}`, err.Error())
		}
		return string(b)
	}
	b, marshalErr := json.Marshal(v)
	if marshalErr != nil {
		return fmt.Sprintf(`{"error":%q}`, marshalErr.Error())
	}
	return string(b)
}

func strPtr(s string) *string { return &s }
