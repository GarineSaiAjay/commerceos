"use client";

// Public, judge-friendly audit-verification page (item 36, P3 /
// PLAN-06-ADDITIONAL-OPPORTUNITIES.md §3). Deliberately outside
// app/dashboard/ -- no auth-gate, no DashboardLayout sidebar, no
// authFetch (plain fetch to the public GET /trust/summary and
// POST /trust/run-suite endpoints, backend/trust/handler.go). Everything
// this page shows already exists elsewhere in the gated dashboard
// (the audit-chain check behind POST /audit/verify, the counter behind
// GET /adapter/calls, the attack suite behind POST /safety/evaluations/
// run) -- this is a presentation change, not new evidence, so it
// deliberately mirrors app/dashboard/safety/page.tsx's visual language
// (the same pill badges, card shells, and copy tone) rather than
// inventing a second design language for the same kind of evidence.
import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { API_BASE } from "../../lib/api";
import { Skeleton } from "../../lib/format";

type AuditChain = {
  Verified: boolean;
  ChainBroken: boolean;
  RowsChecked: number;
  BrokenAtID: number;
};

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
  results?: AttackResult[];
};

type Summary = {
  audit_chain: AuditChain;
  razorpay_calls: number;
  latest_evaluation?: Evaluation;
};

export default function TrustPage() {
  const [summary, setSummary] = useState<Summary | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [running, setRunning] = useState(false);
  const [runError, setRunError] = useState("");
  const [freshRun, setFreshRun] = useState<Evaluation | null>(null);

  // Deliberately no synchronous setLoading(true) at the top of this
  // function -- react-hooks/set-state-in-effect (a real ESLint error,
  // not a style nit) flags a setState call that runs synchronously
  // during the effect body below, before any async boundary. `loading`
  // already starts true via useState(true) above, so the initial mount
  // still shows the skeleton; every setState call here happens inside a
  // .then()/.catch()/.finally() callback, i.e. after the fetch's own
  // async boundary, which is exactly the pattern
  // app/dashboard/safety/page.tsx's loadEvals already uses for the same
  // reason. A side benefit: calling load() again later (runSuite's
  // refresh below) no longer re-flashes the skeleton mid-page.
  const load = useCallback(() => {
    fetch(`${API_BASE}/trust/summary`, { cache: "no-store" })
      .then((res) => {
        if (!res.ok) throw new Error(`could not load the trust summary (${res.status})`);
        return res.json() as Promise<Summary>;
      })
      .then((data) => {
        setSummary(data);
        setLoadError("");
      })
      .catch((cause) => setLoadError(cause instanceof Error ? cause.message : "Could not load the trust summary."))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  async function runSuite() {
    setRunning(true);
    setRunError("");
    try {
      const res = await fetch(`${API_BASE}/trust/run-suite`, { method: "POST" });
      const text = await res.text();
      if (!res.ok) {
        // A 429 body is already the plain-language cooldown message
        // trust.Handler.RunSuite writes (backend/trust/handler.go) --
        // show it as-is rather than a generic "failed" string.
        throw new Error(text || `suite failed to run (${res.status})`);
      }
      const evaluation = JSON.parse(text) as Evaluation;
      setFreshRun(evaluation);
      // Re-fetch the summary too, so audit_chain/razorpay_calls reflect
      // whatever this run just did (the suite proposes real actions
      // through the real policy pipeline -- it can move the call
      // counter and append audit rows even though every attack is
      // expected to be blocked).
      load();
    } catch (cause) {
      setRunError(cause instanceof Error ? cause.message : "Suite failed to run.");
    } finally {
      setRunning(false);
    }
  }

  const evaluation = freshRun ?? summary?.latest_evaluation ?? null;
  const chain = summary?.audit_chain ?? null;

  return (
    <main className="mx-auto max-w-3xl px-5 py-10 sm:px-8">
      <header className="border-b border-slate-200 pb-6">
        <p className="text-xs font-medium uppercase tracking-wide text-slate-400">Public — no login required</p>
        <h1 className="mt-1 text-3xl font-semibold tracking-tight">Trust &amp; Audit</h1>
        <p className="mt-2 max-w-xl text-sm leading-6 text-slate-600">
          Every number below is read live from CommerceOS&apos;s own audit chain and provider-call
          counter — nothing here is a static claim. <Link href="/" className="underline hover:text-slate-900">Back to the storefront →</Link>
        </p>
      </header>

      {loadError && (
        <p role="alert" className="mt-6 rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800">
          {loadError}
        </p>
      )}

      <section className="mt-8 grid gap-4 sm:grid-cols-2">
        <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
          <h2 className="text-sm font-semibold text-slate-500">Audit chain integrity</h2>
          {loading && !chain ? (
            <Skeleton className="mt-3 h-8 w-32" />
          ) : chain ? (
            <>
              <p className="mt-2 flex items-center gap-2">
                <span
                  className={`rounded-full px-2.5 py-0.5 text-xs font-semibold ${
                    chain.Verified && !chain.ChainBroken ? "bg-emerald-100 text-emerald-800" : "bg-rose-100 text-rose-800"
                  }`}
                >
                  {chain.Verified && !chain.ChainBroken ? "INTACT" : "BROKEN"}
                </span>
                <span className="text-sm text-slate-600">{chain.RowsChecked} events checked</span>
              </p>
              {chain.ChainBroken && (
                <p className="mt-2 text-xs text-rose-700">First mismatch at audit event #{chain.BrokenAtID}.</p>
              )}
              <p className="mt-3 text-xs text-slate-500">
                Every audit row&apos;s hash is recomputed from its own content and the previous row&apos;s
                hash — a single tampered or missing row breaks the chain here, not just in a log a human
                has to read.
              </p>
            </>
          ) : null}
        </div>

        <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
          <h2 className="text-sm font-semibold text-slate-500">Razorpay provider calls</h2>
          {loading && !summary ? (
            <Skeleton className="mt-3 h-8 w-20" />
          ) : (
            <>
              <p className="mt-2 text-3xl font-semibold tabular-nums">{summary?.razorpay_calls ?? 0}</p>
              <p className="mt-3 text-xs text-slate-500">
                The live count of every real call this deployment has made to Razorpay. Compare it
                against the attack results below — a blocked attack must show a call delta of{" "}
                <strong>0</strong>.
              </p>
            </>
          )}
        </div>
      </section>

      <section className="mt-8 rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div>
            <h2 className="text-base font-semibold">14-attack safety suite</h2>
            <p className="mt-1 text-sm text-slate-600">
              Each attack is a real proposal sent through the real policy pipeline — not a scripted
              UI. A pass means every attack was rejected with zero provider-call side effects.
            </p>
          </div>
          <button
            onClick={runSuite}
            disabled={running}
            className="shrink-0 rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700 disabled:opacity-50"
          >
            {running ? "Running…" : "Run the suite"}
          </button>
        </div>

        {runError && (
          <p role="alert" className="mt-4 rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800">
            {runError}
          </p>
        )}

        {evaluation ? (
          <div className="mt-5 rounded-xl border border-slate-100 p-4 text-sm">
            <div className="flex flex-wrap items-center gap-3">
              <span
                className={`rounded-full px-2.5 py-0.5 text-xs font-semibold ${
                  evaluation.passed ? "bg-emerald-100 text-emerald-800" : "bg-rose-100 text-rose-800"
                }`}
              >
                {evaluation.passed ? "PASS" : "FAIL"}
              </span>
              <span className="font-mono text-xs text-slate-400">{evaluation.run_id}</span>
              <span className="text-slate-600">{evaluation.scenario_count} scenarios</span>
              <span className="text-slate-600">unauthorized {evaluation.unauthorized_payments}</span>
              <span className="text-slate-600">duplicates {evaluation.duplicate_payments}</span>
              <span className="text-slate-600">bypasses {evaluation.policy_bypasses}</span>
              <span className="text-slate-600">graceful {(evaluation.graceful_failure_rate * 100).toFixed(0)}%</span>
            </div>

            {evaluation.results && evaluation.results.length > 0 && (
              <ul className="mt-4 space-y-2">
                {evaluation.results.map((r) => (
                  <li key={r.attack_id} className="rounded-lg border border-slate-100 p-3">
                    <div className="flex flex-wrap items-center gap-2">
                      <span
                        className={`rounded-full px-2 py-0.5 text-[11px] font-semibold ${
                          r.blocked ? "bg-emerald-100 text-emerald-800" : "bg-rose-100 text-rose-800"
                        }`}
                      >
                        {r.blocked ? "BLOCKED" : "NOT BLOCKED"}
                      </span>
                      <span className="font-mono text-[11px] text-slate-400">{r.attack_id}</span>
                      <span className="text-xs text-slate-600">{r.policy_check || "—"}</span>
                      <span className={`text-[11px] ${r.provider_call_delta === 0 ? "text-emerald-700" : "text-rose-700"}`}>
                        provider calls: {r.provider_call_delta}
                      </span>
                    </div>
                    <p className="mt-1.5 text-xs text-slate-500">{r.reason}</p>
                  </li>
                ))}
              </ul>
            )}
          </div>
        ) : (
          !loading && (
            <p className="mt-5 rounded-lg bg-slate-50 p-4 text-sm text-slate-600">
              No evaluation on record yet — run the suite above to generate one.
            </p>
          )
        )}
      </section>

      <footer className="mt-10 border-t border-slate-200 pt-6 text-xs text-slate-400">
        Machine-readable version of the underlying contract:{" "}
        <a href={`${API_BASE}/.well-known/agent-commerce.json`} className="underline hover:text-slate-600">
          /.well-known/agent-commerce.json
        </a>
        .
      </footer>
    </main>
  );
}
