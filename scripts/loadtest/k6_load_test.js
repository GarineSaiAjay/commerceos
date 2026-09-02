// Load/chaos readiness pass for judging day (item 41, P3,
// PLAN-06-ADDITIONAL-OPPORTUNITIES.md §5). §5 lists three things:
//   1. "A basic load test script (k6 or plain Go, hitting /products,
//      /agent/checkout, /growth/suggest concurrently) to find the
//      actual breaking point before a judge does." -- this file.
//   2. "Confirm connection pool sizing in backend/infra/db/postgres.go
//      is set explicitly rather than left at pgx defaults" -- already
//      true (config.MaxConns = 10, config.MinConns = 2,
//      config.MaxConnLifetime = time.Hour, all explicit) before this
//      item touched anything. See README.md in this directory for the
//      full finding -- nothing to fix there, only to confirm and run
//      this script against.
//   3. "Rate-limit the two LLM-backed endpoints" -- already done as
//      item 34 (backend/ratelimit/). This script's agent_checkout
//      scenario deliberately runs AT that limiter, not around it --
//      see the scenario's own comment below for why a wall of 429s
//      there is the expected, correct outcome, not a bug this script
//      found.
//
// k6, not "plain Go" (the plan explicitly allows either): a load
// generator has no reason to compile into this project's own Go
// module (backend/go.mod) or be picked up by `go build ./...` --
// keeping it a standalone script with its own tool (k6) means it can
// never be the thing that breaks the backend build, and k6 gives
// concurrent-VU scenarios, thresholds, and percentile latency
// reporting out of the box instead of hand-rolling them.
//
// Run: see README.md in this directory for prerequisites (k6 install,
// a running stack, seed data) and how to read the results.
import http from "k6/http";
import { check, sleep } from "k6";
import { Rate, Trend } from "k6/metrics";

// commerceMux -- every route this script hits (products, carts,
// agent/checkout, growth/suggest) -- listens on :8081, not :8080.
// backend/cmd/server/main.go's own comment on apiGatewayMux/
// commerceMux/agentAPIMux/dashboardMux explains why: four ports are
// still exposed (Phase 1's four-service split), but every real route
// lives on commerceMux/:8081 today.
const BASE_URL = __ENV.BASE_URL || "http://localhost:8081";

// db/seeds/001_catalog.sql's known-good merchant/product/variant --
// the same ones frontend/app/checkout/helpers.ts's MERCHANT_ID and
// this repo's own cart tests (commerce/cart/handler_test.go's
// fakeVariantReader) already key off, so this script exercises real
// seeded data rather than IDs that only work by coincidence.
const MERCHANT_ID = "merchant_001";
const VARIANT_ID = "airpods-pro-2-default";

// A handful of realistic prompts (same flavor as files/demo-script.md
// and the guided-demo walkthrough, item 38) -- varied so the LLM/
// deterministic extractor isn't scoring the exact same string on every
// request, closer to real traffic than one hardcoded prompt.
const AGENT_PROMPTS = [
  "Find me AirPods Pro for my brother.",
  "I need wireless earbuds under 30000 rupees.",
  "Show me a charging accessory for my AirPods.",
  "I want the best noise cancelling headphones you have.",
  "Get me AppleCare for my AirPods Pro.",
];

// Custom metrics -- see the README's "Reading the results" section for
// what "good" looks like on each. k6's built-in http_req_failed only
// tracks network-level failures (status 0) unless a response callback
// is set; these Rate metrics instead encode this script's own,
// endpoint-specific idea of "acceptable" (e.g. 429 is a PASS on
// agent_checkout, not a failure) so the thresholds below mean what
// their names say.
const productsOk = new Rate("products_ok");
const productsDuration = new Trend("products_duration_ms", true);

const agentCheckoutAcceptable = new Rate("agent_checkout_acceptable"); // 200 or 429
const agentCheckoutServerError = new Rate("agent_checkout_server_error"); // 5xx only
const agentCheckoutDuration = new Trend("agent_checkout_duration_ms", true);

const growthSuggestOk = new Rate("growth_suggest_ok");
const growthSuggestDuration = new Trend("growth_suggest_duration_ms", true);

// Overridable via e.g. `k6 run -e VUS=30 -e DURATION=2m
// scripts/loadtest/k6_load_test.js` -- see the README for suggested
// values to escalate through to actually find a breaking point,
// rather than running this once at the default and calling it done.
const VUS = Number(__ENV.VUS || 10);
const DURATION = __ENV.DURATION || "1m";

export const options = {
  scenarios: {
    // Read-heavy, unauthenticated, no rate limiting -- this is the
    // one endpoint every buyer hits on every page load, so it's the
    // most realistic "background load" of the three.
    products_read: {
      executor: "constant-vus",
      vus: VUS,
      duration: DURATION,
      exec: "productsRead",
    },
    // Deliberately run at the item-34 rate limiter, not around it.
    // llmLimiter is a 10-burst, 1-per-6-seconds *per client IP* token
    // bucket (backend/ratelimit/limiter.go), and every VU here shares
    // this test runner's one IP (or one X-Forwarded-For, if the load
    // is routed through a proxy that sets one) -- so with VUS above
    // roughly 1-2, expect the large majority of requests to come back
    // 429 almost immediately after the initial burst of 10, by
    // design. That is the limiter working, not this script finding a
    // bug -- see agentCheckoutServerError below for the metric that
    // actually matters here (5xx, which should stay at 0 regardless
    // of how many 429s the limiter hands out).
    agent_checkout: {
      executor: "constant-vus",
      vus: VUS,
      duration: DURATION,
      exec: "agentCheckout",
    },
    // Exercises the full real path: create a cart, add the seeded
    // item (which -- since item 42 -- also publishes cart.item_added
    // and triggers growth.CartEventConsumer's async precompute), then
    // ask for a suggestion. A fresh cart per iteration means most
    // iterations hit growth/suggest.go's real scoring path rather
    // than the item-20 frequency cap (2 suggestions per cart per 10
    // minutes) -- both are valid 200 responses either way.
    growth_suggest: {
      executor: "constant-vus",
      vus: VUS,
      duration: DURATION,
      exec: "growthSuggestFlow",
    },
  },
  thresholds: {
    // Read path must actually work under load -- this is the one
    // threshold this script fails the build on if it regresses.
    products_ok: ["rate>0.99"],
    products_duration_ms: ["p(95)<1000"],

    // 429s are expected and fine; 5xx (a crashed handler, an
    // exhausted DB pool surfacing as an error, a panic) are not.
    agent_checkout_server_error: ["rate==0"],

    growth_suggest_ok: ["rate>0.95"],
    growth_suggest_duration_ms: ["p(95)<2000"],
  },
};

function freshCartId(prefix) {
  // k6 has no crypto.randomUUID in the default runtime -- __VU/__ITER
  // plus Date.now() is unique enough for a load test's purposes
  // (uniqueness, not unguessability).
  return `${prefix}_${__VU}_${__ITER}_${Date.now()}`;
}

export function productsRead() {
  const res = http.get(`${BASE_URL}/products`, { tags: { name: "products" } });
  productsOk.add(res.status === 200);
  productsDuration.add(res.timings.duration);
  check(res, { "GET /products: 200": (r) => r.status === 200 });
  sleep(0.2);
}

export function agentCheckout() {
  const prompt = AGENT_PROMPTS[Math.floor(Math.random() * AGENT_PROMPTS.length)];
  const payload = JSON.stringify({
    prompt,
    merchant: MERCHANT_ID,
    cart_id: freshCartId("cart_loadtest_agent"),
  });

  const res = http.post(`${BASE_URL}/agent/checkout`, payload, {
    headers: { "Content-Type": "application/json" },
    tags: { name: "agent_checkout" },
  });

  const isServerError = res.status >= 500;
  const isAcceptable = res.status === 200 || res.status === 429;

  agentCheckoutServerError.add(isServerError);
  agentCheckoutAcceptable.add(isAcceptable);
  agentCheckoutDuration.add(res.timings.duration);

  check(res, {
    "POST /agent/checkout: 200 or 429 (429 = rate limiter, expected under load)":
      () => isAcceptable,
    "POST /agent/checkout: never 5xx": () => !isServerError,
  });

  sleep(0.5);
}

export function growthSuggestFlow() {
  const cartId = freshCartId("cart_loadtest_growth");

  const createRes = http.post(
    `${BASE_URL}/carts`,
    JSON.stringify({ cart_id: cartId, merchant_id: MERCHANT_ID, currency: "INR" }),
    { headers: { "Content-Type": "application/json" }, tags: { name: "carts_create" } },
  );
  if (createRes.status !== 201) {
    growthSuggestOk.add(false);
    return;
  }

  const addItemRes = http.post(
    `${BASE_URL}/carts/${cartId}/items`,
    JSON.stringify({ variant_id: VARIANT_ID, quantity: 1 }),
    { headers: { "Content-Type": "application/json" }, tags: { name: "carts_add_item" } },
  );
  if (addItemRes.status !== 204) {
    growthSuggestOk.add(false);
    return;
  }

  const suggestRes = http.post(
    `${BASE_URL}/growth/suggest`,
    JSON.stringify({ cart_id: cartId }),
    { headers: { "Content-Type": "application/json" }, tags: { name: "growth_suggest" } },
  );

  growthSuggestOk.add(suggestRes.status === 200);
  growthSuggestDuration.add(suggestRes.timings.duration);

  check(suggestRes, { "POST /growth/suggest: 200": (r) => r.status === 200 });

  sleep(0.3);
}
