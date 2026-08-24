"use client";

import { useCallback, useEffect, useState } from "react";

type Attack = { attack_id: string; prompt: string; kind: string; expected_guard: string };
type AttackResult = {
  attack_id: string;
  attack_string: string;
  attack_kind: string;
  blocked: boolean;
  decision: string;
  reason: string;
  policy_check: string;
  provider_call_delta: number;
};
type Evaluation = {
  evaluation_id: string;
  run_id: string;
  scenario_count: number;
  unauthorized_payments: number;
  duplicate_payments: number;
  policy_bypasses: number;
  wrong_merchant: number;
  invalid_authorization: number;
  graceful_failure_rate: number;
  passed: boolean;
};

const API = "http://localhost:8081";

export default function SafetyPage() {
  const [attacks, setAttacks] = useState<Attack[]>([]);
  const [results, setResults] = useState<AttackResult[]>([]);
  const [evals, setEvals] = useState<Evaluation[]>([]);
  const [running, setRunning] = useState(false);
  const [error, setError] = useState("");

  const loadEvals = useCallback(() => {
    fetch(`${API}/safety/evaluations`, { cache: "no-store" })
      .then((r) => r.json())
      .then((d: Evaluation[]) => setEvals(d))
      .catch(() => {});
  }, []);

  useEffect(() => {
    fetch(`${API}/safety/attacks`, { cache: "no-store" })
      .then((r) => r.json())
      .then(setAttacks)
      .catch(() => setError("Could not load the attack library."));
    loadEvals();
  }, [loadEvals]);

  async function runAttack(a: Attack) {
    setRunning(true);
    setError("");
    try {
      const res = await fetch(`${API}/safety/attacks/${a.attack_id}/run`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ mandate_id: "mnd_demo" }),
      });
      if (!res.ok) throw new Error(await res.text());
      const r = (await res.json()) as AttackResult;
      setResults((prev) => [r, ...prev]);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Attack failed to run");
    } finally {
      setRunning(false);
    }
  }

  async function runSuite() {
    setRunning(true);
    setError("");
    try {
      const res = await fetch(`${API}/safety/evaluations/run`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ mandate_id: "mnd_demo" }),
      });
      if (!res.ok) throw new Error(await res.text());
      const e = (await res.json()) as Evaluation;
      setEvals((prev) => [e, ...prev]);
      setResults([]);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "suite failed to run");
    } finally {
      setRunning(false);
    }
  }

  return (
    <main className="px-5 py-7 sm:px-8 lg:px-10">
      <header className="border-b border-slate-200 pb-6">
        <h1 className="text-3xl font-semibold tracking-tight">Agent Safety</h1>
        <p className="mt-2 max-w-xl text-sm leading-6 text-slate-600">
          Run the canned red-team attacks through the real policy pipeline. Every block is
          proven by the adapter&apos;s provider-call delta of <strong>0</strong>.
        </p>
      </header>

      {error && <p role="alert" className="mt-6 rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800">{error}</p>}

      <section className="mt-8 rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
        <div className="flex items-center justify-between gap-4">
          <h2 className="text-base font-semibold">Evaluation history</h2>
          <button onClick={runSuite} disabled={running} className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700 disabled:opacity-50">
            {running ? "Running…" : "Run full suite"}
          </button>
        </div>
        {evals.length === 0 ? (
          <p className="mt-4 rounded-lg bg-slate-50 p-4 text-sm text-slate-600">No evaluations yet. Run the suite to generate the Phase 8 safety report.</p>
        ) : (
          <ul className="mt-4 space-y-3">
            {evals.map((e) => (
              <li key={e.evaluation_id} className="rounded-lg border border-slate-100 p-4 text-sm">
                <div className="flex flex-wrap items-center gap-3">
                  <span className={`rounded-full px-2.5 py-0.5 text-xs font-semibold ${e.passed ? "bg-emerald-100 text-emerald-800" : "bg-rose-100 text-rose-800"}`}>
                    {e.passed ? "PASS" : "FAIL"}
                  </span>
                  <span className="font-mono text-xs text-slate-400">{e.run_id}</span>
                  <span className="text-slate-600">{e.scenario_count} scenarios</span>
                  <span className="text-slate-600">unauthorized {e.unauthorized_payments}</span>
                  <span className="text-slate-600">duplicates {e.duplicate_payments}</span>
                  <span className="text-slate-600">bypasses {e.policy_bypasses}</span>
                  <span className="text-slate-600">graceful {(e.graceful_failure_rate * 100).toFixed(0)}%</span>
                </div>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="mt-8">
        <h2 className="text-base font-semibold">Attack library</h2>
        <p className="mt-1 text-sm text-slate-600">Each attack runs through the real pipeline — the block is a genuine policy rejection, not a scripted UI.</p>
        <ul className="mt-4 grid gap-3 sm:grid-cols-2">
          {attacks.map((a) => (
            <li key={a.attack_id} className="flex items-center justify-between gap-3 rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
              <div className="min-w-0 flex-1">
                <p className="truncate font-mono text-xs text-slate-900">&ldquo;{a.prompt}&rdquo;</p>
                <p className="mt-1 text-xs text-slate-500">{a.kind} · {a.expected_guard}</p>
              </div>
              <button onClick={() => runAttack(a)} disabled={running} className="shrink-0 rounded-lg border border-slate-300 px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-100 disabled:opacity-50">
                Run
              </button>
            </li>
          ))}
        </ul>
      </section>

      {results.length > 0 && (
        <section className="mt-8">
          <h2 className="text-base font-semibold">Latest evidence</h2>
          <ul className="mt-4 space-y-2">
            {results.map((r, i) => (
              <li key={i} className="rounded-xl border border-slate-200 bg-white p-4 text-sm shadow-sm">
                <div className="flex flex-wrap items-center gap-3">
                  <span className={`rounded-full px-2.5 py-0.5 text-xs font-semibold ${r.blocked ? "bg-emerald-100 text-emerald-800" : "bg-rose-100 text-rose-800"}`}>
                    {r.blocked ? "BLOCKED" : "NOT BLOCKED"}
                  </span>
                  <span className="font-mono text-xs text-slate-400">{r.attack_id}</span>
                  <span className="text-slate-600">{r.policy_check || "—"}</span>
                  <span className={`text-xs ${r.provider_call_delta === 0 ? "text-emerald-700" : "text-rose-700"}`}>
                    provider calls: {r.provider_call_delta}
                  </span>
                </div>
                <p className="mt-2 text-xs text-slate-500">{r.reason}</p>
              </li>
            ))}
          </ul>
        </section>
      )}
    </main>
  );
}