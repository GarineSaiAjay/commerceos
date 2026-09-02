"use client";

import { Skeleton } from "../../lib/format";
import type { AgentChatMessage, AlternativeProduct, CheckoutPlan, LoopStep, SuggestResponse } from "./types";
import { formatINR } from "./helpers";
import { SuggestionCard } from "./SuggestionCard";

// "Ask the shopping agent" card at the top of the catalog screen --
// conversational entry point over POST /agent/checkout. The agent only
// ever returns a proposal (a selected product_id plus its reasoning) --
// it never creates a cart or moves money itself; accepting the
// proposal just calls the same addToCart the manual catalog uses, so
// the normal cart/policy/payment pipeline still runs unchanged.
//
// Extracted from checkout.tsx's catalog-step JSX as part of item 21
// (PLAN-04-UI-UX-AND-LATENCY.md §A2) -- same JSX, moved; all handlers
// (askAgent/acceptAgentPlan/chooseAlternative/dismiss) are now explicit
// callback props instead of closures over CheckoutFlow's state.
export function AgentChatPanel({
  agentHistory,
  agentPrompt,
  onAgentPromptChange,
  onAsk,
  agentLoading,
  agentError,
  agentPlan,
  agentSteps,
  loading,
  onAcceptPlan,
  onDismissPlan,
  onChooseAlternative,
  suggestion,
  suggestionLoading,
  onAcceptSuggestion,
  onDismissSuggestion,
}: {
  agentHistory: AgentChatMessage[];
  agentPrompt: string;
  onAgentPromptChange: (value: string) => void;
  onAsk: () => void;
  agentLoading: boolean;
  agentError: string;
  agentPlan: CheckoutPlan | null;
  // agentSteps is the bounded tool-calling loop's (item 18,
  // PLAN-01-AGENTIC-CORE.md §2) turn-by-turn trace for the most recent
  // reply -- search/inspect/recommend tool calls, not just the final
  // proposal or clarifying question. Always [] when the /agent/checkout
  // single-shot fallback answered instead (see checkout.tsx's askAgent).
  agentSteps: LoopStep[];
  loading: boolean;
  onAcceptPlan: () => void;
  onDismissPlan: () => void;
  onChooseAlternative: (alt: AlternativeProduct) => void;
  // suggestion/suggestionLoading/onAcceptSuggestion/onDismissSuggestion
  // (PLAN-03-PROACTIVE-GROWTH-AGENT.md §2) are the exact same cart-level
  // cross-sell state/handlers CartPanel and the catalog-step
  // SuggestionCard already use -- reused here, not refetched, so this
  // never issues a second /growth/suggest call (which would double-
  // count a real growth.RecordImpression metric). See the crossSellCard
  // block below for how a chat message becomes "live" for this card.
  suggestion: SuggestResponse | null;
  suggestionLoading: boolean;
  onAcceptSuggestion: () => void;
  onDismissSuggestion: () => void;
}) {
  return (
    <div className="mb-6 rounded-xl border border-slate-200 bg-slate-50 p-5">
      <h2 className="mb-1 text-sm font-semibold uppercase tracking-wide text-slate-500">
        Ask the shopping agent
      </h2>
      <p className="mb-3 text-sm text-slate-600">
        Say what you want and the budget. It reads the catalog and proposes one item -- it never places an order itself; the normal checkout below still runs.
      </p>

      {agentHistory.length > 0 && (
        <div className="mb-3 max-h-64 space-y-2 overflow-y-auto rounded-lg border border-slate-200 bg-white p-3">
          {agentHistory.map((msg, i) => {
            // crossSellCard: this message is the one askAgent's cart-
            // mutation-triggered fetchSuggestion() labeled with a
            // product_id (PLAN-03-PROACTIVE-GROWTH-AGENT.md §2), AND
            // that product is still the live `suggestion` state -- so
            // accept/dismiss always act on the real, current suggestion,
            // never a frozen copy that could go stale (e.g. after a
            // dismiss server-side, or a second unrelated cross-sell
            // replacing it). Reuses the exact same suggestion state and
            // handlers CartPanel's own card does -- rendering this never
            // triggers a second /growth/suggest call.
            const crossSellCard =
              msg.role === "assistant" &&
              !!msg.crossSellProductId &&
              suggestion?.available &&
              suggestion.product?.product_id === msg.crossSellProductId;
            return (
              <div key={i}>
                <p className="text-sm">
                  <span className="font-medium text-slate-500">
                    {msg.role === "user" ? "You: " : "Agent: "}
                  </span>
                  <span className="text-slate-700">{msg.content}</span>
                </p>
                {crossSellCard && (
                  <SuggestionCard
                    suggestion={suggestion}
                    suggestionLoading={suggestionLoading}
                    loading={loading}
                    optimistic={null}
                    onAccept={onAcceptSuggestion}
                    onDismiss={onDismissSuggestion}
                  />
                )}
              </div>
            );
          })}
        </div>
      )}

      <div className="flex gap-2">
        <input
          type="text"
          value={agentPrompt}
          onChange={(e) => onAgentPromptChange(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") onAsk();
          }}
          placeholder="earbuds for my sister, budget 25000, good battery life"
          aria-label="Message the shopping agent"
          className="flex-1 rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-slate-500 focus:outline-none"
          disabled={agentLoading}
        />
        <button
          onClick={onAsk}
          disabled={agentLoading || !agentPrompt.trim()}
          className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-800 disabled:opacity-50"
        >
          {agentLoading ? "Thinking..." : "Ask"}
        </button>
      </div>

      {/* item 28 (P2, PLAN-04-UI-UX-AND-LATENCY.md §A5): "form inputs
          like the agent prompt box should gain aria-live on the
          response region so screen readers announce the agent's
          proposal." The live region has to already exist in the
          accessibility tree BEFORE its content changes for most screen
          readers to reliably announce the change -- a <div
          aria-live="polite"> that itself only gets inserted at the
          same moment as the content isn't guaranteed to be picked up.
          So this wrapper renders unconditionally; only what's inside
          it (error / loading / proposal) is conditional. */}
      <div aria-live="polite" aria-atomic="true">
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
          <div className="mt-4 animate-fade-in rounded-lg border border-slate-300 bg-white p-4">
            <p className="text-xs font-medium uppercase tracking-wide text-slate-500">
              Agent proposes
            </p>
            <p className="mt-1 text-sm text-slate-700">{agentPlan.reasoning}</p>
            <div className="mt-3 flex gap-3">
              <button
                onClick={onAcceptPlan}
                disabled={loading}
                className="rounded-lg bg-black px-4 py-2 text-sm font-medium text-white hover:bg-slate-800 disabled:opacity-50"
              >
                Add to cart
              </button>
              <button
                onClick={onDismissPlan}
                className="rounded-lg border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-100"
              >
                Never mind
              </button>
            </div>
            {agentPlan.alternatives && agentPlan.alternatives.length > 0 && (
              <div className="mt-3 border-t border-slate-100 pt-3">
                <p className="text-xs text-slate-500">Or:</p>
                <ul className="mt-1 space-y-1">
                  {agentPlan.alternatives.map((alt) => (
                    <li key={alt.product_id}>
                      <button
                        onClick={() => onChooseAlternative(alt)}
                        disabled={loading}
                        className="text-sm font-medium text-slate-700 underline underline-offset-4 hover:text-slate-900 disabled:opacity-50"
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

        {agentSteps.length > 0 && (
          <details className="mt-3 rounded-lg border border-slate-200 bg-white px-3 py-2 text-xs">
            <summary className="cursor-pointer font-medium text-slate-500">
              Agent reasoning trail ({agentSteps.length} step{agentSteps.length === 1 ? "" : "s"})
            </summary>
            <ul className="mt-2 space-y-1.5">
              {agentSteps.map((s, i) => (
                <li key={i} className="flex items-start gap-2 text-slate-600">
                  <span className="mt-1 h-1 w-1 flex-shrink-0 rounded-full bg-slate-400" />
                  <span>
                    <span className="font-medium capitalize text-slate-500">
                      {s.type.replace(/_/g, " ")}:
                    </span>{" "}
                    {s.detail}
                  </span>
                </li>
              ))}
            </ul>
          </details>
        )}
      </div>
    </div>
  );
}
