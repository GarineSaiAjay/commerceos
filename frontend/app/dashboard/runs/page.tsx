"use client";

import { useCallback, useEffect, useState } from "react";
import { formatINR, formatTime, actionLabel } from "../../../lib/format";
import { authFetch } from "../../../lib/auth";

type RunStep = {
  stage: string;
  detail: string;
  timestamp: string;
};

type Run = {
  run_id: string;
  action: string;
  amount: number;
  currency: string;
  merchant: string;
  items: string[];
  decision: string;
  reason: string;
  authorization_id: string;
  authorization_status: string;
  created_at: string;
  steps?: RunStep[];
};

const STAGE_LABEL: Record<string, string> = {
  proposed: "Proposed",
  risk_assessed: "Risk assessed",
  policy_evaluated: "Policy evaluated",
  authorized: "Authorized",
  authorization_consumed: "Authorization consumed",
};

export default function RunsPage() {
  const [runs, setRuns] = useState<Run[]>([]);
  const [selected, setSelected] = useState<Run | null>(null);
  const [error, setError] = useState("");

  // GET /runs (list) is merchant-only -- it exposes every buyer's runs.
  // GET /runs/{id} stays reachable without auth so checkout.tsx can show a
  // buyer their own audit trail; authFetch works for it too, since it
  // simply adds a header the unguarded endpoint ignores.
  const loadRuns = useCallback(() => {
    authFetch("/runs?limit=50", { cache: "no-store" })
      .then((r) => r.json())
      .then((d: Run[]) => setRuns(d))
      .catch(() => setError("Could not load agent runs."));
  }, []);

  useEffect(() => {
    loadRuns();
  }, [loadRuns]);

  async function selectRun(id: string) {
    setError("");
    try {
      const res = await authFetch(`/runs/${id}`, { cache: "no-store" });
      if (!res.ok) throw new Error("run not found");
      setSelected((await res.json()) as Run);
    } catch {
      setError("Could not load that run.");
    }
  }

  return (
    <main className="px-5 py-7 sm:px-8 lg:px-10">
      <header className="border-b border-slate-200 pb-6">
        <h1 className="text-3xl font-semibold tracking-tight">Agent Runs</h1>
        <p className="mt-2 max-w-xl text-sm leading-6 text-slate-600">
          Forensic replay of every policy-reviewed agent action, reconstructed from persisted
          records — read-only, never re-executes anything.
        </p>
      </header>

      {error && <p role="alert" className="mt-6 rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800">{error}</p>}

      <div className="mt-8 grid gap-6 lg:grid-cols-2">
        <section className="rounded-2xl border border-slate-200 bg-white shadow-sm">
          <div className="border-b border-slate-100 px-5 py-4"><h2 className="text-base font-semibold">All runs</h2></div>
          {runs.length === 0 ? (
            <p className="p-5 text-sm text-slate-600">No agent actions recorded yet.</p>
          ) : (
            <ul className="max-h-[32rem] divide-y divide-slate-100 overflow-y-auto">
              {runs.map((run) => (
                <li key={run.run_id}>
                  <button onClick={() => selectRun(run.run_id)} className={`flex w-full items-center justify-between gap-4 px-5 py-4 text-left hover:bg-slate-50 ${selected?.run_id === run.run_id ? "bg-slate-50" : ""}`}>
                    <div className="min-w-0">
                      <p className="truncate font-medium text-slate-900">{actionLabel(run.action)} · {run.run_id}</p>
                      <p className="mt-0.5 text-xs text-slate-500">{run.merchant} · {formatTime(run.created_at)}</p>
                    </div>
                    <div className="flex shrink-0 items-center gap-2">
                      <span className={`rounded-full px-2 py-0.5 text-xs font-semibold ${run.decision === "APPROVED" ? "bg-emerald-100 text-emerald-800" : run.decision === "PENDING_HUMAN_APPROVAL" ? "bg-amber-100 text-amber-800" : "bg-rose-100 text-rose-800"}`}>
                        {run.decision || "—"}
                      </span>
                      <p className="text-sm font-semibold text-slate-900">{formatINR(run.amount)}</p>
                    </div>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </section>

        <section className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
          <h2 className="text-base font-semibold">Replay</h2>
          {!selected ? (
            <p className="mt-4 rounded-lg bg-slate-50 p-5 text-sm leading-6 text-slate-600">Select a run on the left to reconstruct its recorded sequence.</p>
          ) : (
            <dl className="mt-4 space-y-4 text-sm">
              <div><dt className="text-xs font-medium uppercase tracking-wide text-slate-500">Run ID</dt><dd className="mt-1 font-mono">{selected.run_id}</dd></div>
              <div><dt className="text-xs font-medium uppercase tracking-wide text-slate-500">Proposed action</dt><dd className="mt-1 font-medium">{selected.action}</dd></div>
              <div><dt className="text-xs font-medium uppercase tracking-wide text-slate-500">Merchant</dt><dd className="mt-1">{selected.merchant}</dd></div>
              <div><dt className="text-xs font-medium uppercase tracking-wide text-slate-500">Amount</dt><dd className="mt-1 font-semibold">{formatINR(selected.amount)}</dd></div>
              <div><dt className="text-xs font-medium uppercase tracking-wide text-slate-500">Items</dt><dd className="mt-1">{selected.items.join(", ") || "—"}</dd></div>
              <div><dt className="text-xs font-medium uppercase tracking-wide text-slate-500">Policy outcome</dt>
                <dd className="mt-1">
                  <span className={`rounded-full px-2 py-0.5 text-xs font-semibold ${selected.decision === "APPROVED" ? "bg-emerald-100 text-emerald-800" : selected.decision === "PENDING_HUMAN_APPROVAL" ? "bg-amber-100 text-amber-800" : "bg-rose-100 text-rose-800"}`}>{selected.decision || "—"}</span>
                  {selected.reason && <p className="mt-1 text-xs text-slate-500">{selected.reason}</p>}
                </dd>
              </div>
              {selected.authorization_id && (
                <div><dt className="text-xs font-medium uppercase tracking-wide text-slate-500">Authorization</dt><dd className="mt-1 font-mono text-xs">{selected.authorization_id} · {selected.authorization_status}</dd></div>
              )}
              <div><dt className="text-xs font-medium uppercase tracking-wide text-slate-500">Occurred at</dt><dd className="mt-1">{formatTime(selected.created_at)}</dd></div>
            </dl>
          )}
          {selected && selected.steps && selected.steps.length > 0 && (
            <div className="mt-6 border-t border-slate-100 pt-5">
              <h3 className="text-xs font-medium uppercase tracking-wide text-slate-500">Timeline</h3>
              <ol className="mt-3 space-y-3 border-l-2 border-slate-200 pl-4">
                {selected.steps.map((step, i) => (
                  <li key={`${step.stage}-${i}`} className="relative">
                    <span className="absolute -left-[1.32rem] top-1 h-2 w-2 rounded-full bg-slate-400" />
                    <p className="text-sm font-medium text-slate-900">{STAGE_LABEL[step.stage] || step.stage}</p>
                    <p className="mt-0.5 text-xs text-slate-600">{step.detail}</p>
                    <p className="mt-0.5 text-xs text-slate-400">{formatTime(step.timestamp)}</p>
                  </li>
                ))}
              </ol>
            </div>
          )}
        </section>
      </div>
    </main>
  );
}
