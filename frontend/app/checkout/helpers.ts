import type { Product, ProductVariant } from "./types";

// Shared helpers for the checkout flow (item 21 split,
// PLAN-04-UI-UX-AND-LATENCY.md §A2). Moved verbatim from checkout.tsx.

export const MERCHANT_ID = "merchant_001";

// Persisted so a hard reload restores whatever the buyer was still
// shopping (see CheckoutFlow's restore/persist effects). Only ever
// holds an ACTIVE cart's ID -- GET /carts/{id} now 404s for a
// checked-out cart (backend/commerce/cart/service.go), so a stale ID
// from a completed order is never resurrected.
export const CART_STORAGE_KEY = "commerceos_cart_id";

// Each order uses a fresh cart ID. A cart is single-use: once it is
// checked out it is marked `checked_out` and can never be reused, so a
// fixed ID would leave the UI stuck on a stale, already-checked-out cart.
export function freshCartId() {
  return `cart_${Date.now()}`;
}

// variantLabel derives a short, human label for a variant picker option
// (item 10). It checks the variant's OWN attributes first (color,
// length_m, coverage_years -- the only label-bearing keys any seeded
// variant currently uses). A "-default" variant seeded before item 10
// existed carries none of these on itself (e.g. AppleCare's original
// 2-year row, the cable's original 1m row) -- rather than edit those
// already-seeded rows, this falls back to the same keys on the PARENT
// PRODUCT's own attributes, which already had them. "Standard" is the
// last resort for a product with no differentiating attributes at all.
export function variantLabel(variant: ProductVariant, product: Product): string {
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
// points in CheckoutFlow all go through this, not the picker). Prefers
// the exact "<id>-default" entry every product is guaranteed to have;
// falls back to whatever variant happens to be first if that's somehow
// missing, and finally to the old hardcoded-guess string so a product
// object built from a partial agent/suggestion payload (no variants
// array at all -- see CheckoutFlow's chooseAlternative/acceptSuggestion
// synthetic `matched` fallback objects) still resolves to something.
export function defaultVariantFor(product: Product): string {
  const exact = product.variants?.find((v) => v.variant_id === `${product.product_id}-default`);
  if (exact) return exact.variant_id;
  if (product.variants && product.variants.length > 0) return product.variants[0].variant_id;
  return `${product.product_id}-default`;
}

// NOTE (item 21 split): this is a DELIBERATE duplicate of
// frontend/lib/format.tsx's formatINR, not a shared import -- the two
// are NOT behaviorally identical. lib/format.tsx uses Intl.NumberFormat
// with Indian digit grouping (e.g. "₹1,234.56"); this one is the plain
// `(amount/100).toFixed(2)` checkout.tsx has always used (e.g.
// "₹1234.56", no grouping). This duplication predates this split -- it
// was already present in the pre-split checkout.tsx -- and
// consolidating the two would be a visible display change (adding
// digit grouping to every price on this page), which is out of scope
// for a structural split that must not alter behavior. Moved verbatim.
export function formatINR(amount: number) {
  return `₹${(amount / 100).toFixed(2)}`;
}
