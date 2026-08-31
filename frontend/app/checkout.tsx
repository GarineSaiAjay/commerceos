"use client";

import { useState, useEffect, useMemo } from "react";
import { API_BASE } from "../lib/api";
// Skeleton (item 22, PLAN-04-UI-UX-AND-LATENCY.md §A3): the dashboard's
// existing loading-state component, reused here rather than
// reinvented -- checkout.tsx previously had zero skeleton states, just
// bare "Loading..." text in a handful of places.
import { Skeleton } from "../lib/format";

declare global {
  interface Window {
    Razorpay: new (options: RazorpayOptions) => RazorpayInstance;
  }
}

interface RazorpayOptions {
  key: string;
  amount: number;
  currency: string;
  name: string;
  description: string;
  order_id: string;
  handler: (response: RazorpayResponse) => void;
  modal?: {
    ondismiss?: () => void;
  };
  prefill?: {
    name?: string;
    email?: string;
    contact?: string;
  };
  theme?: {
    color?: string;
  };
}

interface RazorpayResponse {
  razorpay_payment_id: string;
  razorpay_order_id: string;
  razorpay_signature: string;
}

interface RazorpayInstance {
  open: () => void;
}

// ProductVariant mirrors backend/commerce/catalog/product.go's
// ProductVariant JSON shape exactly (PLAN-02-CATALOG-AND-COMMERCE.md
// §1, item 10) -- every product now always has at least its own
// "<id>-default" entry (CreateProduct provisions it transactionally),
// so Product.variants below is never empty for a real product even
// when it has no color/length/tier differentiation.
interface ProductVariant {
  variant_id: string;
  product_id: string;
  sku: string;
  price: { amount: number; currency: string };
  availability: number;
  attributes: Record<string, unknown>;
}

interface Product {
  product_id: string;
  title: string;
  price: { amount: number; currency: string };
  availability: number;
  average_rating?: number;
  review_count?: number;
  // attributes is the parent product's own attribute bag -- used only
  // as a label fallback for a "-default" variant that carries no
  // label-bearing attribute of its own (see variantLabel below), e.g.
  // AppleCare's implicit "2-year" or the cable's implicit "1m".
  attributes?: Record<string, unknown>;
  variants?: ProductVariant[];
  // features/compatibility/use_cases/return_policy/shipping (item 19,
  // PLAN-02-CATALOG-AND-COMMERCE.md §4) are all present in every real
  // API response (catalog.Product never omits them) but were never
  // rendered anywhere until the product-detail expand below -- optional
  // here only so a partial Product built from an agent/suggestion
  // payload (see chooseAlternative/acceptSuggestion's synthetic
  // `matched` fallback) still type-checks without them.
  features?: string[];
  compatibility?: string[];
  use_cases?: string[];
  return_policy?: { days: number };
  shipping?: { estimated_days: number };
}

interface CartItem {
  product_id: string;
  variant_id: string;
  title: string;
  quantity: number;
  unit_price: number;
  total: number;
}

interface Cart {
  cart_id: string;
  items: CartItem[];
  subtotal: number;
  currency: string;
}

interface Order {
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
// keyed by product_id in the `reviews` state below, one entry per item
// in the just-completed order.
interface ReviewEntry {
  rating: number;
  comment: string;
  submitting: boolean;
  submitted: boolean;
  error: string;
}

interface Payment {
  payment_id: string;
  order_id: string;
  provider_order_id: string;
  amount: number;
  currency: string;
  status: string;
  key_id: string;
}

interface Recovery {
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

interface Mandate {
  mandate_id: string;
}

interface Decision {
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
interface RunStep {
  stage: string;
  detail: string;
  timestamp: string;
}

interface Run {
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

interface ApprovalRequestDetail {
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

interface Intent {
  budget: number;
  category: string;
  priority: string;
  recipient: string;
  clarify?: string;
}

interface AlternativeProduct {
  product_id: string;
  title: string;
  price: number;
  currency: string;
}

// RunStep mirrors backend/policy's RunStep JSON shape (stage/detail/
// timestamp) -- the same audit-trail entry type dashboard/runs/page.tsx
// already renders. reasoning_trail below carries it, but this page
// doesn't render it itself (item 16, PLAN-01-AGENTIC-CORE.md §4: "no
// new UI surface needed" -- a buyer wanting the full trail can already
// open the same run from the merchant dashboard).
interface RunStep {
  stage: string;
  detail: string;
  timestamp: string;
}

interface CheckoutPlan {
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
interface AgentChatMessage {
  role: "user" | "assistant";
  content: string;
}

interface SuggestedProduct {
  product_id: string;
  title: string;
  price: number;
  currency: string;
}

interface SuggestionDetail {
  expected_value: number;
  reason: string;
}

interface SuggestResponse {
  available: boolean;
  recommendation?: SuggestionDetail;
  product?: SuggestedProduct;
  message?: string;
}

const MERCHANT_ID = "merchant_001";

// Each order uses a fresh cart ID. A cart is single-use: once it is
// checked out it is marked `checked_out` and can never be reused, so a
// fixed ID would leave the UI stuck on a stale, already-checked-out cart.
function freshCartId() {
  return `cart_${Date.now()}`;
}

// Persisted so a hard reload restores whatever the buyer was still
// shopping (see the restore/persist effects below). Only ever holds an
// ACTIVE cart's ID -- GET /carts/{id} now 404s for a checked-out cart
// (backend/commerce/cart/service.go), so a stale ID from a completed
// order is never resurrected.
const CART_STORAGE_KEY = "commerceos_cart_id";

type Step = "catalog" | "cart" | "checkout" | "approval" | "gate" | "pay" | "complete" | "failed" | "policy_rejected" | "orders";

// variantLabel derives a short, human label for a variant picker option
// (item 10). It checks the variant's OWN attributes first (color,
// length_m, coverage_years -- the only label-bearing keys any seeded
// variant currently uses). A "-default" variant seeded before item 10
// existed carries none of these on itself (e.g. AppleCare's original
// 2-year row, the cable's original 1m row) -- rather than edit those
// already-seeded rows, this falls back to the same keys on the PARENT
// PRODUCT's own attributes, which already had them. "Standard" is the
// last resort for a product with no differentiating attributes at all.
function variantLabel(variant: ProductVariant, product: Product): string {
  const attrs = variant.attributes ?? {};
  const color = attrs["color"];
  if (typeof color === "string") return color[0].toUpperCase() + color.slice(1);

  const lengthM = attrs["length_m"];
  if (typeof lengthM === "number") return `${lengthM}m`;

  const coverageYears = attrs["coverage_years"];
  if (typeof coverageYears === "number") return `${coverageYears}-year`;

  if (variant.variant_id === `${product.product_id}-default`) {
    const productAttrs = product.attributes ?? {};
    const fallbackColor = productAttrs["color"];
    if (typeof fallbackColor === "string") return fallbackColor[0].toUpperCase() + fallbackColor.slice(1);

    const fallbackLength = productAttrs["length_m"];
    if (typeof fallbackLength === "number") return `${fallbackLength}m`;

    const fallbackCoverage = productAttrs["coverage_years"];
    if (typeof fallbackCoverage === "number") return `${fallbackCoverage}-year`;
  }

  return "Standard";
}

// defaultVariantFor picks which variant addToCart should use when the
// buyer hasn't picked one explicitly (the three agent/suggestion entry
// points below all go through this, not the picker). Prefers the exact
// "<id>-default" entry every product is guaranteed to have; falls back
// to whatever variant happens to be first if that's somehow missing,
// and finally to the old hardcoded-guess string so a product object
// built from a partial agent/suggestion payload (no variants array at
// all -- see chooseAlternative/acceptSuggestion's synthetic `matched`
// fallback objects) still resolves to something.
function defaultVariantFor(product: Product): string {
  const exact = product.variants?.find((v) => v.variant_id === `${product.product_id}-default`);
  if (exact) return exact.variant_id;
  if (product.variants && product.variants.length > 0) return product.variants[0].variant_id;
  return `${product.product_id}-default`;
}

export default function CheckoutFlow({
  initialProducts,
}: {
  initialProducts: Product[];
}) {
  const [step, setStep] = useState<Step>("catalog");
  const [products] = useState<Product[]>(initialProducts);
  const [searchQuery, setSearchQuery] = useState("");
  const [selectedCategory, setSelectedCategory] = useState<string | null>(null);
  type SortOption = "default" | "price_asc" | "price_desc" | "rating" | "availability";
  const [sortBy, setSortBy] = useState<SortOption>("default");

  const categories = useMemo(() => {
    const seen = new Set<string>();
    for (const product of products) {
      for (const tag of product.use_cases ?? []) seen.add(tag);
    }
    return Array.from(seen).sort();
  }, [products]);

  const filteredProducts = useMemo(() => {
    const query = searchQuery.trim().toLowerCase();
    const filtered = products.filter((product) => {
      if (selectedCategory && !(product.use_cases ?? []).includes(selectedCategory)) return false;
      if (!query) return true;
      const haystack = [product.title, ...(product.features ?? []), ...(product.use_cases ?? [])]
        .join(" ")
        .toLowerCase();
      return haystack.includes(query);
    });
    switch (sortBy) {
      case "price_asc":
        return [...filtered].sort((a, b) => a.price.amount - b.price.amount);
      case "price_desc":
        return [...filtered].sort((a, b) => b.price.amount - a.price.amount);
      case "rating":
        return [...filtered].sort((a, b) => (b.average_rating ?? 0) - (a.average_rating ?? 0));
      case "availability":
        return [...filtered].sort((a, b) => b.availability - a.availability);
      default:
        return filtered;
    }
  }, [products, searchQuery, selectedCategory, sortBy]);
  const [cartId, setCartId] = useState<string>(() => freshCartId());
  const [cart, setCart] = useState<Cart | null>(null);
  const [order, setOrder] = useState<Order | null>(null);
  const [payment, setPayment] = useState<Payment | null>(null);
  	const [approvalRequestId, setApprovalRequestId] = useState("");
  	const [approvalReason, setApprovalReason] = useState("");
  	// Set when the policy engine rejects a proposal outright (e.g. over
  	// the amount ceiling, or an item not on the merchant's permitted
  	// list) -- distinct from "failed" (a Razorpay payment attempt was
  	// made and declined): here, no payment was ever attempted at all.
  	const [policyRejectionReason, setPolicyRejectionReason] = useState("");
  	const [recovery, setRecovery] = useState<Recovery | null>(null);
  	const [approvalLevel, setApprovalLevel] = useState(0);
  	const [approvalSnapshot, setApprovalSnapshot] = useState<ApprovalRequestDetail | null>(null);
  	const [gateConfirmed, setGateConfirmed] = useState(false);
  	const [gateError, setGateError] = useState("");
  	const [loading, setLoading] = useState(false);
  	const [message, setMessage] = useState("");
  	const [agentPrompt, setAgentPrompt] = useState("");
  	const [agentLoading, setAgentLoading] = useState(false);
  	const [agentPlan, setAgentPlan] = useState<CheckoutPlan | null>(null);
  	const [agentError, setAgentError] = useState("");
  	const [agentHistory, setAgentHistory] = useState<AgentChatMessage[]>([]);
  	const [suggestion, setSuggestion] = useState<SuggestResponse | null>(null);
  	const [suggestionLoading, setSuggestionLoading] = useState(false);
  	const [dismissedProductId, setDismissedProductId] = useState<string | null>(null);
  	const [orders, setOrders] = useState<Order[]>([]);
  	const [ordersLoading, setOrdersLoading] = useState(false);
  	const [ordersError, setOrdersError] = useState("");
  	const [runId, setRunId] = useState("");
  	const [run, setRun] = useState<Run | null>(null);
  	const [runLoading, setRunLoading] = useState(false);
  const [reviews, setReviews] = useState<Record<string, ReviewEntry>>({});
  // Per-product picker selection (item 10) -- keyed by product_id so
  // each catalog row remembers its own choice independently. Only
  // populated by the buyer clicking an option; addToCart falls back to
  // defaultVariantFor(product) when a product has no entry here yet.
  const [selectedVariant, setSelectedVariant] = useState<Record<string, string>>({});

  // Product-detail expand (item 19, PLAN-03-PROACTIVE-GROWTH-AGENT.md
  // §3 / PLAN-02-CATALOG-AND-COMMERCE.md §4) -- at most one product's
  // detail panel is open at a time, so a single current-suggestion pair
  // is enough rather than a map keyed by product_id.
  const [expandedProductId, setExpandedProductId] = useState<string | null>(null);
  const [detailSuggestion, setDetailSuggestion] = useState<SuggestResponse | null>(null);
  const [detailSuggestionLoading, setDetailSuggestionLoading] = useState(false);

  // Post-checkout "complete the set" cross-sell (item 19, PLAN-03-
  // PROACTIVE-GROWTH-AGENT.md §4) -- scored once against the order that
  // was just placed, shown on the order-complete screen.
  const [postCheckoutSuggestion, setPostCheckoutSuggestion] = useState<SuggestResponse | null>(null);
  const [postCheckoutSuggestionLoading, setPostCheckoutSuggestionLoading] = useState(false);

  // Persist the active cart ID so a hard reload doesn't lose the cart
  // (P0.2: previously a fresh ID was minted on every mount).
  useEffect(() => {
    try {
      window.localStorage.setItem(CART_STORAGE_KEY, cartId);
    } catch {
      // localStorage unavailable (private browsing, etc.) -- shopping
      // still works for this tab, it just won't survive a reload.
    }
  }, [cartId]);

  // Restore a cart the buyer left mid-shop, once, on first mount. Must
  // read localStorage before the effect above has a chance to overwrite
  // it with the fresh cartId already in state -- effects run in the
  // order they're declared, so this being declared first is what makes
  // that ordering safe.
  useEffect(() => {
    const saved =
      typeof window !== "undefined" ? window.localStorage.getItem(CART_STORAGE_KEY) : null;
    if (!saved || saved === cartId) return;
    (async () => {
      try {
        const res = await fetch(`${API_BASE}/carts/${saved}`);
        if (!res.ok) return; // gone, expired, or already checked out -- keep the fresh cart
        const restored = (await res.json()) as Cart;
        setCartId(saved);
        setCart(restored);
        if (restored.items.length > 0) setStep("cart");
      } catch {
        // ignore -- keep shopping with the fresh cart already in state
      }
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // targetCartId lets a caller add to a cart OTHER than the one
  // currently in `cartId` state -- needed because setCartId(...) is
  // async (a React state update, not visible to code running later in
  // the same function via closure), so "start a fresh cart and
  // immediately add this item to it" (acceptPostCheckoutSuggestion)
  // cannot rely on the `cartId` closure alone. Every existing caller
  // omits it and gets the previous behavior unchanged.
  async function ensureCart(targetCartId?: string) {
    const id = targetCartId ?? cartId;
    try {
      const res = await fetch(`${API_BASE}/carts/${id}`);
      if (res.status === 404) {
        const createRes = await fetch(`${API_BASE}/carts`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            cart_id: id,
            merchant_id: MERCHANT_ID,
            currency: "INR",
          }),
        });
        if (!createRes.ok) throw new Error("Failed to create cart");
        return (await createRes.json()) as Cart;
      }
      if (!res.ok) throw new Error("Failed to load cart");
      return (await res.json()) as Cart;
    } catch (error) {
      throw error instanceof Error ? error : new Error("Failed to load cart");
    }
  }

  // Returns whether the item actually landed in the cart -- every
  // existing caller ignored addToCart's return value already (it used
  // to be void), so adding this is backward compatible; the three
  // suggestion-accept flows (acceptSuggestion/acceptDetailSuggestion/
  // acceptPostCheckoutSuggestion) use it to only report an acceptance
  // (POST /growth/suggest/accept, item 20) once the add genuinely
  // succeeded, not just once the buyer clicked "Add".
  async function addToCart(product: Product, variantId?: string, targetCartId?: string): Promise<boolean> {
    const id = targetCartId ?? cartId;
    setLoading(true);
    setMessage("");
    try {
      await ensureCart(id);
      const resolvedVariantId = variantId ?? defaultVariantFor(product);

      const res = await fetch(`${API_BASE}/carts/${id}/items`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          product_id: product.product_id,
          variant_id: resolvedVariantId,
          title: product.title,
          quantity: 1,
        }),
      });
      if (!res.ok) throw new Error("Failed to add item to cart");

      setCart(await fetch(`${API_BASE}/carts/${id}`).then((r) => r.json()));
      setStep("cart");
      return true;
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to add item");
      return false;
    } finally {
      setLoading(false);
    }
  }

  // Both endpoints already existed on the backend (commerce/cart) and
  // were fully wired -- only the UI to call them was missing.
  async function updateItemQuantity(variantId: string, quantity: number) {
    if (quantity < 1) return;
    setLoading(true);
    setMessage("");
    try {
      const res = await fetch(`${API_BASE}/carts/${cartId}/items/${variantId}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ quantity }),
      });
      if (!res.ok) throw new Error(await res.text());
      setCart(await fetch(`${API_BASE}/carts/${cartId}`).then((r) => r.json()));
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to update quantity");
    } finally {
      setLoading(false);
    }
  }

  async function removeCartItem(variantId: string) {
    setLoading(true);
    setMessage("");
    try {
      const res = await fetch(`${API_BASE}/carts/${cartId}/items/${variantId}`, {
        method: "DELETE",
      });
      if (!res.ok) throw new Error(await res.text());
      setCart(await fetch(`${API_BASE}/carts/${cartId}`).then((r) => r.json()));
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to remove item");
    } finally {
      setLoading(false);
    }
  }

  async function proceedToCheckout() {
    setLoading(true);
    setMessage("");
    try {
      const res = await fetch(`${API_BASE}/carts/${cartId}/checkout`, {
        method: "POST",
      });
      if (!res.ok) throw new Error("Failed to create order");
      const ord = (await res.json()) as Order;
      setOrder(ord);
      setStep("checkout");
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Checkout failed");
    } finally {
      setLoading(false);
    }
  }

  // Conversational entry point: POST /agent/checkout. The agent only ever
  // returns a proposal (a selected product_id plus its reasoning) -- it
  // never creates a cart or moves money itself. Accepting the proposal
  // below just calls the same addToCart the manual catalog uses, so the
  // normal cart/policy/payment pipeline still runs unchanged.
  async function askAgent() {
    if (!agentPrompt.trim()) return;
    const prompt = agentPrompt;
    setAgentHistory((h) => [...h, { role: "user", content: prompt }]);
    setAgentPrompt("");
    setAgentLoading(true);
    setAgentError("");
    setAgentPlan(null);
    try {
      // cart_id doubles as the conversation_id (backend/agents/conversation.go)
      // -- sending it is what lets a follow-up like "no, for my brother
      // instead" build on what was already said in this cart, instead of
      // being extracted from scratch and rejected for missing budget/category.
      const res = await fetch(`${API_BASE}/agent/checkout`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ prompt, merchant: MERCHANT_ID, cart_id: cartId }),
      });
      if (!res.ok) {
        const text = await res.text();
        let errorMessage: string;
        if (text.includes("ambiguous intent")) {
          errorMessage = "Say a bit more -- what are you shopping for, and what is the budget?";
        } else if (text.includes("no suitable product")) {
          errorMessage = "Nothing in the catalog matches that yet -- try browsing below instead.";
        } else {
          errorMessage = "The assistant is temporarily unavailable -- you can browse the catalog manually below.";
        }
        setAgentError(errorMessage);
        setAgentHistory((h) => [...h, { role: "assistant", content: errorMessage }]);
        return;
      }
      const plan = (await res.json()) as CheckoutPlan;
      setAgentPlan(plan);
      setAgentHistory((h) => [...h, { role: "assistant", content: plan.reasoning }]);
    } catch {
      const errorMessage = "The assistant is temporarily unavailable -- you can browse the catalog manually below.";
      setAgentError(errorMessage);
      setAgentHistory((h) => [...h, { role: "assistant", content: errorMessage }]);
    } finally {
      setAgentLoading(false);
    }
  }

  async function acceptAgentPlan() {
    if (!agentPlan) return;
    const matched = products.find((p) => p.product_id === agentPlan.selected_product_id);
    if (!matched) {
      setAgentError("That product is no longer available -- try browsing below.");
      setAgentPlan(null);
      return;
    }
    setAgentPlan(null);
    setAgentPrompt("");
    await addToCart(matched);
  }

  // Adds one of the agent's next-best alternatives instead of its top
  // pick -- previously Search() ranked several matches but PlanCheckout
  // discarded everything past the first, so a buyer who didn't like the
  // one proposed product had no way to see what else the agent
  // considered short of retyping their whole request. Alternatives skip
  // the "Agent proposes" confirm step entirely (same one-click behavior
  // as the catalog list's own "Add to cart") since choosing one already
  // *is* the buyer's confirmation.
  async function chooseAlternative(alt: AlternativeProduct) {
    const matched = products.find((p) => p.product_id === alt.product_id) ?? {
      product_id: alt.product_id,
      title: alt.title,
      price: { amount: alt.price, currency: alt.currency },
      availability: 1,
    };
    setAgentPlan(null);
    setAgentPrompt("");
    await addToCart(matched);
  }

  // Cross-sell surfaced in the cart: POST /growth/suggest. The backend
  // picks the candidate and scores it (see backend/growth/suggest.go) --
  // this only renders whatever it returns, it never invents a product.
  async function fetchSuggestion() {
    setSuggestionLoading(true);
    try {
      const res = await fetch(`${API_BASE}/growth/suggest`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ cart_id: cartId }),
      });
      if (!res.ok) {
        setSuggestion(null);
        return;
      }
      const data = (await res.json()) as SuggestResponse;
      if (data.available && data.product && data.product.product_id !== dismissedProductId) {
        setSuggestion(data);
      } else {
        setSuggestion(null);
      }
    } catch {
      setSuggestion(null);
    } finally {
      setSuggestionLoading(false);
    }
  }

  // Best-effort notification that a suggestion was actually accepted
  // (POST /growth/suggest/accept, item 20, PLAN-03-PROACTIVE-GROWTH-
  // AGENT.md §8) -- feeds the merchant dashboard's suggestion_
  // impressions/suggestion_acceptances metrics. Same fire-and-forget
  // posture as dismissSuggestion below: the buyer's item is already in
  // their cart either way, so a failure here should never surface as a
  // page-level error.
  function notifySuggestionAccepted(forCartId: string, productId: string) {
    fetch(`${API_BASE}/growth/suggest/accept`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ cart_id: forCartId, product_id: productId }),
    }).catch(() => {
      // best-effort, see comment above
    });
  }

  async function acceptSuggestion() {
    if (!suggestion?.product) return;
    const suggested = suggestion.product;
    setSuggestion(null);
    const matched = products.find((p) => p.product_id === suggested.product_id) ?? {
      product_id: suggested.product_id,
      title: suggested.title,
      price: { amount: suggested.price, currency: suggested.currency },
      availability: 1,
    };
    if (await addToCart(matched)) {
      notifySuggestionAccepted(cartId, suggested.product_id);
    }
  }

  // Persists the dismissal server-side (POST /growth/suggest/dismiss,
  // backend/growth/suggest.go's DismissalStore) so it survives a reload
  // and Suggest excludes this product for the rest of the cart's life --
  // previously dismissedProductId only ever lived in React state, so a
  // reload (or the effect simply re-running) could show the exact same
  // "No thanks"-ed product again. dismissedProductId stays as an
  // immediate client-side hide for this render; the POST is
  // best-effort -- if it fails, the suggestion just isn't excluded on
  // the *next* fetch, which is a much smaller regression than the
  // dismiss button silently doing nothing, so it isn't surfaced as a
  // page-level error.
  function dismissSuggestion() {
    if (suggestion?.product) {
      setDismissedProductId(suggestion.product.product_id);
      fetch(`${API_BASE}/growth/suggest/dismiss`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ cart_id: cartId, product_id: suggestion.product.product_id }),
      }).catch(() => {
        // best-effort, see comment above
      });
    }
    setSuggestion(null);
  }

  // Product-detail cross-sell: POST /growth/suggest/product. Scores
  // against just the one viewed product's own tags (backend/growth/
  // suggest.go's SuggestForProduct), not the whole cart -- reaches a
  // buyer who never adds anything to a cart or opens the agent chat,
  // the one surface with previously zero cross-sell exposure at all
  // (PLAN-03-PROACTIVE-GROWTH-AGENT.md §3).
  async function toggleProductDetail(productId: string) {
    if (expandedProductId === productId) {
      setExpandedProductId(null);
      setDetailSuggestion(null);
      return;
    }
    setExpandedProductId(productId);
    setDetailSuggestion(null);
    setDetailSuggestionLoading(true);
    try {
      const res = await fetch(`${API_BASE}/growth/suggest/product`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ product_id: productId, cart_id: cartId }),
      });
      if (res.ok) {
        const data = (await res.json()) as SuggestResponse;
        setDetailSuggestion(data.available ? data : null);
      } else {
        setDetailSuggestion(null);
      }
    } catch {
      setDetailSuggestion(null);
    } finally {
      setDetailSuggestionLoading(false);
    }
  }

  async function acceptDetailSuggestion() {
    if (!detailSuggestion?.product) return;
    const suggested = detailSuggestion.product;
    setDetailSuggestion(null);
    const matched = products.find((p) => p.product_id === suggested.product_id) ?? {
      product_id: suggested.product_id,
      title: suggested.title,
      price: { amount: suggested.price, currency: suggested.currency },
      availability: 1,
    };
    if (await addToCart(matched)) {
      notifySuggestionAccepted(cartId, suggested.product_id);
    }
  }

  // Post-checkout "complete the set" cross-sell: POST
  // /growth/suggest/order, scored against the order that was just
  // placed rather than a live cart -- the checked-out cart_id would
  // 404 on GetCart (backend/commerce/cart/service.go), which is exactly
  // why this is a separate endpoint (backend/growth/suggest.go's
  // SuggestForOrder) instead of reusing /growth/suggest with the old
  // cart_id. A real, low-risk revenue surface most e-commerce checkouts
  // use that this app previously had zero equivalent of
  // (PLAN-03-PROACTIVE-GROWTH-AGENT.md §4).
  async function fetchPostCheckoutSuggestion(orderId: string) {
    setPostCheckoutSuggestionLoading(true);
    try {
      const res = await fetch(`${API_BASE}/growth/suggest/order`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ order_id: orderId }),
      });
      if (res.ok) {
        const data = (await res.json()) as SuggestResponse;
        setPostCheckoutSuggestion(data.available ? data : null);
      } else {
        setPostCheckoutSuggestion(null);
      }
    } catch {
      setPostCheckoutSuggestion(null);
    } finally {
      setPostCheckoutSuggestionLoading(false);
    }
  }

  // Accepting a post-checkout suggestion starts a brand new cart (the
  // just-completed one is single-use and already checked_out) with the
  // suggested item already in it, then lands on the cart screen ready
  // to check out again -- same reset shape as the "Start a new order"
  // button below, just pre-populated. Passes the fresh cart id straight
  // into addToCart/ensureCart rather than relying on setCartId's
  // (asynchronous) state update, which addToCart's own `cartId` closure
  // would not see yet in this same function call.
  async function acceptPostCheckoutSuggestion() {
    if (!postCheckoutSuggestion?.product) return;
    const suggested = postCheckoutSuggestion.product;
    const freshId = freshCartId();
    // SuggestForOrder (backend/growth/suggest.go) keys this
    // recommendation to the ORDER's own cart_id, not the new cart about
    // to be created -- so the accept notification must use that same
    // original cart_id (captured here, before the reset below clears
    // `order` from state) for RecordAcceptance to find the right row.
    const originalCartId = order?.cart_id ?? freshId;
    setPostCheckoutSuggestion(null);
    setCartId(freshId);
    setCart(null);
    setOrder(null);
    setPayment(null);
    setRunId("");
    setRun(null);
    setReviews({});
    const matched = products.find((p) => p.product_id === suggested.product_id) ?? {
      product_id: suggested.product_id,
      title: suggested.title,
      price: { amount: suggested.price, currency: suggested.currency },
      availability: 1,
    };
    if (await addToCart(matched, undefined, freshId)) {
      notifySuggestionAccepted(originalCartId, suggested.product_id);
    }
  }

  // GET /orders?merchant_id=... -- merchant-scoped for now since there
  // is no buyer identity yet (files/AUTH.md); every order for this
  // single-merchant demo qualifies as "history".
  async function fetchOrders() {
    setOrdersLoading(true);
    setOrdersError("");
    try {
      const res = await fetch(`${API_BASE}/orders?merchant_id=${MERCHANT_ID}`);
      if (!res.ok) throw new Error("Failed to load orders");
      const data = (await res.json()) as Order[];
      setOrders(data ?? []);
    } catch (error) {
      // Its own state, not the page-wide `message` banner: a failed
      // fetch and a genuinely empty order list need to render as two
      // different things, not stack on top of each other.
      setOrders([]);
      setOrdersError(
        error instanceof Error ? error.message : "Failed to load orders"
      );
    } finally {
      setOrdersLoading(false);
    }
  }

  function viewOrderHistory() {
    setStep("orders");
    fetchOrders();
  }

  // POST /orders/{id}/review -- the post-checkout "Rate this order"
  // prompt (PLAN-02-CATALOG-AND-COMMERCE.md §2). Per-product client
  // state only; a failed submit leaves the form in place with an
  // inline error instead of losing what the buyer typed.
  function rateProduct(productId: string, rating: number) {
    setReviews((prev) => ({
      ...prev,
      [productId]: {
        rating,
        comment: prev[productId]?.comment ?? "",
        submitting: false,
        submitted: prev[productId]?.submitted ?? false,
        error: "",
      },
    }));
  }

  function commentOnProduct(productId: string, comment: string) {
    setReviews((prev) => ({
      ...prev,
      [productId]: {
        rating: prev[productId]?.rating ?? 0,
        comment,
        submitting: prev[productId]?.submitting ?? false,
        submitted: prev[productId]?.submitted ?? false,
        error: prev[productId]?.error ?? "",
      },
    }));
  }

  async function submitReview(orderId: string, productId: string) {
    const entry = reviews[productId];
    if (!entry || entry.rating < 1) return;

    setReviews((prev) => ({
      ...prev,
      [productId]: { ...prev[productId], submitting: true, error: "" },
    }));

    try {
      const res = await fetch(`${API_BASE}/orders/${orderId}/review`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          product_id: productId,
          rating: entry.rating,
          comment: entry.comment,
        }),
      });
      if (!res.ok) {
        throw new Error(await res.text() || "Failed to submit review");
      }
      setReviews((prev) => ({
        ...prev,
        [productId]: { ...prev[productId], submitting: false, submitted: true },
      }));
    } catch (error) {
      setReviews((prev) => ({
        ...prev,
        [productId]: {
          ...prev[productId],
          submitting: false,
          error: error instanceof Error ? error.message : "Failed to submit review",
        },
      }));
    }
  }

  // Re-check for a suggestion on every cart mutation, independent of
  // which screen the buyer is currently on. Previously this was gated on
  // `step === "cart"`, so a suggestion only ever appeared while the
  // buyer happened to be looking at the cart screen at the exact moment
  // it was computed -- going back to "Keep shopping" (the normal flow
  // after accepting an agent-proposed product) meant no further
  // suggestion was ever surfaced again for the rest of the session, even
  // though the growth agent kept working correctly the whole time. See
  // files/REALITY-CHECK-2026-08-30.md §3 and
  // files/PLAN-03-PROACTIVE-GROWTH-AGENT.md §1. renderSuggestionCard()
  // now renders on both the catalog and cart screens, so this only needs
  // to decide *when* to fetch, not *where* to show the result.
  useEffect(() => {
    if (cart && cart.items.length > 0) {
      // Fetching from an external system (the growth service) on a
      // dependency change is exactly what useEffect is for; the
      // setState calls happen inside fetchSuggestion, not here.
      // eslint-disable-next-line react-hooks/set-state-in-effect
      fetchSuggestion();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cart?.cart_id, cart?.items.length]);

  async function startPayment() {
    setLoading(true);
    setMessage("Creating payment...");
    try {
      const mandateRes = await fetch(`${API_BASE}/policy/mandates`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          buyer: "checkout_user",
          merchant: MERCHANT_ID,
          maximum_amount: order!.subtotal,
          currency: order!.currency,
          allowed_payment_methods: ["card", "upi"],
          purpose: `Checkout ${order!.order_id}`,
          cart_id: order!.cart_id,
        }),
      });
      if (!mandateRes.ok) throw new Error(await mandateRes.text());
      const mandate = (await mandateRes.json()) as Mandate;

      		const authorizationRes = await fetch(`${API_BASE}/policy/propose`, {
      			method: "POST",
      			headers: { "Content-Type": "application/json" },
      			body: JSON.stringify({
      				action: "CREATE_ORDER",
      				amount: order!.subtotal,
      				currency: order!.currency,
      				merchant: MERCHANT_ID,
      				items: order!.items.map((item) => item.product_id),
      				mandate_id: mandate.mandate_id,
      				cart_id: order!.cart_id,
      			}),
      		});
      		if (!authorizationRes.ok) throw new Error(await authorizationRes.text());
      		const decision = (await authorizationRes.json()) as Decision;
      		if (decision.action_id) setRunId(decision.action_id);

      		// Level 2/3 → durable human approval required before an
      		// authorization is issued.
      if (decision.decision === "PENDING_HUMAN_APPROVAL") {
        if (!decision.approval_request_id) {
          throw new Error("Approval required but no request was created");
        }
        const detail = await fetchApprovalRequest(decision.approval_request_id);
        setApprovalRequestId(decision.approval_request_id);
        setApprovalReason(decision.reason || "This purchase requires operator approval.");
        setApprovalLevel(decision.level);
        setApprovalSnapshot(detail);
        setGateConfirmed(false);
        setGateError("");
        setStep(decision.level >= 3 ? "gate" : "approval");
        setLoading(false);
        return;
      }

      		if (decision.decision !== "APPROVED" || !decision.authorization_id) {
      			// Rejected outright (e.g. over the amount ceiling, or an item
      			// not on the merchant's permitted list): show it as a clear
      			// terminal state instead of leaving this same "Confirm Order /
      			// Pay" screen up with a button that would just fail identically
      			// every time it's clicked again -- no payment was ever
      			// attempted, so "failed"/recovery (which assumes a declined
      			// Razorpay attempt) doesn't apply here.
      			setPolicyRejectionReason(decision.reason || "This purchase was not authorized.");
      			setStep("policy_rejected");
      			setLoading(false);
      			return;
      		}

		await createPaymentWithLaunch(decision.authorization_id);
	} catch (error) {
      					const msg = error instanceof Error ? error.message : "Payment failed";
      					setMessage(msg);
      					setLoading(false);
      				}
      			}

      			// Approve a Level 2/3 request, then continue to payment.
      			async function approveAndPay() {
      				setLoading(true);
      				setMessage("Approving payment...");
      				try {
      					const res = await fetch(`${API_BASE}/approval-requests/${approvalRequestId}/approve`, {
      						method: "POST",
      						headers: { "Content-Type": "application/json" },
      						body: JSON.stringify({ cart_id: order!.cart_id }),
      					});
      					if (!res.ok) throw new Error(await res.text());
      					const decision = (await res.json()) as Decision;
      					if (decision.decision !== "APPROVED" || !decision.authorization_id) {
      						throw new Error(decision.reason || "Approval did not produce an authorization");
      					}
      					if (decision.action_id) setRunId(decision.action_id);
      					await createPaymentWithLaunch(decision.authorization_id);
      				} catch (error) {
      					setMessage(error instanceof Error ? error.message : "Approval failed");
      					setLoading(false);
      				}
      			}

      			// Reject a Level 2/3 request, returning to the catalog.
      			async function rejectApproval() {
      				setLoading(true);
      				setMessage("");
      				try {
      					await fetch(`${API_BASE}/approval-requests/${approvalRequestId}/reject`, {
      						method: "POST",
      						headers: { "Content-Type": "application/json" },
      						body: JSON.stringify({ cart_id: order!.cart_id, reason: "cancelled at approval screen" }),
      					});
      				} catch {
      					// best-effort; the cart is abandoned either way
      				}
      				resetToCatalog("Purchase cancelled. The approval was not granted.");
      			}

      			// Create the Razorpay order and open the Standard Checkout UI.
      			async function createPaymentWithLaunch(authId: string) {
      				const res = await fetch(`${API_BASE}/orders/${order!.order_id}/payment`, {
      					method: "POST",
      					headers: {
      						"Authorization-Id": authId,
      						"Idempotency-Key": `payment_${order!.order_id}`,
      					},
      				});
      				if (!res.ok) throw new Error("Failed to create payment");
      				const pay = (await res.json()) as Payment;
      				setPayment(pay);
      				setStep("pay");
      				setLoading(false);

      				// Open Razorpay Standard Checkout with the server-created order.
      				const options: RazorpayOptions = {
      					key: pay.key_id,
      					amount: pay.amount,
      					currency: pay.currency,
      					name: "CommerceOS",
      					description: `Order ${pay.order_id}`,
      					order_id: pay.provider_order_id,
      					handler: async (response) => {
      						await verifyPayment(response);
      					},
					modal: {
						ondismiss: () => {
							setStep("failed");
							loadRecovery();
							setLoading(false);
						},
					},
      					theme: { color: "#000000" },
      				};

      				const razorpay = new window.Razorpay(options);
      				razorpay.open();
      			}

      			// Fetch the authoritative recovery view from the server.
			async function loadRecovery() {
				if (!order) return;
				try {
					const res = await fetch(`${API_BASE}/orders/${order.order_id}/recovery`, { cache: "no-store" });
					if (res.ok) {
						setRecovery((await res.json()) as Recovery);
					}
				} catch {
					// fall back to the static message; recovery stays null
				}
			}

			// Remove one removable item (e.g. an accessory) from a failed
			// order, rebuild a fresh smaller cart with catalog-authoritative
			// prices/availability, and re-checkout it server-side. The
			// caller returns to the order screen with the smaller order;
			// clicking Pay there re-proposes to the policy engine on the
			// new total -- policy is never bypassed for the smaller cart.
			async function removeAccessoryAndRetry(variantId: string) {
				if (!order) return;
				setLoading(true);
				setMessage("Removing item and recomputing your order...");
				try {
					const res = await fetch(`${API_BASE}/orders/${order.order_id}/recovery/remove-item`, {
						method: "POST",
						headers: { "Content-Type": "application/json" },
						body: JSON.stringify({ variant_id: variantId }),
					});
					if (!res.ok) throw new Error(await res.text());
					const newOrder = (await res.json()) as Order;
					setOrder(newOrder);
					setCartId(newOrder.cart_id);
					setPayment(null);
					setRecovery(null);
					setMessage("Item removed. Review your updated order, then pay when ready.");
					setStep("checkout");
				} catch (error) {
					setMessage(error instanceof Error ? error.message : "Could not remove that item");
				} finally {
					setLoading(false);
				}
			}

			async function resetToCatalog(messageText: string) {
      				setStep("catalog");
      				setCartId(freshCartId());
      				setCart(null);
      				setOrder(null);
      				setPayment(null);
      				setApprovalRequestId("");
      				setApprovalLevel(0);
      				setApprovalSnapshot(null);
      				setGateConfirmed(false);
      				setGateError("");
      				setPolicyRejectionReason("");
      				setRunId("");
      				setRun(null);
      				setMessage(messageText);
      			}

  // Fetch the current state of an approval request from the server. Used
  // both to build the confirmation snapshot and, for Level 3, to detect
  // drift immediately before acting on stale data.
  async function fetchApprovalRequest(id: string) {
    const res = await fetch(`${API_BASE}/approval-requests/${id}`, { cache: "no-store" });
    if (!res.ok) throw new Error("Failed to load approval request");
    return (await res.json()) as ApprovalRequestDetail;
  }

  // Level 3 hard gate: re-fetch the approval request immediately before
  // acting. Refuses to proceed if it is no longer PENDING or if the
  // amount/items/merchant/cart/policy version have drifted from what was
  // shown on this screen -- never trust cached state for a hard gate.
  async function approveGateAndPay() {
    if (!approvalSnapshot) return;
    setLoading(true);
    setGateError("");
    setMessage("Re-checking approval request...");
    try {
      const fresh = await fetchApprovalRequest(approvalRequestId);
      if (fresh.status !== "PENDING") {
        throw new Error(`This approval request is now ${fresh.status.toLowerCase()}, not pending. Go back and start over.`);
      }
      const drifted =
        fresh.amount !== approvalSnapshot.amount ||
        fresh.currency !== approvalSnapshot.currency ||
        fresh.merchant !== approvalSnapshot.merchant ||
        fresh.cart_id !== approvalSnapshot.cart_id ||
        fresh.policy_version !== approvalSnapshot.policy_version ||
        fresh.items.join("|") !== approvalSnapshot.items.join("|");
      if (drifted) {
        throw new Error("The order changed since this approval was requested. Go back and start over.");
      }
      setMessage("Approving payment...");
      const res = await fetch(`${API_BASE}/approval-requests/${approvalRequestId}/approve`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ cart_id: fresh.cart_id }),
      });
      if (!res.ok) throw new Error(await res.text());
      const decision = (await res.json()) as Decision;
      if (decision.decision !== "APPROVED" || !decision.authorization_id) {
        throw new Error(decision.reason || "Approval did not produce an authorization");
      }
      if (decision.action_id) setRunId(decision.action_id);
      await createPaymentWithLaunch(decision.authorization_id);
    } catch (error) {
      const msg = error instanceof Error ? error.message : "Approval failed";
      setGateError(msg);
      setMessage("");
      setLoading(false);
    }
  }

  // Return to the order screen after a Level 3 gate is invalidated
  // (drifted or no longer pending) so the buyer can re-propose cleanly.
  function backToOrderFromGate() {
    setStep("checkout");
    setApprovalRequestId("");
    setApprovalReason("");
    setApprovalLevel(0);
    setApprovalSnapshot(null);
    setGateConfirmed(false);
    setGateError("");
    setMessage("That approval request is no longer valid. Review your order and try again.");
  }

  // GET /runs/{id}: the audit trail for the action this checkout actually
  // proposed -- proposed -> risk-assessed -> policy-evaluated -> authorized
  // -- reconstructed from the persisted records, not a separate log. Shown
  // inline on the complete/failed screens (files/AUTH.md).
  async function fetchRun() {
    if (!runId) return;
    setRunLoading(true);
    try {
      const res = await fetch(`${API_BASE}/runs/${runId}`, { cache: "no-store" });
      if (!res.ok) throw new Error("Failed to load audit trail");
      setRun((await res.json()) as Run);
    } catch {
      // The audit trail is a nice-to-have on this screen -- a failure to
      // load it should never block showing the buyer their order result.
    } finally {
      setRunLoading(false);
    }
  }

  useEffect(() => {
    if ((step === "complete" || step === "failed") && runId && !run) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      fetchRun();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [step, runId]);

  // Fetch the post-checkout "complete the set" suggestion once, the
  // moment the order-complete screen has a real order to score against
  // -- order?.order_id as the dependency (not just `step`) means this
  // only re-fires for a genuinely new order, not a re-render of the
  // same one.
  useEffect(() => {
    if (step === "complete" && order) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      fetchPostCheckoutSuggestion(order.order_id);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [step, order?.order_id]);

  async function verifyPayment(response: RazorpayResponse) {
    setMessage("Verifying payment...");
    try {
      const res = await fetch(
        `${API_BASE}/orders/${order!.order_id}/payment/verify`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            razorpay_payment_id: response.razorpay_payment_id,
            razorpay_order_id: response.razorpay_order_id,
            razorpay_signature: response.razorpay_signature,
          }),
        },
      );
      if (!res.ok) throw new Error("Payment verification failed");
      const verified = (await res.json()) as Payment;
      setPayment(verified);
      setStep("complete");
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Verification failed");
    }
  }

  function formatINR(amount: number) {
    return `₹${(amount / 100).toFixed(2)}`;
  }

  // Inline audit-trail panel shown on the complete/failed screens --
  // P0.4: the buyer sees the same proposed -> risk-assessed ->
  // policy-evaluated -> authorized timeline a merchant operator would see
  // in the dashboard's Runs tab, for the exact action their checkout ran.
  function renderAuditTrail() {
    if (!runId) return null;
    return (
      <div className="mt-6 rounded-xl border border-slate-200 p-5">
        <p className="text-sm font-semibold text-slate-900">Audit trail</p>
        <p className="mt-1 text-xs text-slate-500">
          Every step the policy engine took for this action, reconstructed
          from the persisted audit log (run {runId}).
        </p>
        {runLoading && (
          <div className="mt-3 space-y-2 border-t border-slate-100 pt-3">
            <Skeleton className="h-3 w-full" />
            <Skeleton className="h-3 w-5/6" />
            <Skeleton className="h-3 w-2/3" />
          </div>
        )}
        {run && run.steps && run.steps.length > 0 && (
          <ul className="mt-3 space-y-3 border-t border-slate-100 pt-3">
            {run.steps.map((s, i) => (
              <li key={i} className="flex items-start gap-3 text-xs">
                <span className="mt-1 h-1.5 w-1.5 flex-shrink-0 rounded-full bg-slate-400" />
                <div>
                  <p className="font-medium capitalize text-slate-700">
                    {s.stage.replace(/_/g, " ")}
                  </p>
                  <p className="text-slate-500">{s.detail}</p>
                  <p className="mt-0.5 text-slate-400">
                    {new Date(s.timestamp).toLocaleTimeString()}
                  </p>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>
    );
  }

  // Cross-sell card, shared between the catalog and cart screens so a
  // computed suggestion stays visible regardless of which one the buyer
  // is on -- previously this only rendered inside the cart screen, so a
  // buyer who went back to "Keep shopping" (or arrived at a suggestion
  // via the agent chat, which also lands them on the cart screen only
  // momentarily) had no way to see it again without navigating back into
  // the cart. See files/PLAN-03-PROACTIVE-GROWTH-AGENT.md §1-2.
  function renderSuggestionCard() {
    if (suggestion?.available && suggestion.product) {
      return (
        <div className="mt-4 rounded-xl border border-indigo-200 bg-indigo-50 p-5">
          <p className="text-xs font-medium uppercase tracking-wide text-indigo-700">
            Agent suggests
          </p>
          <p className="mt-1 font-semibold text-slate-900">
            Add {suggestion.product.title} -- {formatINR(suggestion.product.price)}
          </p>
          {suggestion.recommendation && (
            <p className="mt-1 text-sm text-indigo-800">{suggestion.recommendation.reason}</p>
          )}
          <div className="mt-3 flex gap-3">
            <button
              onClick={acceptSuggestion}
              disabled={loading}
              className="rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
            >
              Add to cart
            </button>
            <button
              onClick={dismissSuggestion}
              className="rounded-lg border border-indigo-300 px-4 py-2 text-sm font-medium text-indigo-800 hover:bg-indigo-100"
            >
              No thanks
            </button>
          </div>
        </div>
      );
    }
    if (suggestionLoading) {
      return (
        <div className="mt-4 rounded-xl border border-indigo-200 bg-indigo-50 p-5">
          <Skeleton className="h-3 w-28" />
          <Skeleton className="mt-2 h-4 w-2/3" />
          <div className="mt-3 flex gap-3">
            <Skeleton className="h-9 w-28" />
            <Skeleton className="h-9 w-24" />
          </div>
        </div>
      );
    }
    return null;
  }

  return (
    <main className="min-h-screen bg-slate-50">
      <div className="mx-auto max-w-3xl px-6 py-10">
        <header className="mb-8 flex items-start justify-between">
          <div>
            <p className="text-sm font-medium text-slate-500">CommerceOS</p>
            <h1 className="mt-2 text-3xl font-bold tracking-tight text-slate-900">
              {step === "complete" ? "Order Complete" : step === "orders" ? "Order History" : "Checkout"}
            </h1>
          </div>
          {(step === "catalog" || step === "cart" || step === "orders") && (
            <button
              onClick={() => (step === "orders" ? setStep("catalog") : viewOrderHistory())}
              className="mt-1 text-sm font-medium text-slate-600 underline underline-offset-4 hover:text-slate-900"
            >
              {step === "orders" ? "Back to shopping" : "Order history"}
            </button>
          )}
        </header>

        {message && (
          <div className="mb-6 rounded-lg bg-slate-100 p-4 text-sm text-slate-700">
            {message}
          </div>
        )}

        {step === "catalog" && (
          <section>
            <div className="mb-6 rounded-xl border border-slate-200 bg-slate-50 p-5">
              <h2 className="mb-1 text-sm font-semibold uppercase tracking-wide text-slate-500">
                Ask the shopping agent
              </h2>
              <p className="mb-3 text-sm text-slate-600">
                Say what you want and the budget. It reads the catalog and proposes one item -- it never places an order itself; the normal checkout below still runs.
              </p>

              {agentHistory.length > 0 && (
                <div className="mb-3 max-h-40 space-y-2 overflow-y-auto rounded-lg border border-zinc-200 bg-white p-3">
                  {agentHistory.map((msg, i) => (
                    <p key={i} className="text-sm">
                      <span className="font-medium text-zinc-500">
                        {msg.role === "user" ? "You: " : "Agent: "}
                      </span>
                      <span className="text-zinc-700">{msg.content}</span>
                    </p>
                  ))}
                </div>
              )}

              <div className="flex gap-2">
                <input
                  type="text"
                  value={agentPrompt}
                  onChange={(e) => setAgentPrompt(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") askAgent();
                  }}
                  placeholder="earbuds for my sister, budget 25000, good battery life"
                  className="flex-1 rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-slate-500 focus:outline-none"
                  disabled={agentLoading}
                />
                <button
                  onClick={askAgent}
                  disabled={agentLoading || !agentPrompt.trim()}
                  className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-800 disabled:opacity-50"
                >
                  {agentLoading ? "Thinking..." : "Ask"}
                </button>
              </div>

              {agentError && <p className="mt-3 text-sm text-amber-700">{agentError}</p>}

              {agentLoading && (
                <div className="mt-4 rounded-lg border border-slate-300 bg-white p-4">
                  <Skeleton className="h-3 w-24" />
                  <Skeleton className="mt-3 h-4 w-full" />
                  <div className="mt-3 flex gap-3">
                    <Skeleton className="h-9 w-28" />
                    <Skeleton className="h-9 w-28" />
                  </div>
                </div>
              )}

              {agentPlan && (
                <div className="mt-4 rounded-lg border border-slate-300 bg-white p-4">
                  <p className="text-xs font-medium uppercase tracking-wide text-slate-500">
                    Agent proposes
                  </p>
                  <p className="mt-1 text-sm text-slate-700">{agentPlan.reasoning}</p>
                  <div className="mt-3 flex gap-3">
                    <button
                      onClick={acceptAgentPlan}
                      disabled={loading}
                      className="rounded-lg bg-black px-4 py-2 text-sm font-medium text-white hover:bg-slate-800 disabled:opacity-50"
                    >
                      Add to cart
                    </button>
                    <button
                      onClick={() => setAgentPlan(null)}
                      className="rounded-lg border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-100"
                    >
                      Never mind
                    </button>
                  </div>
                  {agentPlan.alternatives && agentPlan.alternatives.length > 0 && (
                    <div className="mt-3 border-t border-zinc-100 pt-3">
                      <p className="text-xs text-zinc-500">Or:</p>
                      <ul className="mt-1 space-y-1">
                        {agentPlan.alternatives.map((alt) => (
                          <li key={alt.product_id}>
                            <button
                              onClick={() => chooseAlternative(alt)}
                              disabled={loading}
                              className="text-sm font-medium text-zinc-700 underline underline-offset-4 hover:text-zinc-900 disabled:opacity-50"
                            >
                              {alt.title} -- {formatINR(alt.price)}
                            </button>
                          </li>
                        ))}
                      </ul>
                    </div>
                  )}
                </div>
              )}
            </div>

            {renderSuggestionCard()}

            <h2 className="mb-4 text-lg font-semibold text-slate-900">
              Browse Catalog
            </h2>
            {products.length === 0 ? (
              <p className="text-sm text-slate-500">
                No products available. Ensure the Commerce Service is running
                and the catalog is seeded.
              </p>
            ) : (
              <>
                <div className="mb-4 space-y-3">
                  <input
                    type="text"
                    value={searchQuery}
                    onChange={(e) => setSearchQuery(e.target.value)}
                    placeholder="Search by name or feature..."
                    className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-slate-500 focus:outline-none"
                  />
                  <div className="flex flex-wrap items-center gap-2">
                    {categories.map((category) => (
                      <button
                        key={category}
                        onClick={() => setSelectedCategory((current) => (current === category ? null : category))}
                        className={`rounded-full border px-3 py-1 text-xs font-medium transition ${
                          selectedCategory === category
                            ? "border-black bg-black text-white"
                            : "border-slate-300 bg-white text-slate-700 hover:border-slate-400"
                        }`}
                      >
                        {category.replace(/_/g, " ")}
                      </button>
                    ))}
                    <label className="ml-auto flex items-center gap-2 text-sm text-slate-500">
                      Sort by
                      <select
                        value={sortBy}
                        onChange={(e) => setSortBy(e.target.value as typeof sortBy)}
                        className="rounded-lg border border-slate-300 px-2 py-1.5 text-sm focus:border-slate-500 focus:outline-none"
                      >
                        <option value="default">Featured</option>
                        <option value="price_asc">Price: low to high</option>
                        <option value="price_desc">Price: high to low</option>
                        <option value="rating">Rating</option>
                        <option value="availability">Availability</option>
                      </select>
                    </label>
                  </div>
                </div>

                {filteredProducts.length === 0 ? (
                  <p className="text-sm text-slate-500">
                    No products match your search or filter.
                  </p>
                ) : (
                  <ul className="divide-y divide-slate-200">
                    {filteredProducts.map((product) => {
                  // Only a genuine differentiator (>1 variant) gets a
                  // picker rendered -- a product with just its own
                  // "<id>-default" entry shows the plain row exactly as
                  // before item 10.
                  const hasVariants = !!product.variants && product.variants.length > 1;
                  const activeVariantId = selectedVariant[product.product_id] ?? defaultVariantFor(product);
                  const activeVariant = product.variants?.find((v) => v.variant_id === activeVariantId);
                  const displayPrice = activeVariant?.price.amount ?? product.price.amount;
                  const displayAvailability = activeVariant?.availability ?? product.availability;

                  const isExpanded = expandedProductId === product.product_id;

                  return (
                    <li key={product.product_id} className="py-4">
                      <div className="flex items-center justify-between">
                        <div>
                          <button
                            onClick={() => toggleProductDetail(product.product_id)}
                            className="text-left font-semibold text-slate-900 underline-offset-2 hover:underline"
                          >
                            {product.title}
                          </button>
                          <p className="text-sm text-slate-500">
                            {formatINR(displayPrice)} · {displayAvailability} in stock
                            {!!product.review_count && (
                              <>
                                {" "}
                                · <span className="text-amber-600">★ {product.average_rating?.toFixed(1)}</span>{" "}
                                ({product.review_count})
                              </>
                            )}
                          </p>
                          {hasVariants && (
                            <div className="mt-2 flex flex-wrap gap-1">
                              {product.variants!.map((variant) => (
                                <button
                                  key={variant.variant_id}
                                  onClick={() =>
                                    setSelectedVariant((sel) => ({ ...sel, [product.product_id]: variant.variant_id }))
                                  }
                                  disabled={variant.availability === 0}
                                  className={`rounded-full border px-3 py-1 text-xs font-medium transition disabled:cursor-not-allowed disabled:opacity-40 ${
                                    variant.variant_id === activeVariantId
                                      ? "border-black bg-black text-white"
                                      : "border-slate-300 bg-white text-slate-700 hover:border-slate-400"
                                  }`}
                                >
                                  {variantLabel(variant, product)}
                                </button>
                              ))}
                            </div>
                          )}
                        </div>
                        <button
                          onClick={() => addToCart(product, activeVariantId)}
                          disabled={loading}
                          className="rounded-lg bg-black px-4 py-2 text-sm font-medium text-white transition hover:bg-slate-800 disabled:opacity-50"
                        >
                          Add to cart
                        </button>
                      </div>

                      {isExpanded && (
                        <div className="mt-3 rounded-lg bg-slate-50 p-4 text-sm text-slate-600">
                          {!!product.features?.length && (
                            <div className="flex flex-wrap gap-1.5">
                              {product.features.map((feature) => (
                                <span
                                  key={feature}
                                  className="rounded-full border border-slate-200 bg-white px-2.5 py-0.5 text-xs text-slate-600"
                                >
                                  {feature.replace(/_/g, " ")}
                                </span>
                              ))}
                            </div>
                          )}
                          <p className="mt-3">
                            {product.return_policy?.days
                              ? `${product.return_policy.days}-day returns`
                              : "No returns"}
                            {!!product.shipping?.estimated_days && (
                              <> · ships in {product.shipping.estimated_days} day{product.shipping.estimated_days > 1 ? "s" : ""}</>
                            )}
                          </p>

                          {detailSuggestionLoading && (
                            <div className="mt-3 rounded-lg border border-indigo-200 bg-indigo-50 p-3">
                              <Skeleton className="h-3 w-24" />
                              <Skeleton className="mt-2 h-4 w-1/2" />
                            </div>
                          )}
                          {!detailSuggestionLoading && detailSuggestion?.product && (
                            <div className="mt-3 rounded-lg border border-indigo-200 bg-indigo-50 p-3">
                              <p className="text-xs font-medium uppercase tracking-wide text-indigo-700">
                                Frequently paired with
                              </p>
                              <div className="mt-1 flex items-center justify-between">
                                <p className="text-sm text-slate-900">
                                  {detailSuggestion.product.title} -- {formatINR(detailSuggestion.product.price)}
                                </p>
                                <button
                                  onClick={acceptDetailSuggestion}
                                  disabled={loading}
                                  className="rounded-lg bg-indigo-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
                                >
                                  Add
                                </button>
                              </div>
                            </div>
                          )}
                        </div>
                      )}
                    </li>
                  );
                    })}
                  </ul>
                )}
              </>
            )}
          </section>
        )}

        {step === "cart" && cart && (
          <section>
            <h2 className="mb-4 text-lg font-semibold text-slate-900">
              Your Cart
            </h2>
            <ul className="divide-y divide-slate-200">
              {cart.items.map((item) => (
                <li
                  key={item.variant_id}
                  className="flex items-center justify-between py-4"
                >
                  <div>
                    <p className="font-semibold text-slate-900">{item.title}</p>
                    <p className="text-sm text-slate-500">
                      {formatINR(item.unit_price)} each
                    </p>
                  </div>
                  <div className="flex items-center gap-4">
                    <div className="flex items-center gap-2">
                      <button
                        onClick={() => updateItemQuantity(item.variant_id, item.quantity - 1)}
                        disabled={loading || item.quantity <= 1}
                        aria-label={`Decrease quantity of ${item.title}`}
                        className="h-7 w-7 rounded border border-slate-300 text-sm font-medium text-slate-700 hover:bg-slate-100 disabled:opacity-40"
                      >
                        −
                      </button>
                      <span className="w-5 text-center text-sm font-medium text-slate-900">
                        {item.quantity}
                      </span>
                      <button
                        onClick={() => updateItemQuantity(item.variant_id, item.quantity + 1)}
                        disabled={loading}
                        aria-label={`Increase quantity of ${item.title}`}
                        className="h-7 w-7 rounded border border-slate-300 text-sm font-medium text-slate-700 hover:bg-slate-100 disabled:opacity-40"
                      >
                        +
                      </button>
                    </div>
                    <p className="w-20 text-right font-semibold text-slate-900">
                      {formatINR(item.total)}
                    </p>
                    <button
                      onClick={() => removeCartItem(item.variant_id)}
                      disabled={loading}
                      className="text-xs font-medium text-red-600 hover:text-red-700 disabled:opacity-40"
                    >
                      Remove
                    </button>
                  </div>
                </li>
              ))}
            </ul>

            {renderSuggestionCard()}

            <div className="mt-4 flex items-center justify-between rounded-xl border border-slate-200 p-5">
              <p className="font-semibold text-slate-900">Subtotal</p>
              <p className="text-lg font-semibold text-slate-900">
                {formatINR(cart.subtotal)}
              </p>
            </div>
            <div className="mt-6 flex gap-3">
              <button
                onClick={() => setStep("catalog")}
                className="rounded-lg border border-slate-300 px-5 py-3 font-medium text-slate-700 hover:bg-slate-100"
              >
                Keep shopping
              </button>
              <button
                onClick={proceedToCheckout}
                disabled={loading || cart.items.length === 0}
                className="rounded-lg bg-black px-5 py-3 font-medium text-white transition hover:bg-slate-800 disabled:opacity-50"
              >
                Checkout
              </button>
            </div>
          </section>
        )}

        {step === "checkout" && order && (
          <section>
            <h2 className="mb-4 text-lg font-semibold text-slate-900">
              Confirm Order
            </h2>
            <div className="rounded-xl border border-slate-200 p-5">
              <div className="flex items-center justify-between">
                <div>
                  <p className="font-semibold text-slate-900">
                    Order {order.order_id}
                  </p>
                  <p className="mt-1 text-sm text-slate-500">
                    Status: {order.status}
                  </p>
                </div>
                <p className="text-lg font-semibold text-slate-900">
                  {formatINR(order.subtotal)}
                </p>
              </div>
            </div>
            <button
              onClick={startPayment}
              disabled={loading}
              className="mt-6 w-full rounded-xl bg-black px-5 py-3.5 font-medium text-white transition hover:bg-slate-800 disabled:opacity-50"
            >
              {loading ? "Processing..." : `Pay ${formatINR(order.subtotal)}`}
            </button>
          </section>
        )}

        {step === "approval" && (
          <section>
            <h2 className="mb-4 text-lg font-semibold text-slate-900">
              Purchase Requires Approval
            </h2>
            <div className="rounded-xl border border-amber-200 bg-amber-50 p-5">
              <p className="text-xs font-semibold uppercase tracking-wide text-amber-700">
                Level {approvalLevel} confirmation
              </p>
              <p className="mt-1 text-sm text-amber-900">
                This order is above the auto-approval threshold. An operator
                must approve it before payment can be initiated. {approvalReason}
              </p>
              {order && (
                <dl className="mt-4 grid gap-2 text-sm text-amber-900 sm:grid-cols-2">
                  <div><dt className="font-medium">Order</dt><dd>{order.order_id}</dd></div>
                  <div><dt className="font-medium">Total</dt><dd>{formatINR(order.subtotal)}</dd></div>
                  <div><dt className="font-medium">Approval request</dt><dd className="font-mono">{approvalRequestId}</dd></div>
                </dl>
              )}
            </div>
            <div className="mt-6 space-y-3">
              <button
                onClick={approveAndPay}
                disabled={loading}
                className="w-full rounded-xl bg-black px-5 py-3.5 font-medium text-white transition hover:bg-slate-800 disabled:opacity-50"
              >
                {loading ? "Approving..." : `Approve & Pay ${order ? formatINR(order.subtotal) : ""}`}
              </button>
              <button
                onClick={rejectApproval}
                disabled={loading}
                className="w-full rounded-xl border border-slate-300 px-5 py-3 font-medium text-slate-700 hover:bg-slate-100"
              >
                Reject
              </button>
            </div>
          </section>
        )}

        {step === "gate" && (
          <section>
            <div className="mb-6 rounded-xl border-2 border-red-600 bg-red-50 p-5">
              <p className="text-xs font-bold uppercase tracking-wide text-red-700">
                Level {approvalLevel} &middot; Hard Gate
              </p>
              <h2 className="mt-1 text-xl font-bold text-red-900">
                This purchase cannot proceed without your explicit, deliberate approval
              </h2>
              <p className="mt-2 text-sm text-red-800">
                {approvalReason} This screen cannot be dismissed or skipped &mdash;
                there is no background action, keyboard shortcut, or cached
                authorization that bypasses it, and the request below is
                re-verified against the server the instant you approve it.
              </p>
            </div>

            {approvalSnapshot && (
              <dl className="grid gap-2 rounded-xl border border-slate-200 p-5 text-sm text-slate-900 sm:grid-cols-2">
                <div><dt className="font-medium text-slate-500">Merchant</dt><dd>{approvalSnapshot.merchant}</dd></div>
                <div><dt className="font-medium text-slate-500">Amount</dt><dd>{formatINR(approvalSnapshot.amount)}</dd></div>
                <div className="sm:col-span-2"><dt className="font-medium text-slate-500">Items</dt><dd>{approvalSnapshot.items.join(", ")}</dd></div>
                <div><dt className="font-medium text-slate-500">Policy version</dt><dd className="font-mono">{approvalSnapshot.policy_version}</dd></div>
                <div><dt className="font-medium text-slate-500">Risk score</dt><dd>{approvalSnapshot.risk_score.toFixed(2)}</dd></div>
                <div className="sm:col-span-2"><dt className="font-medium text-slate-500">Approval request</dt><dd className="font-mono">{approvalRequestId}</dd></div>
              </dl>
            )}

            {gateError && (
              <div className="mt-4 rounded-lg border border-red-300 bg-red-50 p-4 text-sm text-red-800">
                {gateError}
              </div>
            )}

            <label className="mt-6 flex items-start gap-3 rounded-xl border border-slate-300 p-4 text-sm text-slate-800">
              <input
                type="checkbox"
                checked={gateConfirmed}
                onChange={(e) => setGateConfirmed(e.target.checked)}
                disabled={loading}
                className="mt-0.5 h-4 w-4"
              />
              I have reviewed the merchant, amount, and items above and I
              deliberately approve this exact purchase.
            </label>

            <div className="mt-4 space-y-3">
              <button
                onClick={approveGateAndPay}
                disabled={loading || !gateConfirmed}
                className="w-full rounded-xl bg-red-700 px-5 py-3.5 font-medium text-white transition hover:bg-red-800 disabled:opacity-50"
              >
                {loading ? "Re-verifying..." : `Approve this exact purchase \u2014 ${approvalSnapshot ? formatINR(approvalSnapshot.amount) : ""}`}
              </button>
              <button
                onClick={rejectApproval}
                disabled={loading}
                className="w-full rounded-xl border border-slate-300 px-5 py-3 font-medium text-slate-700 hover:bg-slate-100"
              >
                Reject
              </button>
              {gateError && (
                <button
                  onClick={backToOrderFromGate}
                  disabled={loading}
                  className="w-full rounded-xl border border-slate-300 px-5 py-3 font-medium text-slate-700 hover:bg-slate-100"
                >
                  Back to order
                </button>
              )}
            </div>
          </section>
        )}


        {step === "pay" && payment && (
          <section>
            <h2 className="mb-4 text-lg font-semibold text-slate-900">
              Complete Payment
            </h2>
            <p className="text-sm text-slate-500">
              The Razorpay checkout window should have opened. Complete the
              payment there to finish your order.
            </p>
            <div className="mt-4 rounded-xl border border-slate-200 p-5">
              <p className="text-sm text-slate-500">Amount due</p>
              <p className="text-2xl font-bold text-slate-900">
                {formatINR(payment.amount)}
              </p>
            </div>
            <button
              onClick={() => {
                setStep("catalog");
                setCartId(freshCartId());
                setCart(null);
                setOrder(null);
                setPayment(null);
                setRunId("");
                setRun(null);
                setMessage("Payment cancelled. Your cart was not charged.");
              }}
              className="mt-4 w-full rounded-xl border border-slate-300 px-5 py-3 font-medium text-slate-700 hover:bg-slate-100"
            >
              Cancel payment
            </button>
          </section>
        )}

	{step === "failed" && (
		<section>
			<h2 className="mb-4 text-lg font-semibold text-slate-900">
				Payment wasn&apos;t completed
			</h2>
			<div className="rounded-xl border border-amber-200 bg-amber-50 p-5">
				<p className="text-sm text-amber-900">
					{recovery ? recovery.safe_message : "Razorpay reported that the payment failed. Your order has not been charged twice. The cart remains reserved for 9 minutes."}
				</p>
			</div>

			{renderAuditTrail()}

			{(payment || (recovery && recovery.cart.subtotal > 0)) && (
				<div className="mt-6 rounded-xl border border-slate-200 p-5">
					<p className="text-sm text-slate-500">Amount due</p>
					<p className="text-2xl font-bold text-slate-900">
						{formatINR((payment ? payment.amount : recovery!.cart.subtotal) || 0)}
					</p>
				</div>
			)}

			{recovery && recovery.removable_items.length > 0 && (
				<div className="mt-6 rounded-xl border border-slate-200 p-5">
					<p className="text-sm font-semibold text-slate-900">Remove an item to reduce the total</p>
					<p className="mt-1 text-xs text-slate-500">
						Removing an item recomputes your total from the catalog and
						re-runs policy on the smaller order before payment.
					</p>
					<ul className="mt-3 divide-y divide-slate-200">
						{recovery.cart.items
							.filter((item) => recovery.removable_items.includes(item.variant_id))
							.map((item) => (
								<li key={item.variant_id} className="flex items-center justify-between py-3">
									<div>
										<p className="text-sm font-medium text-slate-900">{item.title}</p>
										<p className="text-xs text-slate-500">Qty {item.quantity} &middot; {formatINR(item.total)}</p>
									</div>
									<button
										onClick={() => removeAccessoryAndRetry(item.variant_id)}
										disabled={loading}
										className="rounded-lg border border-slate-300 px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-100 disabled:opacity-50"
									>
										Remove
									</button>
								</li>
							))}
					</ul>
				</div>
			)}

			<div className="mt-6 space-y-3">
				<button
					onClick={startPayment}
					disabled={loading || (recovery ? !recovery.retry_allowed : false)}
					className="w-full rounded-xl bg-black px-5 py-3 font-medium text-white transition hover:bg-slate-800 disabled:opacity-50"
				>
					{recovery && !recovery.retry_allowed ? "Reservation expired — start a new cart" : "Retry payment"}
				</button>
				<button
					onClick={() => {
						// Reopen the Razorpay method selector against THIS SAME
						// order -- Standard Checkout always shows all payment
						// methods when it opens, so re-proposing/relaunching for
						// the existing (idempotent) payment is what actually
						// changes the payment method. It must not discard the
						// cart: that would silently become a full restart.
						setMessage("Reopening payment method selection for this order...");
						startPayment();
					}}
					disabled={loading || (recovery ? !recovery.retry_allowed : false)}
					className="w-full rounded-xl border border-slate-300 px-5 py-3 font-medium text-slate-700 hover:bg-slate-100"
				>
					Change payment method
				</button>
				<button
					onClick={() => {
						setStep("catalog");
						setCartId(freshCartId());
						setCart(null);
						setOrder(null);
						setPayment(null);
						setRecovery(null);
						setRunId("");
						setRun(null);
						setMessage("Payment cancelled. Your cart was not charged.");
					}}
					className="w-full rounded-xl border border-slate-300 px-5 py-3 font-medium text-slate-700 hover:bg-slate-100"
				>
					Cancel
				</button>
			</div>
		</section>
	)}

	{step === "policy_rejected" && order && (
		<section>
			<h2 className="mb-4 text-lg font-semibold text-slate-900">
				Payment wasn&apos;t authorized
			</h2>
			<div className="rounded-xl border border-rose-200 bg-rose-50 p-5">
				<p className="text-sm text-rose-900">
					{policyRejectionReason}
				</p>
				<p className="mt-2 text-xs text-rose-700">
					No payment was attempted -- the policy engine rejected this
					purchase before any Razorpay call was made. Your cart is
					unaffected.
				</p>
			</div>

			{order.items.length > 1 && (
				<div className="mt-6 rounded-xl border border-slate-200 p-5">
					<p className="text-sm font-semibold text-slate-900">Remove an item and try again</p>
					<p className="mt-1 text-xs text-slate-500">
						Removing an item recomputes your total from the catalog and
						re-runs policy on the smaller order before payment.
					</p>
					<ul className="mt-3 divide-y divide-slate-200">
						{order.items.map((item) => (
							<li key={item.variant_id} className="flex items-center justify-between py-3">
								<div>
									<p className="text-sm font-medium text-slate-900">{item.title}</p>
									<p className="text-xs text-slate-500">Qty {item.quantity} &middot; {formatINR(item.total)}</p>
								</div>
								<button
									onClick={() => removeAccessoryAndRetry(item.variant_id)}
									disabled={loading}
									className="rounded-lg border border-slate-300 px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-100 disabled:opacity-50"
								>
									Remove
								</button>
							</li>
						))}
					</ul>
				</div>
			)}

			<div className="mt-6 space-y-3">
				<button
					onClick={() => resetToCatalog("Purchase not authorized. Your cart was not charged.")}
					className="w-full rounded-xl bg-black px-5 py-3 font-medium text-white transition hover:bg-slate-800"
				>
					Return to catalog
				</button>
			</div>
		</section>
	)}

        {step === "complete" && payment && (
          <section>
            <div className="rounded-xl border border-slate-200 p-6">
              <p className="text-sm text-slate-500">Payment status</p>
              <p className="mt-1 text-xl font-bold text-slate-900">
                {payment.status}
              </p>
              <p className="mt-4 text-sm text-slate-500">
                Payment ID: {payment.payment_id}
              </p>
              <p className="text-sm text-slate-500">
                Order ID: {payment.order_id}
              </p>
            </div>
            {renderAuditTrail()}
            {order && order.items.length > 0 && (
              <div className="mt-6 rounded-xl border border-slate-200 p-6">
                <p className="text-sm font-semibold text-slate-900">Rate your order</p>
                <p className="mt-1 text-xs text-slate-500">
                  Optional -- helps other buyers, and the seller sees it too.
                </p>
                <ul className="mt-4 divide-y divide-slate-100">
                  {order.items.map((item) => {
                    const entry: ReviewEntry = reviews[item.product_id] ?? {
                      rating: 0,
                      comment: "",
                      submitting: false,
                      submitted: false,
                      error: "",
                    };
                    return (
                      <li key={item.product_id} className="py-4 first:pt-0 last:pb-0">
                        <p className="text-sm font-medium text-slate-900">{item.title}</p>
                        {entry.submitted ? (
                          <p className="mt-2 text-sm text-emerald-700">Thanks for your review!</p>
                        ) : (
                          <>
                            <div className="mt-2 flex gap-1">
                              {[1, 2, 3, 4, 5].map((n) => (
                                <button
                                  key={n}
                                  type="button"
                                  onClick={() => rateProduct(item.product_id, n)}
                                  aria-label={`Rate ${item.title} ${n} star${n > 1 ? "s" : ""}`}
                                  className={`text-lg leading-none ${
                                    n <= entry.rating ? "text-amber-500" : "text-slate-300"
                                  }`}
                                >
                                  ★
                                </button>
                              ))}
                            </div>
                            <textarea
                              value={entry.comment}
                              onChange={(e) => commentOnProduct(item.product_id, e.target.value)}
                              placeholder="Optional comment"
                              rows={2}
                              className="mt-2 w-full rounded-lg border border-slate-200 px-3 py-2 text-sm text-slate-900 focus:border-slate-400 focus:outline-none"
                            />
                            {entry.error && (
                              <p className="mt-1 text-xs text-rose-600">{entry.error}</p>
                            )}
                            <button
                              onClick={() => submitReview(order.order_id, item.product_id)}
                              disabled={entry.rating < 1 || entry.submitting}
                              className="mt-2 rounded-lg bg-slate-900 px-3 py-1.5 text-xs font-medium text-white transition hover:bg-slate-800 disabled:opacity-40"
                            >
                              {entry.submitting ? "Submitting..." : "Submit review"}
                            </button>
                          </>
                        )}
                      </li>
                    );
                  })}
                </ul>
              </div>
            )}

            {postCheckoutSuggestionLoading && (
              <div className="mt-6 rounded-xl border border-indigo-200 bg-indigo-50 p-6">
                <Skeleton className="h-3 w-28" />
                <Skeleton className="mt-2 h-4 w-2/3" />
                <Skeleton className="mt-3 h-9 w-40" />
              </div>
            )}
            {!postCheckoutSuggestionLoading && postCheckoutSuggestion?.product && (
              <div className="mt-6 rounded-xl border border-indigo-200 bg-indigo-50 p-6">
                <p className="text-xs font-medium uppercase tracking-wide text-indigo-700">
                  Complete the set
                </p>
                <p className="mt-1 font-semibold text-slate-900">
                  Add {postCheckoutSuggestion.product.title} -- {formatINR(postCheckoutSuggestion.product.price)}
                </p>
                {postCheckoutSuggestion.recommendation && (
                  <p className="mt-1 text-sm text-indigo-800">{postCheckoutSuggestion.recommendation.reason}</p>
                )}
                <button
                  onClick={acceptPostCheckoutSuggestion}
                  disabled={loading}
                  className="mt-3 rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
                >
                  Add -- starts a new order
                </button>
              </div>
            )}

            <button
              onClick={() => {
                setStep("catalog");
                setCartId(freshCartId());
                setCart(null);
                setOrder(null);
                setPayment(null);
                setRunId("");
                setRun(null);
                setMessage("");
                setReviews({});
                setPostCheckoutSuggestion(null);
              }}
              className="mt-6 w-full rounded-xl bg-black px-5 py-3.5 font-medium text-white transition hover:bg-slate-800"
            >
              Start a new order
            </button>
          </section>
        )}

        {step === "orders" && (
          <section>
            {ordersLoading && (
              <div className="space-y-4">
                {[0, 1, 2].map((i) => (
                  <Skeleton key={i} className="h-24 w-full" />
                ))}
              </div>
            )}
            {!ordersLoading && ordersError && (
              <p className="text-sm text-rose-700">
                {ordersError} --{" "}
                <button
                  onClick={fetchOrders}
                  className="underline underline-offset-2 hover:text-rose-900"
                >
                  try again
                </button>
              </p>
            )}
            {!ordersLoading && !ordersError && orders.length === 0 && (
              <p className="text-sm text-slate-500">
                No orders yet -- completed checkouts will show up here.
              </p>
            )}
            <ul className="space-y-4">
              {orders.map((o) => (
                <li key={o.order_id} className="rounded-xl border border-slate-200 p-5">
                  <div className="flex items-start justify-between">
                    <div>
                      <p className="font-semibold text-slate-900">{o.order_id}</p>
                      {o.created_at && (
                        <p className="text-xs text-slate-500">
                          {new Date(o.created_at).toLocaleString()}
                        </p>
                      )}
                    </div>
                    <div className="text-right">
                      <p className="text-xs font-medium uppercase tracking-wide text-slate-500">
                        {o.status}
                      </p>
                      <p className="font-semibold text-slate-900">{formatINR(o.subtotal)}</p>
                    </div>
                  </div>
                  <ul className="mt-3 divide-y divide-slate-100 border-t border-slate-100 pt-3">
                    {o.items.map((item) => (
                      <li
                        key={item.variant_id}
                        className="flex items-center justify-between py-1.5 text-sm"
                      >
                        <span className="text-slate-700">
                          {item.title} × {item.quantity}
                        </span>
                        <span className="text-slate-500">{formatINR(item.total)}</span>
                      </li>
                    ))}
                  </ul>
                </li>
              ))}
            </ul>
          </section>
        )}

        <p className="mt-8 text-center text-xs text-slate-400">
          Razorpay Test Mode
        </p>
      </div>
    </main>
  );
}
