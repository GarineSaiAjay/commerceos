// Shared types for the checkout flow (item 21 split,
// PLAN-04-UI-UX-AND-LATENCY.md §A2) -- previously all declared inline in
// checkout.tsx. Moved verbatim; see ../checkout.tsx (CheckoutFlow) and the
// sibling component files in this directory for how they're used.

// ProductVariant mirrors backend/commerce/catalog/product.go's
// ProductVariant JSON shape exactly (PLAN-02-CATALOG-AND-COMMERCE.md
// §1, item 10) -- every product now always has at least its own
// "<id>-default" entry (CreateProduct provisions it transactionally),
// so Product.variants below is never empty for a real product even
// when it has no color/length/tier differentiation.
export interface ProductVariant {
  variant_id: string;
  product_id: string;
  sku: string;
  price: { amount: number; currency: string };
  availability: number;
  attributes: Record<string, unknown>;
}

export interface Product {
  product_id: string;
  title: string;
  price: { amount: number; currency: string };
  availability: number;
  average_rating?: number;
  review_count?: number;
  // attributes is the parent product's own attribute bag -- used only
  // as a label fallback for a "-default" variant that carries no
  // label-bearing attribute of its own (see variantLabel in
  // ./helpers.ts), e.g. AppleCare's implicit "2-year" or the cable's
  // implicit "1m".
  attributes?: Record<string, unknown>;
  variants?: ProductVariant[];
  // features/compatibility/use_cases/return_policy/shipping (item 19,
  // PLAN-02-CATALOG-AND-COMMERCE.md §4) are all present in every real
  // API response (catalog.Product never omits them) but were never
  // rendered anywhere until the product-detail expand in ProductList.tsx
  // -- optional here only so a partial Product built from an
  // agent/suggestion payload (see CheckoutFlow's chooseAlternative/
  // acceptSuggestion synthetic `matched` fallback) still type-checks
  // without them.
  features?: string[];
  compatibility?: string[];
  use_cases?: string[];
  return_policy?: { days: number };
  shipping?: { estimated_days: number };
}

export interface CartItem {
  product_id: string;
  variant_id: string;
  title: string;
  quantity: number;
  unit_price: number;
  total: number;
}

export interface Cart {
  cart_id: string;
  items: CartItem[];
  subtotal: number;
  currency: string;
}

export interface Order {
  order_id: string;
  cart_id: string;
  status: string;
  subtotal: number;
  currency: string;
  items: CartItem[];
  created_at?: string;
}

// ReviewEntry is per-line-item, client-only state for the post-checkout
// "Rate this order" prompt (PLAN-02-CATALOG-AND-COMMERCE.md §2) --
// keyed by product_id in CheckoutFlow's `reviews` state, one entry per
// item in the just-completed order.
export interface ReviewEntry {
  rating: number;
  comment: string;
  submitting: boolean;
  submitted: boolean;
  error: string;
}

// Review mirrors backend/commerce/review's Review JSON shape exactly --
// one individual review left against a product, always tied to the
// order it was submitted from (VerifiedPurchase is computed
// server-side from that link; every review submitted through the API
// is a verified purchase by construction, see review.Service.Submit).
// Fetched by GET /products/{id}/reviews for the product-detail expand
// panel's review list (item 13, PLAN-02-CATALOG-AND-COMMERCE.md §4:
// "features as plain tags, return policy, shipping estimate, and --
// once §2 ships -- reviews").
export interface Review {
  id: number;
  product_id: string;
  order_id?: string;
  buyer_reference: string;
  rating: number;
  comment?: string;
  verified_purchase: boolean;
  created_at: string;
}

export interface Payment {
  payment_id: string;
  order_id: string;
  provider_order_id: string;
  amount: number;
  currency: string;
  status: string;
  key_id: string;
}

export interface Recovery {
  order_id: string;
  payment_status: string;
  attempt_status: string;
  error_code: string;
  error_description: string;
  safe_message: string;
  reservation_expires_at: string;
  retry_allowed: boolean;
  cart: { subtotal: number; currency: string; items: { product_id: string; variant_id: string; title: string; quantity: number; unit_price: number; total: number }[] };
  removable_items: string[];
}

export interface Mandate {
  mandate_id: string;
}

export interface Decision {
  decision: string;
  authorization_id: string;
  approval_request_id: string;
  reason: string;
  level: number;
  action_id?: string;
}

// RunStep/Run mirror backend/policy/replay.go's GET /runs/{id} response --
// the audit trail (proposed -> risk-assessed -> policy-evaluated ->
// authorized) for the exact action this checkout ran. See
// files/AUTH.md.
//
// NOTE (item 21 split): this interface was declared TWICE, identically,
// in the pre-split checkout.tsx (a leftover duplicate from item 16's
// audit-trail work) -- collapsed to the single declaration below, since
// two `export interface RunStep` in one module would now be a hard TS
// error anyway. No shape change.
export interface RunStep {
  stage: string;
  detail: string;
  timestamp: string;
}

export interface Run {
  run_id: string;
  action: string;
  amount: number;
  currency: string;
  decision: string;
  reason?: string;
  failed_check?: string;
  authorization_id?: string;
  authorization_status?: string;
  created_at: string;
  steps?: RunStep[];
}

export interface ApprovalRequestDetail {
  approval_request_id: string;
  mandate_id: string;
  action: string;
  amount: number;
  currency: string;
  merchant: string;
  items: string[];
  cart_id: string;
  policy_version: string;
  risk_score: number;
  level: number;
  status: string;
  authorization_id: string;
  reason: string;
}

export interface Intent {
  budget: number;
  category: string;
  priority: string;
  recipient: string;
  clarify?: string;
}

export interface AlternativeProduct {
  product_id: string;
  title: string;
  price: number;
  currency: string;
}

export interface CheckoutPlan {
  intent: Intent;
  proposal: {
    action: string;
    amount: number;
    currency: string;
    merchant: string;
    items: string[];
  };
  selected_product_id: string;
  reasoning: string;
  alternatives?: AlternativeProduct[];
  reasoning_trail?: RunStep[];
}

// One turn of the buyer <-> agent transcript, mirrored client-side for
// display only -- the actual conversation memory that makes a follow-up
// like "no, for my brother instead" work lives server-side, keyed by
// cart_id (see backend/agents/conversation.go), so it survives even if
// this client-side list is empty after a reload.
export interface AgentChatMessage {
  role: "user" | "assistant";
  content: string;
}

export interface SuggestedProduct {
  product_id: string;
  title: string;
  price: number;
  currency: string;
}

export interface SuggestionDetail {
  expected_value: number;
  reason: string;
}

export interface SuggestResponse {
  available: boolean;
  recommendation?: SuggestionDetail;
  product?: SuggestedProduct;
  message?: string;
}

// SubstituteItem/RejectionRecoverySuggestion mirror
// backend/agents/rejection_recovery.go's SubstituteItem/
// RejectionRecoverySuggestion JSON shapes exactly (item 33,
// PLAN-01-AGENTIC-CORE.md §6's second proactive agent turn: a
// policy rejection over the platform amount ceiling proactively
// searches for a cheaper substitute instead of only offering the
// buyer a manual "remove an item" list).
export interface SubstituteItem {
  product_id: string;
  variant_id?: string;
  title: string;
  price: number;
}

export interface RejectionRecoverySuggestion {
  available: boolean;
  reason?: string;
  replaced_item?: SubstituteItem;
  substitute?: SubstituteItem;
  new_subtotal?: number;
  reasoning?: string;
}

export type Step = "catalog" | "cart" | "checkout" | "approval" | "gate" | "pay" | "complete" | "failed" | "policy_rejected" | "orders";

export type SortOption = "default" | "price_asc" | "price_desc" | "rating" | "availability";

// Guided demo walkthrough milestones (item 38, P3,
// PLAN-06-ADDITIONAL-OPPORTUNITIES.md §4): each flag latches true the
// first time CheckoutFlow observes the corresponding real event happen
// -- see the markDemoMilestone call sites and the useEffects that
// watch `suggestion`/`step`/`run` in checkout.tsx -- and is never
// cleared on its own (only an explicit "restart walkthrough" resets
// the whole object back to all-false). DemoGuide.tsx only ever reads
// this, it never derives its own notion of progress.
//
// attemptedOverBudget and sawGracefulRejection latch together (both
// flip true the instant `step` becomes "policy_rejected") rather than
// independently: in this UI a buyer only ever discovers they've gone
// over budget BY seeing the rejection screen -- there's no separate
// "you're about to exceed it" warning today, so treating these as two
// independently-observable moments would claim a UI signal that
// doesn't exist.
export interface DemoMilestones {
  askedAgent: boolean;
  acceptedProposal: boolean;
  sawCrossSell: boolean;
  attemptedOverBudget: boolean;
  sawGracefulRejection: boolean;
  openedAuditTrail: boolean;
}
