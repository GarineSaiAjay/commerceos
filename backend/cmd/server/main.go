package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/garinesaiajay/commerceos/agents"
	"github.com/garinesaiajay/commerceos/analytics"
	"github.com/garinesaiajay/commerceos/audit"
	"github.com/garinesaiajay/commerceos/commerce/cart"
	"github.com/garinesaiajay/commerceos/commerce/catalog"
	"github.com/garinesaiajay/commerceos/commerce/order"
	"github.com/garinesaiajay/commerceos/commerce/payment"
	"github.com/garinesaiajay/commerceos/events"
	"github.com/garinesaiajay/commerceos/growth"
	db "github.com/garinesaiajay/commerceos/infra/db"
	"github.com/garinesaiajay/commerceos/mcp"
	"github.com/garinesaiajay/commerceos/policy"
	"github.com/garinesaiajay/commerceos/safety"
	"github.com/redis/go-redis/v9"
)

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "OK")
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The checkout flow sends Authorization-Id and Idempotency-Key
		// headers on the payment call, so they must pass preflight.
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization-Id, Idempotency-Key")
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

	ctx := context.Background()

	dbPool, err := db.NewPostgresPool(ctx, databaseURL)
	if err != nil {
		fmt.Printf("failed to connect to postgres: %v\n", err)
		os.Exit(1)
	}
	defer dbPool.Close()

	fmt.Println("Connected to PostgreSQL")

	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		fmt.Printf("failed to connect to redis: %v\n", err)
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

	// Use a real LLM extractor (OpenRouter) when configured; otherwise
	// fall back to the deterministic extractor so the app and tests work
	// without an API key.
	var agentExtractor agents.IntentExtractor = agents.NewLLMExtractorFromEnv()
	if agentExtractor == nil {
		agentExtractor = agents.NewDeterministicExtractor()
	}
	agentSearcher := agents.NewSearcher(catalogRepo)
	buyerAgent := agents.NewBuyerAgent(agentExtractor, agentSearcher)
	agentHandler := agents.NewHandler(buyerAgent)

	// -------------------------
	// Phase 5: Growth Agent
	// -------------------------

	growthStore := growth.NewPostgresStore(dbPool)
	growthAgent := growth.NewGrowthAgent(catalogRepo, growthStore)
	growthHandler := growth.NewHandler(growthAgent, growthStore)

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

	// -------------------------
	// Order
	// -------------------------

	orderRepo := order.NewPostgresRepository(dbPool)
	orderService := order.NewService(orderRepo)
	orderHandler := order.NewHandler(orderService)

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

	auditVerifier := audit.NewVerifier(dbPool)
	policyHandler := policy.NewHandler(policyService, auditVerifier)
	analyticsHandler := analytics.NewHandler(analyticsService, experimentService, auditVerifier)

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
	// project explicitly rejects (see files/phase-9-presentation-demo.md
	// §4). Revisit only if the single-service design becomes a real
	// constraint (see PROJECT-AUDIT.md §3.13 / Fix Log).
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
	commerceMux.HandleFunc(
		"/products",
		func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost:
				catalogHandler.CreateProduct(w, r)

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
			switch r.Method {
			case http.MethodGet:
				catalogHandler.GetProduct(w, r)

			case http.MethodPatch:
				catalogHandler.UpdateProduct(w, r)

			case http.MethodDelete:
				catalogHandler.DeleteProduct(w, r)

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
			case r.Method == http.MethodPost &&
				strings.HasSuffix(r.URL.Path, "/payment/verify"):
				paymentHandler.VerifyPayment(w, r)

			case r.Method == http.MethodPost &&
				strings.HasSuffix(r.URL.Path, "/payment"):
				paymentHandler.CreatePaymentOrder(w, r)

			case r.Method == http.MethodPost &&
				strings.HasSuffix(r.URL.Path, "/recovery/remove-item"):
				paymentHandler.RemoveItemAndRecheckout(w, r)

			case r.Method == http.MethodGet &&
				strings.HasSuffix(r.URL.Path, "/recovery"):
				paymentHandler.Recovery(w, r)

			default:
				http.Error(
					w,
					"method not allowed",
					http.StatusMethodNotAllowed,
				)
			}
		},
	)

	commerceMux.HandleFunc(
		"/adapter/calls",
		paymentHandler.CallCount,
	)

	commerceMux.HandleFunc(
		"/webhooks/razorpay",
		webhookHandler.HandleRazorpay,
	)

	// Phase 3: policy routes
	commerceMux.HandleFunc(
		"/policy/mandates",
		policyHandler.CreateMandate,
	)

	commerceMux.HandleFunc(
		"/policy/propose",
		policyHandler.Propose,
	)

	// Level 2/3 durable human-approval requests.
	commerceMux.HandleFunc("/approval-requests", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			policyHandler.ListApprovalRequests(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})

	commerceMux.HandleFunc(
		"/approval-requests/",
		func(w http.ResponseWriter, r *http.Request) {
			path := strings.TrimPrefix(r.URL.Path, "/approval-requests/")
			path = strings.Trim(path, "/")
			switch {
			case path == "":
				// GET /approval-requests?status=... → list
				if r.Method == http.MethodGet {
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
		},
	)

	commerceMux.HandleFunc(
		"/audit/verify",
		policyHandler.VerifyAuditChain,
	)

	// Phase 8: safety / red-team.
	commerceMux.HandleFunc("/safety/attacks", safetyHandler.ListAttacks)
	commerceMux.HandleFunc("/safety/evaluations", safetyHandler.ListEvaluations)
	commerceMux.HandleFunc("/safety/evaluations/run", safetyHandler.RunSuite)
	commerceMux.HandleFunc("/safety/evaluations/", safetyHandler.GetEvaluation)
	commerceMux.HandleFunc("/safety/attacks/", safetyHandler.RunAttack)

	// Replay: reconstructed agent runs.
	commerceMux.HandleFunc("/runs", policyHandler.HandleListRuns)
	commerceMux.HandleFunc("/runs/", policyHandler.HandleGetRun)

	// Phase 4: agent contract (produces proposals only)
	commerceMux.HandleFunc(
		"/agent/checkout",
		agentHandler.PlanCheckout,
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
	growthSuggestHandler := growth.NewSuggestHandler(catalogRepo, cartService, growthAgent)

	commerceMux.HandleFunc(
		"/growth/suggest",
		growthSuggestHandler.Suggest,
	)

	// Phase 6: dashboard
	commerceMux.HandleFunc(
		"/dashboard/overview",
		analyticsHandler.Overview,
	)

	commerceMux.HandleFunc(
		"/dashboard/metrics",
		analyticsHandler.Metrics,
	)

	commerceMux.HandleFunc(
		"/dashboard/experiment",
		analyticsHandler.Experiment,
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
			corsMiddleware(commerceMux),
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
