"use client";

import { Skeleton } from "../../lib/format";
import type { AgentChatMessage, AlternativeProduct, CheckoutPlan } from "./types";
import { formatINR } from "./helpers";

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
  loading,
  onAcceptPlan,
  onDismissPlan,
  onChooseAlternative,
}: {
  agentHistory: AgentChatMessage[];
  agentPrompt: string;
  onAgentPromptChange: (value: string) => void;
  onAsk: () => void;
  agentLoading: boolean;
  agentError: string;
  agentPlan: CheckoutPlan | null;
  loading: boolean;
  onAcceptPlan: () => void;
  onDismissPlan: () => void;
  onChooseAlternative: (alt: AlternativeProduct) => void;
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
          onChange={(e) => onAgentPromptChange(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") onAsk();
          }}
          placeholder="earbuds for my sister, budget 25000, good battery life"
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
            <div className="mt-3 border-t border-zinc-100 pt-3">
              <p className="text-xs text-zinc-500">Or:</p>
              <ul className="mt-1 space-y-1">
                {agentPlan.alternatives.map((alt) => (
                  <li key={alt.product_id}>
                    <button
                      onClick={() => onChooseAlternative(alt)}
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
  );
}
