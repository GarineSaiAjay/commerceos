"use client";

import type { AgentChatMessage, AlternativeProduct, CheckoutPlan, LoopStep, SuggestResponse } from "./types";
import { formatINR } from "./helpers";
import { SuggestionCard } from "./SuggestionCard";

// SourceBadge makes visible what was previously silent and identical
// either way: whether a proposal came from a real LLM call or the
// keyword-matching deterministic fallback (backend/agents/intent.go's
// Intent.Source). Renders nothing for an unset/unrecognized source --
// e.g. a cached response from before this field existed -- rather than
// showing a broken or misleading badge. See
// files/AGENTIC-INTEGRITY-AUDIT-2026-09-04.md, Finding C.
function SourceBadge({ source }: { source?: string }) {
  if (source === "llm") {
    return (
      <span className="inline-flex items-center rounded-full bg-emerald-50 px-2 py-0.5 text-[10px] font-medium uppercase tracking-wide text-emerald-700">
        AI-reasoned
      </span>
    );
  }
  if (source === "deterministic") {
    return (
      <span
        className="inline-flex items-center rounded-full bg-amber-50 px-2 py-0.5 text-[10px] font-medium uppercase tracking-wide text-amber-700"
        title="The AI model wasn't available for this answer, so a rule-based keyword match was used instead."
      >
        Rule-based fallback
      </span>
    );
  }
  return null;
}

// "Ask the shopping agent" card at the top of the catalog screen --
// conversational entry point over POST /agent/loop (falling back to
// /agent/checkout). The agent only ever returns a proposal (a selected
// product_id plus its reasoning) -- it never creates a cart or moves
// money itself; accepting the proposal just calls the same addToCart
// the manual catalog uses, so the normal cart/policy/payment pipeline
// still runs unchanged.
//
// Extracted from checkout.tsx's catalog-step JSX as part of item 21
// (PLAN-04-UI-UX-AND-LATENCY.md §A2) -- same JSX, moved; all handlers
// (askAgent/acceptAgentPlan/chooseAlternative/dismiss) are explicit
// callback props instead of closures over CheckoutFlow's state.
//
// Redesigned (UI/UX pass, prompted by a real transcript where the
// "temporarily unavailable" fallback message appeared twice in a row --
// once as a plain history line, once as a second unlabeled paragraph
// below the input) into a proper chat-bubble transcript:
//  - example prompt chips when the conversation is empty, so a first-
//    time buyer doesn't stare at a blank input;
//  - user/assistant messages as distinct bubbles, with a third amber
//    "error" bubble style (AgentChatMessage.isError) that carries its
//    own inline "Try again" affordance instead of a second, separate
//    error paragraph;
//  - the proposal card and reasoning-trail <details> now attach to the
//    last assistant bubble (mirroring the existing crossSellCard
//    attachment pattern below) instead of floating as separate blocks
//    under the input, so the transcript reads top-to-bottom like an
//    actual conversation;
//  - a typing-indicator bubble replaces the old Skeleton block while
//    agentLoading is true;
//  - a single sr-only aria-live region announces loading/error/plan
//    state changes for screen readers without visually duplicating
//    text that's now shown once, in the transcript itself.
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
  onAsk: (promptOverride?: string) => void;
  agentLoading: boolean;
  agentError: string;
  agentPlan: CheckoutPlan | null;
  agentSteps: LoopStep[];
  loading: boolean;
  onAcceptPlan: () => void;
  onDismissPlan: () => void;
  onChooseAlternative: (alt: AlternativeProduct) => void;
  suggestion: SuggestResponse | null;
  suggestionLoading: boolean;
  onAcceptSuggestion: () => void;
  onDismissSuggestion: () => void;
}) {
  // Shown only before the first turn -- once a real conversation exists
  // these would just clutter the transcript, so they're gated on
  // agentHistory being empty rather than always rendered above the input.
  const examplePrompts = [
    "MagSafe charger for my brother, under ₹5,000",
    "laptop stand for me, budget 3000",
    "earbuds for my sister, good battery life, under 25k",
  ];

  return (
    <div className="mb-6 rounded-xl border border-slate-200 bg-slate-50 p-5">
      <h2 className="mb-1 text-sm font-semibold uppercase tracking-wide text-slate-500">
        Ask the shopping agent
      </h2>
      <p className="mb-3 text-sm text-slate-600">
        Say what you want and the budget. It reads the catalog and proposes one item -- it never places an order itself; the normal checkout below still runs.
      </p>

      {agentHistory.length === 0 && (
        <div className="mb-3 flex flex-wrap gap-2">
          {examplePrompts.map((p) => (
            <button
              key={p}
              type="button"
              onClick={() => onAsk(p)}
              disabled={agentLoading}
              className="rounded-full border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-600 hover:border-slate-400 hover:bg-slate-100 disabled:opacity-50"
            >
              {p}
            </button>
          ))}
        </div>
      )}

      {(agentHistory.length > 0 || agentLoading) && (
        <div className="mb-3 max-h-80 space-y-2 overflow-y-auto rounded-lg border border-slate-200 bg-white p-3">
          {agentHistory.map((msg, i) => {
            const isLast = i === agentHistory.length - 1;
            const isUser = msg.role === "user";
            const crossSellCard =
              !isUser &&
              !!msg.crossSellProductId &&
              suggestion?.available &&
              suggestion.product?.product_id === msg.crossSellProductId;
            // The retry affordance re-sends whatever the buyer actually
            // typed for this failed turn -- that's always the immediately
            // preceding user message, since every assistant turn (error
            // or not) is appended right after its triggering user turn.
            const retryPrompt = !isUser && i > 0 ? agentHistory[i - 1].content : undefined;

            return (
              <div key={i} className={`flex animate-fade-in ${isUser ? "justify-end" : "justify-start"}`}>
                <div className={isUser ? "max-w-[85%]" : "max-w-[90%]"}>
                  <div
                    className={
                      isUser
                        ? "rounded-2xl rounded-br-sm bg-slate-900 px-3 py-2 text-sm text-white"
                        : msg.isError
                          ? "rounded-2xl rounded-bl-sm border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-900"
                          : "rounded-2xl rounded-bl-sm bg-slate-100 px-3 py-2 text-sm text-slate-700"
                    }
                  >
                    {msg.content}
                  </div>
                  {msg.isError && isLast && retryPrompt && (
                    <button
                      type="button"
                      onClick={() => onAsk(retryPrompt)}
                      disabled={agentLoading}
                      className="mt-1 text-xs font-medium text-amber-800 underline underline-offset-2 hover:text-amber-950 disabled:opacity-50"
                    >
                      Try again
                    </button>
                  )}

                  {crossSellCard && (
                    <div className="mt-2">
                      <SuggestionCard
                        suggestion={suggestion}
                        suggestionLoading={suggestionLoading}
                        loading={loading}
                        optimistic={null}
                        onAccept={onAcceptSuggestion}
                        onDismiss={onDismissSuggestion}
                      />
                    </div>
                  )}

                  {isLast && !isUser && agentPlan && (
                    <div className="mt-2 animate-fade-in rounded-lg border border-slate-300 bg-white p-4">
                      <div className="flex items-center justify-between gap-2">
                        <p className="text-xs font-medium uppercase tracking-wide text-slate-500">
                          Agent proposes
                        </p>
                        <SourceBadge source={agentPlan.intent?.source} />
                      </div>
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

                  {isLast && !isUser && agentSteps.length > 0 && (
                    <details className="mt-2 rounded-lg border border-slate-200 bg-white px-3 py-2 text-xs">
                      <summary className="cursor-pointer font-medium text-slate-500">
                        Agent reasoning trail ({agentSteps.length} step{agentSteps.length === 1 ? "" : "s"})
                      </summary>
                      <ul className="mt-2 space-y-1.5">
                        {agentSteps.map((s, si) => (
                          <li key={si} className="flex items-start gap-2 text-slate-600">
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
          })}

          {agentLoading && (
            <div className="flex animate-fade-in justify-start">
              <div className="flex items-center gap-1 rounded-2xl rounded-bl-sm bg-slate-100 px-3 py-2.5">
                <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-slate-400" style={{ animationDelay: "0ms" }} />
                <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-slate-400" style={{ animationDelay: "120ms" }} />
                <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-slate-400" style={{ animationDelay: "240ms" }} />
              </div>
            </div>
          )}
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
          onClick={() => onAsk()}
          disabled={agentLoading || !agentPrompt.trim()}
          className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-800 disabled:opacity-50"
        >
          {agentLoading ? "Thinking..." : "Ask"}
        </button>
      </div>

      {/* A single status region, not visually duplicated anywhere above --
          the loading/error/plan text a screen reader needs is already
          visible once, in the transcript itself (typing bubble, error
          bubble, proposal card). This renders unconditionally (only its
          content is conditional) because a live region inserted at the
          same moment as its content isn't reliably picked up. */}
      <div aria-live="polite" aria-atomic="true" className="sr-only">
        {agentLoading
          ? "Agent is thinking..."
          : agentError
            ? agentError
            : agentPlan
              ? `Agent proposes: ${agentPlan.reasoning}`
              : ""}
      </div>
    </div>
  );
}
