// item 29 (P2, PLAN-04-UI-UX-AND-LATENCY.md section B3): "Optimistic
// client-side pre-score for cross-sell." The authoritative
// recommendation (budget-gated, policy-adjacent EV score, persisted)
// must stay server-side -- POST /growth/suggest always wins and this
// file's guess is discarded the moment that response lands. But the
// full catalog (with every product's use_cases/compatibility/features
// tags) is already fetched client-side, and the exact same tag-overlap
// scoring backend/growth/suggest.go's bestCandidate runs is cheap
// enough to duplicate here as a pure function purely so the UI has
// *something* to show instantly instead of a blank skeleton for the
// ~one network round trip POST /growth/suggest takes. This file never
// decides what's actually offered or gated -- it only ever produces a
// tentative, non-interactive "maybe" guess (see SuggestionCard.tsx's
// handling of the `optimistic` prop) that checkout.tsx replaces with
// the real server response as soon as it arrives.
//
// Deliberately NOT a copy-paste of growth/suggest.go's bestCandidate:
// this app is single-merchant throughout (see e.g.
// backend/policy/engine.go's isTrustedMerchant, or
// backend/policy/postgres_repository.go's policySettingsMerchantID),
// and the client-side Product type (./types.ts) doesn't even carry a
// merchant field for the same reason -- so the merchant-match filter
// bestCandidate applies server-side has no client-side equivalent to
// duplicate. Everything else (tag-overlap scoring across use_cases +
// compatibility + features, skip out-of-stock/excluded, tie-break
// toward the cheaper item) is mirrored exactly.
import type { Product } from "./types";

function buildSignals(products: Product[]): Set<string> {
  const signals = new Set<string>();
  for (const product of products) {
    for (const tag of product.use_cases ?? []) signals.add(tag);
    for (const tag of product.compatibility ?? []) signals.add(tag);
    for (const tag of product.features ?? []) signals.add(tag);
  }
  return signals;
}

function overlapScore(product: Product, signals: Set<string>): number {
  let score = 0;
  for (const tag of product.use_cases ?? []) {
    if (signals.has(tag)) score++;
  }
  for (const tag of product.compatibility ?? []) {
    if (signals.has(tag)) score++;
  }
  for (const tag of product.features ?? []) {
    if (signals.has(tag)) score++;
  }
  return score;
}

// bestCandidateClientSide picks the same winner growth/suggest.go's
// bestCandidate would for a cart-based suggestion, given the same
// catalog snapshot: highest tag-overlap score against signalProducts'
// aggregate use_cases/compatibility/features, skipping anything in
// exclude or currently out of stock, ties broken toward the cheaper
// item. Returns null when nothing overlaps at all -- the same "safe
// no-op, don't guess" posture bestCandidate itself takes (see that
// function's doc comment), so a wrong guess is never shown; only
// "nothing to show yet" ever is.
export function bestCandidateClientSide(
  catalogProducts: Product[],
  signalProducts: Product[],
  exclude: Set<string>,
): Product | null {
  const signals = buildSignals(signalProducts);
  let best: Product | null = null;
  let bestScore = 0;

  for (const product of catalogProducts) {
    if (exclude.has(product.product_id) || product.availability <= 0) continue;
    const score = overlapScore(product, signals);
    if (score === 0) continue;
    if (!best || score > bestScore || (score === bestScore && product.price.amount < best.price.amount)) {
      best = product;
      bestScore = score;
    }
  }

  return best;
}
