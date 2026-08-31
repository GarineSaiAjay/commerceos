"use client";

import { Skeleton } from "../../lib/format";
import type { Run } from "./types";

// Inline audit-trail panel shown on the complete/failed screens --
// P0.4: the buyer sees the same proposed -> risk-assessed ->
// policy-evaluated -> authorized timeline a merchant operator would see
// in the dashboard's Runs tab, for the exact action their checkout ran.
//
// Extracted from checkout.tsx's renderAuditTrail() as part of item 21
// (PLAN-04-UI-UX-AND-LATENCY.md §A2) -- same JSX, moved; runId/run/
// runLoading are now explicit props instead of closed-over state.
export function AuditTrailPanel({
  runId,
  run,
  runLoading,
}: {
  runId: string;
  run: Run | null;
  runLoading: boolean;
}) {
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
