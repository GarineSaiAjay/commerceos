"use client";

import { useEffect, useState } from "react";
import { Skeleton } from "../../lib/format";
import type { Product, SuggestResponse } from "./types";
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
//
// Item 28 (P2, PLAN-04-UI-UX-AND-LATENCY.md §A4) added the "enter/exit"
// transition named there specifically for this card. Entrance is a
// plain CSS @keyframes animation (.animate-fade-in, globals.css) that
// plays automatically on mount -- nothing to manage here. Exit is the
// harder half: checkout.tsx clears its `suggestion` state to null the
// instant accept/dismiss resolves, which would otherwise unmount this
// component before any fade-out could ever play. Rather than touching
// checkout.tsx's state machine for a purely decorative change, this
// component keeps its own snapshot of "what it was last showing" and a
// three-phase state machine (hidden / visible / leaving): the moment
// the incoming props stop asking for a visible card, it keeps
// rendering that snapshot for one more transition duration at reduced
// opacity, then actually unmounts. Self-contained -- no other file
// needs to know this happens.
//
// Item 29 (P2, PLAN-04-UI-UX-AND-LATENCY.md §B3) added the `optimistic`
// prop: while suggestionLoading is true and the real POST
// /growth/suggest response hasn't landed yet, a client-computed guess
// (checkout/optimisticSuggest.ts) is shown instead of the bare
// skeleton, as a tentative, NON-interactive "Maybe" card -- no Add to
// cart/No thanks buttons, since it hasn't been through the server's
// budget/EV check and is only ever a perceived-latency placeholder.
// The real, fully-interactive card above always replaces it the moment
// the authoritative response arrives; see optimisticSuggest.ts's own
// doc comment for why "the server always wins" here.
export const EXIT_DURATION_MS = 150;

type Phase = "hidden" | "visible" | "leaving";

export function SuggestionCard({
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
  const visible = suggestionLoading || !!(suggestion?.available && suggestion.product);

  const [snapshot, setSnapshot] = useState({ suggestion, suggestionLoading, optimistic });
  const [prevVisible, setPrevVisible] = useState(visible);
  const [phase, setPhase] = useState<Phase>(visible ? "visible" : "hidden");

  // Adjust state during render in response to the visible prop changing
  // (React's own documented pattern for this -- see "Adjusting some
  // state when a prop changes" at
  // https://react.dev/learn/you-might-not-need-an-effect) rather than a
  // useEffect: entering the "visible" phase this way costs no extra
  // render pass, since the snapshot is already current in the very
  // render that first shows it. Only the timed exit below needs a real
  // effect (a setTimeout is a genuine side effect).
  if (visible !== prevVisible) {
    setPrevVisible(visible);
    if (visible) {
      setSnapshot({ suggestion, suggestionLoading, optimistic });
      setPhase("visible");
    } else if (phase !== "hidden") {
      setPhase("leaving");
    }
  }

  useEffect(() => {
    if (phase !== "leaving") return;
    const timer = setTimeout(() => setPhase("hidden"), EXIT_DURATION_MS);
    return () => clearTimeout(timer);
  }, [phase]);

  if (phase === "hidden") return null;

  const exitClass = phase === "leaving" ? "opacity-0" : "opacity-100";

  if (snapshot.suggestion?.available && snapshot.suggestion.product) {
    const product = snapshot.suggestion.product;
    const recommendation = snapshot.suggestion.recommendation;
    return (
      <div
        className={`mt-4 animate-fade-in rounded-2xl border border-indigo-200 bg-gradient-to-br from-indigo-50 to-white p-5 shadow-sm transition-opacity duration-150 ${exitClass}`}
      >
        <div className="flex items-center gap-1.5">
          <span
            aria-hidden
            className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-indigo-600 text-white"
          >
            <svg viewBox="0 0 24 24" fill="currentColor" className="h-3 w-3">
              <path d="M12 2l1.8 6.2L20 10l-6.2 1.8L12 18l-1.8-6.2L4 10l6.2-1.8L12 2z" />
            </svg>
          </span>
          <p className="text-xs font-semibold uppercase tracking-wide text-indigo-700">
            Agent suggests
          </p>
        </div>
        <p className="mt-2 font-semibold text-slate-900">
          Add {product.title} -- {formatINR(product.price)}
        </p>
        {recommendation && (
          <p className="mt-1 text-sm text-indigo-800">{recommendation.reason}</p>
        )}
        <div className="mt-3 flex gap-3">
          <button
            onClick={onAccept}
            disabled={loading || phase === "leaving"}
            className="rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white shadow-sm transition hover:bg-indigo-700 hover:shadow disabled:opacity-50 disabled:shadow-none"
          >
            Add to cart
          </button>
          <button
            onClick={onDismiss}
            disabled={phase === "leaving"}
            className="rounded-lg border border-indigo-300 bg-white px-4 py-2 text-sm font-medium text-indigo-800 transition hover:bg-indigo-100 disabled:opacity-50"
          >
            No thanks
          </button>
        </div>
      </div>
    );
  }
  if (snapshot.suggestionLoading) {
    if (snapshot.optimistic) {
      return (
        <div
          className={`mt-4 animate-fade-in rounded-2xl border border-indigo-100 bg-indigo-50 p-5 shadow-sm transition-opacity duration-150 ${exitClass}`}
        >
          <div className="flex items-center gap-2">
            <span aria-hidden className="h-2 w-2 shrink-0 animate-pulse rounded-full bg-indigo-400" />
            <p className="text-xs font-semibold uppercase tracking-wide text-indigo-500">
              Checking for a match
            </p>
          </div>
          <p className="mt-2 font-medium text-slate-600">
            Maybe {snapshot.optimistic.title} -- {formatINR(snapshot.optimistic.price.amount)}
          </p>
        </div>
      );
    }
    return (
      <div
        className={`mt-4 animate-fade-in rounded-2xl border border-indigo-200 bg-indigo-50 p-5 shadow-sm transition-opacity duration-150 ${exitClass}`}
      >
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
