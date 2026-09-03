package main

import (
	"compress/gzip"
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
	"github.com/garinesaiajay/commerceos/commerce/payment/x402"
	"github.com/garinesaiajay/commerceos/commerce/review"
	"github.com/garinesaiajay/commerceos/events"
	"github.com/garinesaiajay/commerceos/growth"
	db "github.com/garinesaiajay/commerceos/infra/db"
	"github.com/garinesaiajay/commerceos/mcp"
	"github.com/garinesaiajay/commerceos/policy"
	"github.com/garinesaiajay/commerceos/ratelimit"
	"github.com/garinesaiajay/commerceos/safety"
	"github.com/garinesaiajay/commerceos/tools"
	"github.com/garinesaiajay/commerceos/trust"
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

// compressionMiddleware gzip-compresses a response when the client
// advertises support for it (Accept-Encoding: gzip) -- item 31 (P2,
// PLAN-04-UI-UX-AND-LATENCY.md section B5): "Confirm (and if missing,
// add) gzip/br response compression on the Go backend for GET
// /products and dashboard JSON endpoints -- net/http doesn't compress
// by default... Content-Length-sensitive endpoints (webhooks, payment)
// should be excluded... apply the middleware selectively." Brotli
// isn't in the Go standard library (would need a third-party
// dependency this environment has no Go toolchain to vet); gzip is,
// via compress/gzip, and every browser this app targets already sends
// "Accept-Encoding: gzip" by default, so this covers the actual win --
// smaller JSON payloads over the wire for the catalog list and
// dashboard endpoints the plan calls out by name -- with zero new
// dependencies.
//
// shouldSkipCompression excludes webhook and payment routes by the
// SAME suffix-matching the "/orders/" mux below already uses for those
// exact routes (POST /orders/{id}/payment, POST/GET .../payment,
// POST .../payment/verify) plus the one webhook route -- a payment
// provider's callback response, or a payment record a client is about
// to run signature/amount verification against, should reach the
// caller byte-for-byte untouched, never re-encoded.
func shouldSkipCompression(path string) bool {
	return path == "/webhooks/razorpay" ||
		strings.HasSuffix(path, "/payment") ||
		strings.HasSuffix(path, "/payment/verify")
}

func compressionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if shouldSkipCompression(r.URL.Path) || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		// Whatever Content-Length a downstream handler might set would
		// describe the UNCOMPRESSED body -- wrong the instant gzip.Writer
		// rewrites the bytes actually sent on the wire. No handler in
		// this codebase sets it manually today (every JSON response goes
		// through json.NewEncoder(w).Encode or an io.Writer like
		// encoding/csv's Writer), but this middleware wraps every
		// response on the mux, so it can't assume that stays true
		// forever -- deleting it here just means net/http falls back to
		// chunked transfer encoding, which is correct either way.
		w.Header().Del("Content-Length")

		gz := gzip.NewWriter(w)
		defer gz.Close()

		next.ServeHTTP(&gzipResponseWriter{ResponseWriter: w, Writer: gz}, r)
	})
}

// gzipResponseWriter overrides only Write, so every downstream handler
// keeps calling the plain http.ResponseWriter methods it already does
// everywhere in this codebase (Header().Set(...), WriteHeader(status),
// Write(...) via json.NewEncoder or encoding/csv) completely unchanged
// -- Header() and WriteHeader() pass straight through via the embedded
// http.ResponseWriter, only the actual body bytes are routed through
// gzip.Writer instead of the connection directly.
type gzipResponseWriter struct {
	http.ResponseWriter
	Writer *gzip.Writer
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
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
	// item 23 (PLAN-04-UI-UX-AND-LATENCY.md §B2): an 8s Redis cache of
	// GET /products, within the plan's own 5-10s recommended range --
	// short enough that a merchant's own catalog edit (already forced
	// fresh via WithCache's invalidation on every mutation) is never
	// the only thing standing between a stale read and a correct one.
	// Reuses the same redisClient already connected above for the
	// Streams event bus -- Redis was provisioned but, until this item,
	// used for nothing else.
	catalogService := catalog.NewService(catalogRepo).
		WithCache(catalog.NewRedisProductsCache(redisClient, 8*time.Second))
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
	//
	// costGuard is the "max-cost/day guard if the OpenRouter key is
	// metered" PLAN-01-AGENTIC-CORE.md §2 calls for -- one shared
	// instance wired onto BOTH llmExtractor below and toolLoopAgent
	// further down, since they share the same OPENROUTER_API_KEY. See
	// CostGuard's own doc comment for why a single guard is correct
	// here rather than one per call site. WithAuditWriter is chained on
	// later, once auditWriter exists (same "doesn't exist yet at this
	// point in the function" reason policyService.WithAuditWriter is
	// deferred below) -- the budget is still enforced immediately
	// either way, it just has nowhere durable to log a trip until then.
	costGuard := agents.NewCostGuardFromEnv()

	llmExtractor := agents.NewLLMExtractorFromEnv().WithCostGuard(costGuard)
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

	// item 34 (P3, PLAN-06-ADDITIONAL-OPPORTUNITIES.md §5): both
	// LLM-backed endpoints (POST /agent/checkout below, and POST
	// /agent/loop -- item 18's bounded tool-calling loop) are
	// guest-accessible with an otherwise-unmetered path to a paid LLM
	// API -- "currently no rate limiting exists anywhere in the
	// codebase," per the plan's own audit. burst=10, refilling at 1
	// request/6s per caller IP thereafter: generous enough that a real
	// buyer clicking "Ask the shopping agent" a few times, or a judge
	// poking at either endpoint directly, never notices it, while a
	// script loop hammering either endpoint gets capped fast. See
	// ratelimit.Limiter's own doc comments for why this is
	// in-memory/per-process and per-IP rather than something more
	// sophisticated -- the plan's own framing for this item is "doesn't
	// need to be sophisticated."
	llmLimiter := ratelimit.NewLimiter(10, 1.0/6.0)
	go func() {
		// Sweep bounds llmLimiter's memory growth from the many distinct
		// caller IPs a public judging URL sees over hours/days -- Allow
		// itself never does this work inline. Ten minutes between sweeps,
		// evicting anything idle for 30+ minutes: cheap relative to how
		// small each bucket is, and nowhere near tight enough to evict a
		// bucket still actively being rate-limited.
		for range time.Tick(10 * time.Minute) {
			llmLimiter.Sweep(30 * time.Minute)
		}
	}()

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
	// fully unaffected. WithConversationStore chains the exact same
	// agentConversationStore instance buyerAgent already uses above,
	// giving RunInConversation (PLAN-01-AGENTIC-CORE.md §3) the same
	// cart-scoped memory buyerAgent's PlanCheckoutInConversation gets --
	// nil-receiver-safe, so this is a no-op when
	// NewToolCallingAgentFromEnv itself returned nil.
	toolLoopAgent := agents.NewToolCallingAgentFromEnv(tools.Dependencies{
		Catalog: catalogService,
		Cart:    cartService,
		Growth:  growthAgent,
	}).WithConversationStore(agentConversationStore).WithCostGuard(costGuard)
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
	// Item 25 (P2, PLAN-05-SELLER-DASHBOARD.md §4): the policy config is
	// now persisted (policy_settings table, seeded by db/seeds/
	// 004_policy_settings.sql with these exact same values) instead of
	// only ever being this Go literal. Loaded once at startup here;
	// policyEngine.Config() is the live source of truth from this point
	// on -- policyService.UpdatePolicyConfig keeps the DB row and the
	// engine's in-memory copy in sync on every operator edit (see
	// policy/service.go). Falling back to policy.DefaultConfig() on any
	// load error (a fresh database before the seed has run, or a
	// transient DB problem) is a deliberate "fail open to known-good
	// defaults" choice for server BOOTSTRAP specifically -- a different
	// risk profile from Engine's own checks, which fail closed when
	// evaluating one live proposal. A server that refused to start
	// because a settings row was momentarily unreadable would be a
	// worse outcome than starting with the same ceiling this app has
	// shipped with the whole time.
	policyConfig := policy.DefaultConfig()
	if loaded, err := policyRepo.GetConfig(ctx); err == nil {
		policyConfig = loaded
	} else if !errors.Is(err, policy.ErrPolicyConfigNotFound) {
		fmt.Printf("[policy] could not load persisted policy config, falling back to defaults: %v\n", err)
	}
	policyEngine := policy.NewEngine(policyConfig, policyRepo)
	riskEngine := policy.NewRiskEngine()
	policyService := policy.NewService(policyEngine, riskEngine, policyRepo)

	// item 16: persist the buyer-facing agent's own reasoning trail
	// (BuyerAgent single-shot pipeline and ToolCallingAgent's bounded
	// tool-calling loop, item 18) as independently-retrievable Runs --
	// see agents.RunRecorder and policy.Service.SaveAgentPlan's doc
	// comments. Wired here, not near agentHandler's earlier
	// construction/WithLoopAgent call above, because policyService
	// doesn't exist yet at that point in this file.
	agentHandler.WithRunRecorder(policyService)

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
	// safetyStore is kept in its own variable (rather than passed inline
	// to safety.NewHandler the way it used to be) because item 36's
	// trustHandler below (backend/trust/handler.go) needs the exact same
	// *safety.Store to read/write evaluations through the public
	// /trust/run-suite endpoint -- one store instance, shared by both the
	// gated /safety/* handler and the public /trust/* handler, not two
	// separate ones racing to write the same table.
	safetyStore := safety.NewStore(dbPool)
	safetyRunner := safety.NewRunner(policyService, razorpayAdapter)
	safetyHandler := safety.NewHandler(safetyRunner, safetyStore)

	// item 36 (P3, PLAN-06-ADDITIONAL-OPPORTUNITIES.md §3): the public,
	// judge-friendly counterpart to /audit/verify and /safety/evaluations/run
	// above -- same auditVerifier, same razorpayAdapter call counter, same
	// safetyRunner/safetyStore, wrapped by trust.Handler instead of
	// authService.RequireOperator. See backend/trust/handler.go's package
	// doc comment for why this is deliberately unauthenticated.
	trustHandler := trust.NewHandler(auditVerifier, razorpayAdapter, safetyRunner, safetyStore)

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

	// item 33, PLAN-01-AGENTIC-CORE.md §6 / ROADMAP-PRIORITIZED.md P2:
	// proactive policy-rejection recovery -- reuses the same
	// orderRepo/catalogService/cartService/orderService instances
	// already wired above for paymentHandler's own recovery actions,
	// plus a live read of policyEngine.Config().Ceiling (not the
	// policyConfig startup snapshot) so this can never enforce a
	// ceiling that has drifted from what the policy engine is actually
	// checking -- item 25 (P2, PLAN-05-SELLER-DASHBOARD.md §4) made the
	// ceiling operator-editable at runtime, which is exactly what would
	// otherwise go stale here.
	rejectionRecoveryHandler := agents.NewRejectionRecoveryHandler(
		orderRepo,
		catalogService,
		cartService,
		orderService,
		func() int64 { return policyEngine.Config().Ceiling },
	)

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
			// policyEngine.Config().Ceiling, not policyConfig.Ceiling: the
			// latter is a one-time snapshot taken at startup, which item 25
			// (P2, PLAN-05-SELLER-DASHBOARD.md §4) made stale the moment the
			// ceiling became operator-editable at runtime via
			// /dashboard/settings/policy.
			return policy.ExplainRejection(check, a, m, policyEngine.Config().Ceiling)
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

	// item 25 (P2, PLAN-05-SELLER-DASHBOARD.md §4): a policy settings
	// change is significant enough (it changes what checkout enforces
	// for every future proposal) to belong on the same general audit
	// ledger campaign/order/webhook events already use, not just the
	// policy package's own policy_evaluations trail (which only records
	// per-proposal decisions, never config changes). Wired here, not at
	// policyService's own construction above, because auditWriter
	// doesn't exist yet at that point in this function.
	policyService = policyService.WithAuditWriter(auditWriter)

	// costGuard was constructed (and already enforcing its budget)
	// before auditWriter existed -- see its construction above. Wiring
	// the audit writer in now just gives a trip somewhere durable to
	// log; nil-receiver-safe like every other WithX here, so this is a
	// no-op if OPENROUTER_API_KEY was never set and costGuard ended up
	// unused by anything.
	costGuard = costGuard.WithAuditWriter(auditWriter)

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
		events.DefaultStream,
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
		events.DefaultStream,
		"commerceos-group",
	)

	go func() {
		fmt.Println("Stream Consumer started")

		if err := streamConsumer.Run(ctx); err != nil {
			fmt.Printf("Stream Consumer stopped: %v\n", err)
		}
	}()

	// item 42 (P3, PLAN-06-ADDITIONAL-OPPORTUNITIES.md §4 /
	// PLAN-03-PROACTIVE-GROWTH-AGENT.md §7): cartHandler now actually
	// publishes "cart.item_added" (it compiled and worked fine without
	// this -- WithEventPublisher is an optional capability, same WithX
	// convention as WithCallCounter/WithAuditWriter elsewhere in this
	// codebase); the consumer below is a second, real consumer group on
	// the same stream (see growth.CartEventConsumer's own doc comment
	// for why this is separate from, not a replacement for, the
	// placeholder logger above) that precomputes cross-sell suggestions
	// off that event instead of only on demand.
	cartHandler = cartHandler.WithEventPublisher(eventBus, events.DefaultStream)

	growthCartEventConsumer := growth.NewCartEventConsumer(
		redisClient,
		events.DefaultStream,
		"growth-suggestions-group",
		catalogRepo,
		cartService,
		growthAgent,
		growthStore,
	)

	go func() {
		fmt.Println("Growth Cart-Event Consumer started")

		if err := growthCartEventConsumer.Run(ctx); err != nil {
			fmt.Printf("Growth Cart-Event Consumer stopped: %v\n", err)
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

			// GET/POST /products/{id}/variants (PLAN-02-CATALOG-AND-
			// COMMERCE.md §5.2 / PLAN-05-SELLER-DASHBOARD.md §1's variant
			// sub-editor) -- same "checked first, suffix-matched" pattern
			// as /reviews above, and for the same reason: strings.HasSuffix
			// on a path that also matches the plain GET/PATCH/DELETE
			// {id} cases below would otherwise be shadowed by them.
			case r.Method == http.MethodGet &&
				strings.HasSuffix(r.URL.Path, "/variants"):
				catalogHandler.ListVariants(w, r)

			case r.Method == http.MethodPost &&
				strings.HasSuffix(r.URL.Path, "/variants"):
				authService.RequireOperator(catalogHandler.CreateVariant)(w, r)

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

	// /variants/{id}: GET stays open (same buyer/agent-readable
	// convention as GET /products), PATCH/DELETE are operator-gated
	// (PLAN-02-CATALOG-AND-COMMERCE.md §5.2 / PLAN-05-SELLER-
	// DASHBOARD.md §1) -- mirrors the /products/ switch above exactly.
	commerceMux.HandleFunc(
		"/variants/",
		func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				catalogHandler.GetVariant(w, r)

			case http.MethodPatch:
				authService.RequireOperator(catalogHandler.UpdateVariant)(w, r)

			case http.MethodDelete:
				authService.RequireOperator(catalogHandler.DeleteVariant)(w, r)

			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		},
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

			// item 33: proactive policy-rejection recovery. Checked
			// before the plain GET /recovery case below via the same
			// suffix-first ordering already used throughout this
			// switch (HasSuffix("/recovery") would not match either of
			// these longer suffixes anyway, but keeping the more
			// specific cases first matches the existing convention).
			case r.Method == http.MethodPost &&
				strings.HasSuffix(r.URL.Path, "/recovery/suggest-substitute"):
				rejectionRecoveryHandler.SuggestSubstitute(w, r)

			case r.Method == http.MethodPost &&
				strings.HasSuffix(r.URL.Path, "/recovery/replace-item"):
				rejectionRecoveryHandler.ReplaceItemAndRecheckout(w, r)

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

	// item 40 (P3, PLAN-05-SELLER-DASHBOARD.md §7): multi-operator
	// invites -- a second (or third, ...) operator per merchant, each
	// with their own login, on top of the single hardcoded operator
	// above. "/auth/invites/accept" is registered as its own exact route
	// (public -- the invitee has no account or token yet) so it is never
	// shadowed by, and never falls through to, the RequireOperator-gated
	// "/auth/invites/" prefix route below it -- same pattern as
	// "/campaigns/export" vs. "/campaigns/" elsewhere in this file.
	commerceMux.HandleFunc("/auth/invites/accept", authHandler.AcceptInvite)
	commerceMux.HandleFunc("/auth/invites", authService.RequireOperator(authHandler.Invites))
	commerceMux.HandleFunc("/auth/invites/", authService.RequireOperator(authHandler.InviteByID))
	commerceMux.HandleFunc("/auth/operators", authService.RequireOperator(authHandler.Operators))
	commerceMux.HandleFunc("/auth/operators/", authService.RequireOperator(authHandler.OperatorByID))

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
	// item 27 (P2, PLAN-05-SELLER-DASHBOARD.md section 6): CSV export of
	// campaigns -- registered here as its own exact route, exactly like
	// "/campaigns/propose" immediately above, so it's never shadowed by
	// or confused with the "/campaigns/" {id}-based subtree registered
	// below (see ExportCSV's own doc comment in campaign/handler.go).
	commerceMux.HandleFunc("/campaigns/export", authService.RequireOperator(campaignHandler.ExportCSV))
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

	// item 36 (P3, PLAN-06-ADDITIONAL-OPPORTUNITIES.md §3): public
	// /trust/* -- deliberately NOT wrapped in authService.RequireOperator,
	// unlike every /safety/* route directly above. The whole point is that
	// a judge (or a judge's own tooling) with no operator credentials can
	// see the audit-chain integrity status, the live Razorpay call
	// counter, and trigger the same 14-attack suite the gated dashboard
	// runs, without ever logging in. See trust.Handler's own doc comments
	// for why this is safe to expose (no new evidence, no new capability)
	// and how RunSuite's write is bounded (a 10s shared cooldown, not a
	// full rate limiter).
	commerceMux.HandleFunc("/trust/summary", trustHandler.Summary)
	commerceMux.HandleFunc("/trust/run-suite", trustHandler.RunSuite)

	// Replay: reconstructed agent runs. Listing every run is merchant-only;
	// fetching a single run by its own ID stays reachable without login so
	// checkout.tsx can show the buyer their own audit trail inline (P0.4).
	commerceMux.HandleFunc("/runs", authService.RequireOperator(policyHandler.HandleListRuns))
	commerceMux.HandleFunc("/runs/", policyHandler.HandleGetRun)

	// Phase 4: agent contract (produces proposals only). Both routes
	// below go through llmLimiter (item 34, constructed above) -- see
	// its own comment for why.
	commerceMux.Handle(
		"/agent/checkout",
		llmLimiter.Middleware(ratelimit.ClientIP, http.HandlerFunc(agentHandler.PlanCheckout)),
	)

	// Item 18: bounded tool-calling agent loop -- a second, genuinely
	// multi-step agentic path alongside the fixed single-shot one above.
	// See tool_loop.go's doc comment for why it can never reach a
	// money-moving tool.
	commerceMux.Handle(
		"/agent/loop",
		llmLimiter.Middleware(ratelimit.ClientIP, http.HandlerFunc(agentHandler.PlanCheckoutLoop)),
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

	// item 25 (P2, PLAN-05-SELLER-DASHBOARD.md §4): view/edit the
	// policy engine's live configuration. One route, two methods (same
	// dispatch-by-method convention as the /orders/ and /products/
	// muxes elsewhere in this file) rather than two separate paths --
	// GET and PATCH on the same resource is the more RESTful shape here
	// and there's no suffix ambiguity to resolve, unlike those.
	commerceMux.HandleFunc(
		"/dashboard/settings/policy",
		authService.RequireOperator(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				policyHandler.GetSettings(w, r)
			case http.MethodPatch:
				policyHandler.UpdateSettings(w, r)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		}),
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

	// item 27 (P2, PLAN-05-SELLER-DASHBOARD.md section 6): CSV export of
	// orders, same operator scoping as the list route directly above --
	// registered as its own path rather than a query parameter
	// (?format=csv) on /dashboard/orders so the browser's native
	// download handling (Content-Disposition: attachment) applies
	// cleanly to a plain GET.
	commerceMux.HandleFunc(
		"/dashboard/orders/export",
		authService.RequireOperator(orderHandler.ExportOrdersCSV),
	)

	// Phase 7: MCP endpoint
	commerceMux.Handle(
		"/mcp",
		mcpHandler,
	)

	// item 35 (P3, PLAN-06-ADDITIONAL-OPPORTUNITIES.md §2): agent-readable
	// catalog manifest. Deliberately NOT wrapped in authService.RequireOperator
	// like every /dashboard/* route above -- this is meant to be fetched by an
	// external agent or a judge's own tooling that holds no operator
	// credentials, before it ever calls anything else. configFn reads
	// policyEngine.Config() live (not the policyConfig startup snapshot) for
	// the same reason the request_authorization Explain closure above does:
	// item 25 made the ceiling operator-editable at runtime, and a manifest
	// that cached the startup value would go stale the moment an operator
	// changed it.
	commerceMux.HandleFunc(
		"/.well-known/agent-commerce.json",
		mcp.ManifestHandler(mcpServer, func() policy.PolicyConfig { return policyEngine.Config() }),
	)

	// item 39 (P3, PLAN-06-ADDITIONAL-OPPORTUNITIES.md §1): a minimal,
	// test-mode-only x402 payment-rail stub -- "one code path, one
	// demo scenario, not a general x402 client." See
	// commerce/payment/x402's own package doc comment for the full
	// scope and honesty notes on wire-format fidelity. Deliberately
	// standalone from the real checkout/policy/audit pipeline every
	// other route above goes through -- paying this demo resource
	// creates no order, consumes no mandate, and writes nothing to the
	// audit chain.
	//
	// x402DemoSecret is not a real credential -- see
	// x402.TestModeFacilitator's own doc comment for why treating it
	// as sensitive would misrepresent what a test-mode-only stub is
	// for. Overridable so a deployed judging URL doesn't ship with the
	// same well-known default every clone of this repo has.
	x402DemoSecret := os.Getenv("X402_DEMO_SECRET")
	if x402DemoSecret == "" {
		x402DemoSecret = "x402-test-mode-demo-secret"
	}

	x402Handler := x402.NewHandler(
		x402.NewTestModeFacilitator(x402DemoSecret),
		x402.DemoRequirements(),
		x402.DemoResource,
	)

	commerceMux.Handle("/x402/priority-support", x402Handler)

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

		// compressionMiddleware sits INSIDE corsMiddleware (closer to
		// commerceMux): an OPTIONS preflight request is intercepted and
		// answered by corsMiddleware itself (see its own body above)
		// before ever reaching compressionMiddleware, so a 204 preflight
		// response is never pointlessly gzip-wrapped -- only real
		// responses from commerceMux's handlers are.
		if err := http.ListenAndServe(
			":"+commercePort,
			corsMiddleware(frontendOrigin, compressionMiddleware(commerceMux)),
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
