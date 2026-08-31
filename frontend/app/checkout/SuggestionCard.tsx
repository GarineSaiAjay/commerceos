"use client";

import { Skeleton } from "../../lib/format";
import type { SuggestResponse } from "./types";
import { formatINR } from "./helpers";

// Cross-sell card, shared between the catalog and cart screens so a
// computed suggestion stays visible regardless of which one the buyer
// is on -- previously this only rendered inside the cart screen, so a
// buyer who went back to "Keep shopping" (or arrived at a suggestion
// via the agent chat, which also lands them on the cart screen only
// momentarily) had no way to see it again without navigating back into
// the cart. See files/PLAN-03-PROACTIVE-GROWTH-AGENT.md §1-2.
//
// Extracted from checkout.tsx's renderSuggestionCard() as part of item
// 21 (PLAN-04-UI-UX-AND-LATENCY.md §A2) -- same JSX, moved;
// accept/dismiss are now explicit callback props instead of closures
// over CheckoutFlow's acceptSuggestion/dismissSuggestion.
export function SuggestionCard({
  suggestion,
  suggestionLoading,
  loading,
  onAccept,
  onDismiss,
}: {
  suggestion: SuggestResponse | null;
  suggestionLoading: boolean;
  loading: boolean;
  onAccept: () => void;
  onDismiss: () => void;
}) {
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
            onClick={onAccept}
            disabled={loading}
            className="rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
          >
            Add to cart
          </button>
          <button
            onClick={onDismiss}
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
