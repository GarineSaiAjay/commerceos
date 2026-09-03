"use client";

import { useEffect, useState } from "react";
import type { Product, SuggestResponse } from "./types";
import { SuggestionCard, EXIT_DURATION_MS } from "./SuggestionCard";

// PLAN-03-PROACTIVE-GROWTH-AGENT.md §1's second half of the "decouple
// fetch from step" fix -- and the half that was actually missed. Once
// the cart-mutation effect in checkout.tsx started re-fetching a
// suggestion regardless of which screen the buyer was on, the *same
// full* SuggestionCard the cart screen shows started rendering
// unconditionally on the catalog screen too: exactly "a full copy of
// the cart's suggestion card floating over the catalog (that would be
// exactly the nagging pattern to avoid)" that §1 explicitly warned
// against. Concretely: dismiss (or ignore) a suggestion on the cart
// screen, click "Keep shopping," and the identical card reappears over
// the catalog with no interaction from the buyer at all.
//
// This wraps SuggestionCard for the catalog screen only (CartPanel.tsx
// keeps rendering SuggestionCard directly -- a cross-sell offer while
// looking at your cart is on-topic, not nagging): a small, non-modal
// badge is shown instead of the full card; the full card only ever
// appears once the buyer deliberately clicks it. Matches the plan's own
// phrasing -- "a persistent, small badge... that expands the existing
// suggestion card only on click."
export function SuggestionBadge({
  suggestion,
  suggestionLoading,
  loading,
  optimistic,
  onAccept,
  onDismiss,
}: {
  suggestion: SuggestResponse | null;
  suggestionLoading: boolean;
  loading: boolean;
  optimistic: Product | null;
  onAccept: () => void;
  onDismiss: () => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const hasSuggestion = !!(suggestion?.available && suggestion.product);
  // Same condition SuggestionCard itself uses to decide whether it has
  // anything to show at all (a real suggestion, or a loading/optimistic
  // placeholder) -- the badge should exist exactly when the card it
  // stands in for would.
  const hasAnything = suggestionLoading || hasSuggestion;

  // Once expanded, keep rendering the real SuggestionCard even after
  // hasAnything goes false (accepted, dismissed, or excluded from a
  // later fetch) -- SuggestionCard has its own internal snapshot+phase
  // state that fades itself out over EXIT_DURATION_MS before returning
  // null. Collapsing this wrapper back to the badge (or to nothing)
  // immediately, instead, would unmount SuggestionCard mid-fade and
  // lose that animation -- so this waits the same EXIT_DURATION_MS
  // before flipping `expanded` back, matching SuggestionCard's own
  // timing exactly rather than guessing at a different delay.
  useEffect(() => {
    if (expanded && !hasAnything) {
      const timer = setTimeout(() => setExpanded(false), EXIT_DURATION_MS);
      return () => clearTimeout(timer);
    }
  }, [expanded, hasAnything]);

  if (expanded) {
    return (
      <SuggestionCard
        suggestion={suggestion}
        suggestionLoading={suggestionLoading}
        loading={loading}
        optimistic={optimistic}
        onAccept={onAccept}
        onDismiss={onDismiss}
      />
    );
  }

  if (!hasAnything) return null;

  const label = hasSuggestion
    ? `Agent suggests: ${suggestion!.product!.title}`
    : optimistic
      ? `Maybe: ${optimistic.title}`
      : "Agent has a suggestion";

  return (
    <button
      type="button"
      onClick={() => setExpanded(true)}
      className="mt-4 flex w-full animate-fade-in items-center gap-2 rounded-full border border-indigo-200 bg-indigo-50 px-4 py-2 text-left text-sm font-medium text-indigo-800 transition-colors hover:bg-indigo-100"
    >
      <span aria-hidden className="h-2 w-2 shrink-0 rounded-full bg-indigo-500" />
      <span className="truncate">{label}</span>
    </button>
  );
}
