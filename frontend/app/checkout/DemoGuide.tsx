"use client";

import { useState } from "react";
import type { DemoMilestones } from "./types";

// Guided demo walkthrough (item 38, P3,
// PLAN-06-ADDITIONAL-OPPORTUNITIES.md §4): "Package [the existing
// failure-handled-gracefully story] as an optional, explicit guided
// demo toggle in the UI ... a small 'walkthrough' affordance that
// highlights, in order: ask agent -> accept -> cross-sell appears ->
// attempt an over-budget item -> see the graceful rejection -> open
// the audit trail. This turns the existing demo script into something
// a judge can self-drive without a live presenter."
//
// Deliberately a pure, presentation-only overlay: it owns no checkout
// state itself, just renders `milestones` (computed in CheckoutFlow
// from the real state those six moments already produce -- see
// markDemoMilestone in checkout.tsx and the DemoMilestones comment in
// checkout/types.ts) as a persistent checklist. It never gates or
// alters the underlying flow -- a buyer can do every one of these
// steps, in any order, with the walkthrough off entirely; turning it
// on only adds a running scoreboard and a hint of what to try next.
const STEPS: { key: keyof DemoMilestones; label: string; hint: string }[] = [
  {
    key: "askedAgent",
    label: "Ask the agent",
    hint: 'Try: "Find me AirPods Pro for my brother."',
  },
  {
    key: "acceptedProposal",
    label: "Accept its proposal",
    hint: "Add the product the agent proposes to your cart.",
  },
  {
    key: "sawCrossSell",
    label: "See the cross-sell",
    hint: "The Growth Agent suggests an add-on, with its own expected-value reasoning.",
  },
  {
    key: "attemptedOverBudget",
    label: "Push past the budget",
    hint: "Add enough to the cart to exceed the mandate's ceiling.",
  },
  {
    key: "sawGracefulRejection",
    label: "Watch it fail safely",
    hint: "The policy engine blocks it before any payment call is made -- your cart stays intact.",
  },
  {
    key: "openedAuditTrail",
    label: "Read the audit trail",
    hint: "See the persisted proposed -> assessed -> evaluated -> authorized timeline for what just happened.",
  },
];

export function DemoGuide({
  milestones,
  onExit,
  onReset,
}: {
  milestones: DemoMilestones;
  onExit: () => void;
  onReset: () => void;
}) {
  const [collapsed, setCollapsed] = useState(false);
  const doneCount = STEPS.filter((s) => milestones[s.key]).length;
  const nextIndex = STEPS.findIndex((s) => !milestones[s.key]);
  const allDone = doneCount === STEPS.length;

  return (
    <div className="fixed bottom-4 right-4 z-50 w-80 max-w-[calc(100vw-2rem)] rounded-2xl border border-slate-200 bg-white p-4 shadow-lg">
      <div className="flex items-start justify-between gap-2">
        <div>
          <p className="text-xs font-medium uppercase tracking-wide text-slate-400">Guided demo</p>
          <p className="text-sm font-semibold text-slate-900">
            {doneCount} / {STEPS.length} steps seen
          </p>
        </div>
        <div className="flex flex-shrink-0 items-center gap-1">
          <button
            onClick={() => setCollapsed((c) => !c)}
            className="rounded-lg px-2 py-1 text-xs font-medium text-slate-500 hover:bg-slate-100"
          >
            {collapsed ? "Expand" : "Collapse"}
          </button>
          <button
            onClick={onExit}
            className="rounded-lg px-2 py-1 text-xs font-medium text-slate-500 hover:bg-slate-100"
          >
            Exit
          </button>
        </div>
      </div>

      {!collapsed && (
        <>
          <ul className="mt-3 space-y-2 border-t border-slate-100 pt-3">
            {STEPS.map((s, i) => {
              const done = milestones[s.key];
              const isNext = !done && i === nextIndex;
              return (
                <li
                  key={s.key}
                  className={`rounded-lg p-2 text-xs ${isNext ? "bg-slate-50 ring-1 ring-slate-200" : ""}`}
                >
                  <div className="flex items-center gap-2">
                    <span
                      className={`flex h-4 w-4 flex-shrink-0 items-center justify-center rounded-full text-[10px] font-bold ${
                        done ? "bg-emerald-600 text-white" : "border border-slate-300 text-transparent"
                      }`}
                    >
                      ✓
                    </span>
                    <span className={done ? "text-slate-400 line-through" : "font-medium text-slate-800"}>
                      {s.label}
                    </span>
                  </div>
                  {isNext && <p className="mt-1 pl-6 text-slate-500">{s.hint}</p>}
                </li>
              );
            })}
          </ul>

          {allDone && (
            <p className="mt-3 border-t border-slate-100 pt-3 text-xs text-emerald-700">
              All six steps seen -- that&apos;s the full story: agentic reasoning, a
              growth cross-sell, and a policy boundary that actually holds, end to
              end.
            </p>
          )}

          {doneCount > 0 && (
            <button
              onClick={onReset}
              className="mt-3 w-full rounded-lg border border-slate-200 px-3 py-1.5 text-xs font-medium text-slate-500 hover:bg-slate-100"
            >
              Restart walkthrough
            </button>
          )}
        </>
      )}
    </div>
  );
}
