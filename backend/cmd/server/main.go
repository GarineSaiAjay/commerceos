package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/garinesaiajay/commerceos/agents"
	"github.com/garinesaiajay/commerceos/analytics"
	"github.com/garinesaiajay/commerceos/audit"
	"github.com/garinesaiajay/commerceos/auth"
	"github.com/garinesaiajay/commerceos/campaign"
	"github.com/garinesaiajay/commerceos/commerce/cart"
	"github.com/garinesaiajay/commerceos/commerce/catalog"
	"github.com/garinesaiajay/commerceos/commerce/order"
	"github.com/garinesaiajay/commerceos/commerce/payment"
	"github.com/garinesaiajay/commerceos/commerce/review"
	"github.com/garinesaiajay/commerceos/events"
	"github.com/garinesaiajay/commerceos/growth"
	db "github.com/garinesaiajay/commerceos/infra/db"
	"github.com/garinesaiajay/commerceos/mcp"
	"github.com/garinesaiajay/commerceos/policy"
	"github.com/garinesaiajay/commerceos/safety"
	"github.com/garinesaiajay/commerceos/tools"
	"github.com/redis/go-redis/v9"
)

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "OK")
}

func corsMiddleware(allowedOrigin string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The checkout flow sends Authorization-Id and Idempotency-Key
		// headers on the payment call, so they must pass preflight.
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Authorization-Id, Idempotency-Key")
		w.Header().Set("Access-Control-Expose-Headers", "Authorization-Id, Idempotency-Key")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://commerceos:commerceos_dev_password@localhost:5433/commerceos?sslmode=disable"
	}

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	// Browser origin allowed to call the Commerce Service (CORS).
	// Previously hardcoded to http://localhost:3000 with no override --
	// deploying the frontend anywhere else (a public demo URL, a
	// different port) meant every cross-origin request failed silently
	// in the browser with no server-side error at all.
	frontendOrigin := os.Getenv("FRONTEND_ORIGIN")
	if frontendOrigin == "" {
		frontendOrigin = "http://localhost:3000"
	}

	ctx := context.Background()

	dbPool, err := db.NewPostgresPool(ctx, databaseURL)
	if err != nil {
		fmt.Printf("failed to connect to postgres: %v\n", err)
		os.Exit(1)
	}
	defer dbPool.Close()

	fmt.Println("Connected to PostgreSQL")

	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})

	// Same defense-in-depth as db.NewPostgresPool's own retry loop:
	// infra/docker-compose.yml's health-gating is the first line, this
	// is the second, independent one for when Compose isn't in the
	// picture (e.g. running the backend directly on the host).
	const redisConnectRetries = 10
	var redisPingErr error
	for attempt := 1; attempt <= redisConnectRetries; attempt++ {
		redisPingErr = redisClient.Ping(ctx).Err()
		if redisPingErr == nil {
			break
		}
		if attempt < redisConnectRetries {
			fmt.Printf("redis not ready yet (attempt %d/%d): %v\n", attempt, redisConnectRetries, redisPingErr)
			time.Sleep(2 * time.Second)
		}
	}
	if redisPingErr != nil {
		fmt.Printf("failed to connect to redis: %v\n", redisPingErr)
		os.Exit(1)
	}
	defer redisClient.Close()

	fmt.Println("Connected to Redis")

	// -------------------------
	// Catalog
	// -------------------------

	catalogRepo := catalog.NewPostgresRepository(dbPool)
	catalogService := catalog.NewService(catalogRepo)
	catalogHandler := catalog.NewHandler(catalogService)

	// -------------------------
	// Phase 4: Buyer Agent
	// -------------------------

	// Use a real LLM extractor (OpenRouter) when configured, raced against
	// the deterministic extractor rather than called serially -- see
	// RacingExtractor's doc comment. The deterministic extractor is a pure
	// function over the prompt string (no I/O), so racing it costs nothing;
	// the LLM extractor's own HTTP client timeout is a 60s safety ceiling,
	// but RacingExtractor's race window (3.5s) is what actually bounds
	// perceived latency in the common case -- if the LLM hasn't answered
	// by then, the buyer sees the deterministic answer immediately instead
	// of a frozen "Thinking..." state. Any LLM failure (timeout, network
	// error, bad response) still recovers to the deterministic extractor,
	// exactly as FallbackExtractor did -- RacingExtractor is a strict
	// latency improvement on the same recovery contract, not a behavior
	// change to it. Without an API key at all, the deterministic extractor
	// is used directly.
	//
	// llmExtractor is deliberately compared to nil BEFORE being assigned
	// to the agents.IntentExtractor interface variable below, not after:
	// agents.NewLLMExtractorFromEnv() returns a concrete *agents.LLMExtractor,
	// and a nil pointer of a concrete type, once boxed into an interface
	// value, no longer compares equal to nil (a well-known Go footgun --
	// the interface value then has a non-nil type descriptor even though
	// its data pointer is nil). Comparing the concrete pointer directly,
	// while it is still a concrete pointer, avoids that entirely.
	llmExtractor := agents.NewLLMExtractorFromEnv()
	deterministicExtractor := agents.NewDeterministicExtractor()

	var agentExtractor agents.IntentExtractor = deterministicExtractor
	if llmExtractor != nil {
		agentExtractor = agents.NewRacingExtractor(llmExtractor, deterministicExtractor)
	}
	agentSearcher := agents.NewSearcher(catalogRepo)

	// agentConversationStore backs conversation memory (PLAN-01-AGENTIC-CORE.md
	// §3, ROADMAP-PRIORITIZED.md P0 item 6): a follow-up like "no, for my
	// brother instead" is understood against what the buyer already said
	// in this cart, instead of failing extraction from scratch. Wiring it
	// via WithConversationStore is opt-in and additive -- buyerAgent's
	// original PlanCheckout keeps working unchanged either way.
	agentConversationStore := agents.NewPostgresConversationStore(dbPool)
	buyerAgent := agents.NewBuyerAgent(agentExtractor, agentSearcher).
		WithConversationStore(agentConversationStore)
	agentHandler := agents.NewHandler(buyerAgent)

	// -------------------------
	// Phase 5: Growth Agent
	// -------------------------

	growthStore := growth.NewPostgresStore(dbPool)
	growthAgent := growth.NewGrowthAgent(catalogRepo, growthStore)
	growthHandler := growth.NewHandler(growthAgent, growthStore)

	// -------------------------
	// Campaign Orchestrator
	// -------------------------
	// growthStore already satisfies campaign.DemandSource structurally
	// (RejectedDemandByProduct, backend/growth/demand.go) -- no adapter
	// needed. catalogRepo already satisfies campaign.CatalogReader
	// (GetProduct) the same way growth.GrowthAgent uses it.

	campaignRepo := campaign.NewPostgresRepository(dbPool)
	campaignEngine := campaign.NewEngine(campaign.DefaultConfig())
	campaignAgent := campaign.NewCampaignAgent(catalogRepo, growthStore, campaignRepo, campaignEngine)
	campaignHandler := campaign.NewHandler(campaignAgent, campaignRepo)

	// -------------------------
	// Phase 6: Analytics
	// -------------------------

	analyticsService := analytics.NewService(dbPool)
	experimentService := analytics.NewExperimentService(dbPool)

	// -------------------------
	// Cart
	// -------------------------

	cartRepo := cart.NewPostgresRepository(dbPool)
	cartService := cart.NewService(cartRepo, catalogRepo)
	cartHandler := cart.NewHandler(cartService)

	// The bounded tool-calling agent (PLAN-01-AGENTIC-CORE.md §2,
	// ROADMAP-PRIORITIZED.md P1 item 18) shares the exact same
	// backend/tools package the MCP server's tool handlers use (item 17)
	// -- constructed here, after catalogService/cartService/growthAgent
	// all exist, and wired onto the already-built agentHandler.
	// NewToolCallingAgentFromEnv returns nil without OPENROUTER_API_KEY,
	// same convention as llmExtractor above; WithLoopAgent(nil) makes
	// /agent/loop respond 503 rather than panic, leaving /agent/checkout
	// fully unaffected.
	toolLoopAgent := agents.NewToolCallingAgentFromEnv(tools.Dependencies{
		Catalog: catalogService,
		Cart:    cartService,
		Growth:  growthAgent,
	})
	agentHandler.WithLoopAgent(toolLoopAgent)

	// -------------------------
	// Order
	// -------------------------

	orderRepo := order.NewPostgresRepository(dbPool)
	orderService := order.NewService(orderRepo)
	orderHandler := order.NewHandler(orderService)

	// -------------------------
	// Reviews (PLAN-02-CATALOG-AND-COMMERCE.md §2, ROADMAP-PRIORITIZED.md
	// P1 item 11)
	// -------------------------

	reviewRepo := review.NewPostgresRepository(dbPool)
	reviewService := review.NewService(reviewRepo, orderService)
	reviewHandler := review.NewHandler(reviewService)

	// -------------------------
	// Payment
	// -------------------------

	razorpayKeyID := os.Getenv("RAZORPAY_KEY_ID")
	razorpayKeySecret := os.Getenv("RAZORPAY_KEY_SECRET")

	if razorpayKeyID == "" || razorpayKeySecret == "" {
		fmt.Println("RAZORPAY_KEY_ID and RAZORPAY_KEY_SECRET must be set")
		os.Exit(1)
	}

	razorpayClient := payment.NewRazorpayClient(
		razorpayKeyID,
		razorpayKeySecret,
	)

	// The RazorpayAdapter is the only code path that touches the Razorpay
	// SDK. The Payment Service depends only on the narrow Provider surface,
	// so swapping rails (mock, x402, …) is a one-line change.
	razorpayAdapter := payment.NewRazorpayAdapter(razorpayClient)

	paymentRepo := payment.NewPostgresRepository(dbPool)
	paymentAttemptRepo := payment.NewPostgresAttemptRepository(dbPool)

	// Phase 3: Policy Engine — the hard chokepoint.
	policyRepo := policy.NewPostgresRepository(dbPool)
	policyEngine := policy.NewEngine(policy.DefaultConfig(), policyRepo)
	riskEngine := policy.NewRiskEngine()
	policyService := policy.NewService(policyEngine, riskEngine, policyRepo)

	// PolicyConfig.AllowedProducts (policy/model.go) is a static
	// fallback list that has gone stale three times now -- twice from a
	// forgotten hand-edit, and a third time (the bug this wiring fixes)
	// because ROADMAP-PRIORITIZED.md item 14
	// (frontend/app/dashboard/catalog/page.tsx) lets a merchant add a
	// real product at runtime, which no static list can ever reflect.
	// WithProductExistsFunc replaces the static membership check with a
	// live one against catalogService -- a product added through the
	// dashboard is immediately purchasable, with zero further code
	// changes needed the next time the catalog changes. Engine itself
	// stays free of any import on the catalog package (policy/engine.go's
	// ProductExistsFunc is a plain function type, not a catalog-typed
	// interface); only this closure, living here in main.go where both
	// packages are already imported, knows about catalog.ErrProductNotFound.
	policyEngine.WithProductExistsFunc(func(ctx context.Context, productID string) (bool, error) {
		_, err := catalogService.GetProduct(ctx, productID)
		if errors.Is(err, catalog.ErrProductNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return true, nil
	})

	auditVerifier := audit.NewVerifier(dbPool)
	policyHandler := policy.NewHandler(policyService, auditVerifier)
	analyticsHandler := analytics.NewHandler(analyticsService, experimentService, auditVerifier)

	// Phase 9: operator authentication -- gates the merchant dashboard and
	// the approve/reject/safety endpoints. Buyer checkout stays guest; see
	// files/AUTH.md.
	authRepo := auth.NewPostgresRepository(dbPool)
	authService := auth.NewService(authRepo)
	authHandler := auth.NewHandler(authService)

	// Phase 8: safety / red-team — the runner drives the real policy
	// pipeline and reports provider-call deltas from the real counter.
	safetyRunner := safety.NewRunner(policyService, razorpayAdapter)
	safetyHandler := safety.NewHandler(safetyRunner, safety.NewStore(dbPool))

	paymentService := payment.NewServiceWithAuthorizer(
		razorpayAdapter,
		paymentRepo,
		paymentAttemptRepo,
		orderRepo,
		policyService,
	)

	paymentHandler := payment.NewHandler(
		paymentService,
		orderRepo,
	).WithCallCounter(razorpayAdapter).
		WithRecoveryReaders(cartRepo, paymentAttemptRepo).
		WithRecoveryActions(cartService, orderService)

	// -------------------------
	// Phase 7: MCP server
	// -------------------------

	mcpServer := mcp.NewServer()
	mcp.RegisterTools(mcpServer, mcp.Dependencies{
		Catalog: catalogService,
		Cart:    cartService,
		Order:   orderService,
		Payment: paymentService,
		Policy:  policyService,
		Growth:  growthAgent,
		Explain: func(a policy.ProposedAction, m policy.Mandate, check string) string {
			return policy.ExplainRejection(check, a, m)
		},
	})
	mcpHandler := mcp.NewHTTPServer(mcpServer)

	// -------------------------
	// Phase 2: webhook pipeline
	// -------------------------

	// Razorpay signs webhooks with its own webhook secret (configured in
	// the Razorpay dashboard), which may differ from the API Key Secret.
	// Use RAZORPAY_WEBHOOK_SECRET when provided; fall back to the key
	// secret for backward compatibility.
	webhookSecret := os.Getenv("RAZORPAY_WEBHOOK_SECRET")
	if webhookSecret == "" {
		webhookSecret = razorpayKeySecret
	}
	webhookVerifier := payment.NewWebhookSignatureVerifier(webhookSecret)
	webhookStore := payment.NewPostgresWebhookEventStore(dbPool)

	auditWriter := audit.NewPostgresWriter(dbPool)

	// Campaign discounts applied in order.CheckoutCart write
	// best-effort audit events (campaign_discount_applied /
	// campaign_budget_exhausted) after each checkout commits -- see the
	// comment above that call site for why it can't be part of the
	// checkout transaction itself.
	orderRepo = orderRepo.WithAuditWriter(auditWriter)

	outboxRepo := events.NewPostgresOutboxRepository(dbPool)

	webhookApplier := payment.NewWebhookApplier(
		paymentRepo,
		orderRepo,
		auditWriter,
		outboxRepo,
	).WithAttempts(paymentAttemptRepo)

	webhookProcessor := payment.NewWebhookProcessor(
		webhookVerifier,
		webhookStore,
		webhookApplier,
	)

	webhookHandler := payment.NewWebhookHandler(webhookProcessor)

	// -------------------------
	// Phase 2: outbox worker (Redis Streams event bus)
	// -------------------------

	eventBus := events.NewRedisStreamBus(redisClient)
	outboxWorker := events.NewOutboxWorker(
		outboxRepo,
		eventBus,
		"commerceos.events",
	)

	go func() {
		fmt.Println("Outbox Worker started")

		if err := outboxWorker.Run(ctx); err != nil {
			fmt.Printf("Outbox Worker stopped: %v\n", err)
		}
	}()

	// Placeholder stream consumer proving the event bus is wired.
	streamConsumer := events.NewStreamConsumer(
		redisClient,
		"commerceos.events",
		"commerceos-group",
	)

	go func() {
		fmt.Println("Stream Consumer started")

		if err := streamConsumer.Run(ctx); err != nil {
			fmt.Printf("Stream Consumer stopped: %v\n", err)
		}
	}()

	// -------------------------
	// Service routers
	// -------------------------

	apiGatewayMux := http.NewServeMux()
	commerceMux := http.NewServeMux()
	// agentAPIMux and dashboardMux are intentional, not unfinished: Phase 1
	// envisioned four separate services (API Gateway :8080, Commerce :8081,
	// Agent API :8082, Dashboard API :8083), but every real route -- agent
	// checkout, growth, policy, payment, catalog, cart, order, dashboard
	// overview/metrics/experiment -- lives on commerceMux/:8081 today. Each
	// mux still gets its own port and health check below so the original
	// service boundary is one config change away if it's ever needed, but
	// collapsing four network hops into one process for a prototype this
	// size is a feature, not a gap -- splitting them now would be exactly
	// the "seven microservices for architecture theatre" anti-pattern this
	// project explicitly rejects (see files/pitch-one-pager.md's "Why a
	// modular monolith, not microservices" section). Revisit only if the
	// single-service design becomes a real constraint.
	agentAPIMux := http.NewServeMux()
	dashboardMux := http.NewServeMux()

	// -------------------------
	// API Gateway
	// -------------------------

	apiGatewayMux.HandleFunc("/health", healthHandler)

	// -------------------------
	// Commerce Service
	// -------------------------

	commerceMux.HandleFunc("/health", healthHandler)

	// Catalog
	// Catalog mutation (create/update/delete) is merchant-only -- these
	// were previously reachable with no authentication at all, meaning
	// anyone who could reach the Commerce Service could rewrite prices,
	// delete products, or plant a prompt-injection payload into a
	// product's attributes (see the deliberately-planted example on
	// wireless-charging-pad in db/seeds/001_catalog.sql, kept there for
	// safety.AttackLibrary's att_14). GET stays open: the buyer checkout
	// flow and any external MCP/agent client both need to browse the
	// catalog without an operator session.
	commerceMux.HandleFunc(
		"/products",
		func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost:
				authService.RequireOperator(catalogHandler.CreateProduct)(w, r)

			case http.MethodGet:
				catalogHandler.ListProducts(w, r)

			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		},
	)

	commerceMux.HandleFunc(
		"/products/",
		func(w http.ResponseWriter, r *http.Request) {
			switch {
			// GET /products/{id}/reviews -- checked first so it never
			// shadows the plain GET /products/{id} case below.
			case r.Method == http.MethodGet &&
				strings.HasSuffix(r.URL.Path, "/reviews"):
				reviewHandler.ListByProduct(w, r)

			case r.Method == http.MethodGet:
				catalogHandler.GetProduct(w, r)

			case r.Method == http.MethodPatch:
				authService.RequireOperator(catalogHandler.UpdateProduct)(w, r)

			case r.Method == http.MethodDelete:
				authService.RequireOperator(catalogHandler.DeleteProduct)(w, r)

			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		},
	)

	commerceMux.HandleFunc(
		"/variants/",
		catalogHandler.GetVariant,
	)

	// Cart
	commerceMux.HandleFunc(
		"/carts",
		cartHandler.CreateCart,
	)

	commerceMux.HandleFunc(
		"/carts/",
		func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost &&
				strings.HasSuffix(r.URL.Path, "/items"):
				cartHandler.AddItem(w, r)

			case r.Method == http.MethodPatch &&
				strings.Contains(r.URL.Path, "/items/"):
				cartHandler.UpdateItemQuantity(w, r)

			case r.Method == http.MethodDelete &&
				strings.Contains(r.URL.Path, "/items/"):
				cartHandler.RemoveItem(w, r)

			case r.Method == http.MethodPost &&
				strings.HasSuffix(r.URL.Path, "/checkout"):
				orderHandler.Checkout(w, r)

			case r.Method == http.MethodGet:
				cartHandler.GetCart(w, r)

			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		},
	)

	commerceMux.HandleFunc(
		"/orders/",
		func(w http.ResponseWriter, r *http.Request) {
			switch {
			// POST /orders/{id}/review -- the post-checkout "Rate this
			// order" prompt (item 11).
			case r.Method == http.MethodPost &&
				strings.HasSuffix(r.URL.Path, "/review"):
				reviewHandler.Submit(w, r)

			case r.Method == http.MethodPost &&
				strings.HasSuffix(r.URL.Path, "/payment/verify"):
				paymentHandler.VerifyPayment(w, r)

			case r.Method == http.MethodPost &&
				strings.HasSuffix(r.URL.Path, "/payment"):
				paymentHandler.CreatePaymentOrder(w, r)

			// GET /orders/{id}/payment -- the linked payment record for
			// the order-detail view (item 15,
			// PLAN-05-SELLER-DASHBOARD.md §2). Checked before the plain
			// GET /orders/{id} case below via the same suffix-first
			// ordering already used for /recovery and /payment/verify.
			case r.Method == http.MethodGet &&
				strings.HasSuffix(r.URL.Path, "/payment"):
				paymentHandler.GetPayment(w, r)

			case r.Method == http.MethodPost &&
				strings.HasSuffix(r.URL.Path, "/recovery/remove-item"):
				paymentHandler.RemoveItemAndRecheckout(w, r)

			case r.Method == http.MethodGet &&
				strings.HasSuffix(r.URL.Path, "/recovery"):
				paymentHandler.Recovery(w, r)

			// Order history / detail: GET /orders/{id}. Checked after the
			// more specific suffix cases above so it never shadows them.
			case r.Method == http.MethodGet:
				orderHandler.GetOrder(w, r)

			default:
				http.Error(
					w,
					"method not allowed",
					http.StatusMethodNotAllowed,
				)
			}
		},
	)

	// Order history list: GET /orders?merchant_id=... . Registered
	// separately from "/orders/" (Go's ServeMux treats a trailing slash
	// as a distinct subtree pattern).
	commerceMux.HandleFunc(
		"/orders",
		orderHandler.ListOrders,
	)

	commerceMux.HandleFunc(
		"/adapter/calls",
		paymentHandler.CallCount,
	)

	commerceMux.HandleFunc(
		"/webhooks/razorpay",
		webhookHandler.HandleRazorpay,
	)

	// Phase 9: operator authentication.
	commerceMux.HandleFunc("/auth/login", authHandler.Login)
	commerceMux.HandleFunc("/auth/logout", authHandler.Logout)

	// Phase 3: policy routes
	commerceMux.HandleFunc(
		"/policy/mandates",
		policyHandler.CreateMandate,
	)

	commerceMux.HandleFunc(
		"/policy/propose",
		policyHandler.Propose,
	)

	// Level 2/3 durable human-approval requests. Listing is merchant-only
	// (it exposes every buyer's pending approvals); fetching or acting on
	// a single request by ID stays reachable by the buyer who owns it --
	// see the /approval-requests/ subtree below and files/AUTH.md.
	commerceMux.HandleFunc("/approval-requests", authService.RequireOperator(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			policyHandler.ListApprovalRequests(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}))

	// OptionalOperator attaches a verified operator to the context (or
	// leaves it anonymous) so Approve/Reject can resolve either legitimate
	// caller -- see resolveApprover in backend/policy/service.go. Listing
	// is merchant-only, enforced explicitly below since only part of this
	// subtree needs it.
	commerceMux.HandleFunc(
		"/approval-requests/",
		authService.OptionalOperator(func(w http.ResponseWriter, r *http.Request) {
			path := strings.TrimPrefix(r.URL.Path, "/approval-requests/")
			path = strings.Trim(path, "/")
			switch {
			case path == "":
				// GET /approval-requests?status=... → list (merchant-only:
				// exposes every buyer's pending approvals).
				if r.Method == http.MethodGet {
					if _, ok := auth.OperatorFromContext(r.Context()); !ok {
						http.Error(w, "authentication required", http.StatusUnauthorized)
						return
					}
					policyHandler.ListApprovalRequests(w, r)
					return
				}
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

			case r.Method == http.MethodPost && strings.HasSuffix(path, "/approve"):
				policyHandler.Approve(w, r)

			case r.Method == http.MethodPost && strings.HasSuffix(path, "/reject"):
				policyHandler.Reject(w, r)

			case r.Method == http.MethodGet:
				policyHandler.GetApprovalRequest(w, r)

			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		}),
	)

	commerceMux.HandleFunc(
		"/audit/verify",
		authService.RequireOperator(policyHandler.VerifyAuditChain),
	)

	// Campaign orchestrator -- merchant-only end to end: there is no
	// buyer-facing action here (unlike approval-requests, which a buyer
	// can also approve/reject for their own cart), so every route below
	// uses RequireOperator rather than OptionalOperator.
	commerceMux.HandleFunc("/campaigns/propose", authService.RequireOperator(campaignHandler.Propose))
	commerceMux.HandleFunc("/campaigns", authService.RequireOperator(campaignHandler.List))
	commerceMux.HandleFunc("/campaigns/", authService.RequireOperator(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/campaigns/")
		path = strings.Trim(path, "/")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/approve"):
			campaignHandler.Approve(w, r)
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/reject"):
			campaignHandler.Reject(w, r)
		case r.Method == http.MethodGet:
			campaignHandler.Get(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))

	// Phase 8: safety / red-team -- merchant-only (files/AUTH.md).
	commerceMux.HandleFunc("/safety/attacks", authService.RequireOperator(safetyHandler.ListAttacks))
	commerceMux.HandleFunc("/safety/evaluations", authService.RequireOperator(safetyHandler.ListEvaluations))
	commerceMux.HandleFunc("/safety/evaluations/run", authService.RequireOperator(safetyHandler.RunSuite))
	commerceMux.HandleFunc("/safety/evaluations/", authService.RequireOperator(safetyHandler.GetEvaluation))
	commerceMux.HandleFunc("/safety/attacks/", authService.RequireOperator(safetyHandler.RunAttack))

	// Replay: reconstructed agent runs. Listing every run is merchant-only;
	// fetching a single run by its own ID stays reachable without login so
	// checkout.tsx can show the buyer their own audit trail inline (P0.4).
	commerceMux.HandleFunc("/runs", authService.RequireOperator(policyHandler.HandleListRuns))
	commerceMux.HandleFunc("/runs/", policyHandler.HandleGetRun)

	// Phase 4: agent contract (produces proposals only)
	commerceMux.HandleFunc(
		"/agent/checkout",
		agentHandler.PlanCheckout,
	)

	// Item 18: bounded tool-calling agent loop -- a second, genuinely
	// multi-step agentic path alongside the fixed single-shot one above.
	// See tool_loop.go's doc comment for why it can never reach a
	// money-moving tool.
	commerceMux.HandleFunc(
		"/agent/loop",
		agentHandler.PlanCheckoutLoop,
	)

	// Phase 5: growth agent
	commerceMux.HandleFunc(
		"/growth/evaluate",
		growthHandler.Evaluate,
	)

	commerceMux.HandleFunc(
		"/growth/recommend/",
		growthHandler.Explain,
	)

	// Phase 5b: cross-sell suggestion for the checkout UI. Wraps the same
	// GrowthAgent.EvaluateCandidate path /growth/evaluate uses, but picks
	// the candidate and its EV inputs deterministically instead of
	// requiring the caller to supply them (see growth/suggest.go).
	// orderService also backs SuggestForOrder's post-checkout surface
	// (item 19) -- same GetOrder path the buyer's own order-history view
	// already uses unauthenticated (files/AUTH.md). WithImpressions
	// (item 20) turns on the per-cart frequency cap and impression/
	// acceptance recording -- growthStore already implements
	// ImpressionStore (postgres_store.go).
	growthSuggestHandler := growth.NewSuggestHandler(catalogRepo, cartService, orderService, growthAgent, growthStore).
		WithImpressions(growthStore)

	commerceMux.HandleFunc(
		"/growth/suggest",
		growthSuggestHandler.Suggest,
	)

	// Product-detail cross-sell (item 19, PLAN-03-PROACTIVE-GROWTH-
	// AGENT.md §3): scores against one viewed product's own tags instead
	// of a cart's aggregate, reaching a buyer who never adds anything to
	// a cart or opens the agent chat.
	commerceMux.HandleFunc(
		"/growth/suggest/product",
		growthSuggestHandler.SuggestForProduct,
	)

	// Post-checkout "complete the set" cross-sell (item 19, PLAN-03-
	// PROACTIVE-GROWTH-AGENT.md §4): scores against a just-completed
	// order's line items rather than a live cart, since a checked-out
	// cart 404s on GetCart (commerce/cart/service.go).
	commerceMux.HandleFunc(
		"/growth/suggest/order",
		growthSuggestHandler.SuggestForOrder,
	)

	// Persists a buyer's "No thanks" so the same product isn't suggested
	// again for this cart, on any surface (growth.DismissalStore,
	// suggest.go).
	commerceMux.HandleFunc(
		"/growth/suggest/dismiss",
		growthSuggestHandler.Dismiss,
	)

	// Records that a buyer actually added a suggested product (item 20,
	// PLAN-03-PROACTIVE-GROWTH-AGENT.md §8) -- feeds the merchant
	// dashboard's suggestion_impressions/suggestion_acceptances metrics.
	commerceMux.HandleFunc(
		"/growth/suggest/accept",
		growthSuggestHandler.Accept,
	)

	// Phase 6: dashboard -- merchant-only (files/AUTH.md).
	commerceMux.HandleFunc(
		"/dashboard/overview",
		authService.RequireOperator(analyticsHandler.Overview),
	)

	commerceMux.HandleFunc(
		"/dashboard/metrics",
		authService.RequireOperator(analyticsHandler.Metrics),
	)

	commerceMux.HandleFunc(
		"/dashboard/experiment",
		authService.RequireOperator(analyticsHandler.Experiment),
	)

	commerceMux.HandleFunc(
		"/dashboard/experiments",
		authService.RequireOperator(analyticsHandler.ListExperiments),
	)

	// /dashboard/growth (item 24, PLAN-05-SELLER-DASHBOARD.md §3 /
	// PLAN-03-PROACTIVE-GROWTH-AGENT.md §8): the natural home for item
	// 20's impression/acceptance tracking, plus the same rejected-demand
	// query the Campaign Orchestrator already reads
	// (RejectedDemandByProduct) surfaced directly instead of only
	// implicit in CampaignAgent's own argmax pick.
	growthDashboardHandler := growth.NewGrowthDashboardHandler(growthStore, catalogRepo)

	commerceMux.HandleFunc(
		"/dashboard/growth",
		authService.RequireOperator(growthDashboardHandler.Overview),
	)

	// /dashboard/orders (item 15, PLAN-05-SELLER-DASHBOARD.md §2): the
	// merchant "command center" had no view of its own orders at all --
	// GET /orders?merchant_id= already existed but only for the buyer's
	// own unauthenticated order history (files/AUTH.md). This is the
	// operator-scoped equivalent, list only; detail reuses the existing
	// public GET /orders/{id} (and the new GET /orders/{id}/payment
	// above) directly, matching how /runs/{id}'s detail is public while
	// /runs' list is operator-gated.
	commerceMux.HandleFunc(
		"/dashboard/orders",
		authService.RequireOperator(orderHandler.ListOrdersForOperator),
	)

	// Phase 7: MCP endpoint
	commerceMux.Handle(
		"/mcp",
		mcpHandler,
	)

	// -------------------------
	// Agent API Service
	// -------------------------

	agentAPIMux.HandleFunc("/health", healthHandler)

	// -------------------------
	// Dashboard API
	// -------------------------

	dashboardMux.HandleFunc("/health", healthHandler)

	// -------------------------
	// Start API Gateway
	// -------------------------

	apiGatewayPort := os.Getenv("API_GATEWAY_PORT")
	if apiGatewayPort == "" {
		apiGatewayPort = "8080"
	}

	go func() {
		fmt.Printf("API Gateway listening on :%s\n", apiGatewayPort)

		if err := http.ListenAndServe(
			":"+apiGatewayPort,
			apiGatewayMux,
		); err != nil {
			fmt.Printf("API Gateway stopped: %v\n", err)
		}
	}()

	// -------------------------
	// Start Commerce Service
	// -------------------------

	commercePort := os.Getenv("COMMERCE_PORT")
	if commercePort == "" {
		commercePort = "8081"
	}

	go func() {
		fmt.Printf("Commerce Service listening on :%s\n", commercePort)

		if err := http.ListenAndServe(
			":"+commercePort,
			corsMiddleware(frontendOrigin, commerceMux),
		); err != nil {
			fmt.Printf("Commerce Service stopped: %v\n", err)
		}
	}()

	// -------------------------
	// Start Agent API Service
	// -------------------------

	agentAPIPort := os.Getenv("AGENT_API_PORT")
	if agentAPIPort == "" {
		agentAPIPort = "8082"
	}

	go func() {
		fmt.Printf("Agent API Service listening on :%s\n", agentAPIPort)

		if err := http.ListenAndServe(
			":"+agentAPIPort,
			agentAPIMux,
		); err != nil {
			fmt.Printf("Agent API Service stopped: %v\n", err)
		}
	}()

	// -------------------------
	// Start Dashboard API
	// -------------------------

	dashboardPort := os.Getenv("DASHBOARD_PORT")
	if dashboardPort == "" {
		dashboardPort = "8083"
	}

	go func() {
		fmt.Printf("Dashboard API listening on :%s\n", dashboardPort)

		if err := http.ListenAndServe(
			":"+dashboardPort,
			dashboardMux,
		); err != nil {
			fmt.Printf("Dashboard Service stopped: %v\n", err)
		}
	}()

	select {}
}
