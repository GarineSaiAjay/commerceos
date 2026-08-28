"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { actionLabel, formatINR, formatPct, formatTime, Skeleton } from "../../lib/format";
import { authFetch } from "../../lib/auth";

export type Overview = {
  metrics: {
    revenue: number;
    ai_revenue: number;
    conversion_rate: number;
    average_order_value: number;
    simulated: boolean;
  };
  recent_activity: Array<{ id: number; actor: string; action: string; entity_type: string; entity_id: string; detail: Record<string, unknown>; occurred_at: string }>;
  agent_actions: Array<{ id: string; action: string; merchant: string; amount: number; occurred_at: string }>;
  audit_integrity: { verified: boolean; chain_broken: boolean; rows_checked: number; broken_at_id?: number };
  safety: { available: boolean; message: string };
  generated_at: string;
};

function MetricCard({ label, value, hint }: { label: string; value: string; hint: string }) {
  return (
    <section className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
      <p className="text-sm font-medium text-slate-600">{label}</p>
      <p className="mt-3 text-2xl font-semibold tracking-tight text-slate-950">{value}</p>
      <p className="mt-2 text-xs leading-5 text-slate-500">{hint}</p>
    </section>
  );
}

export default function MerchantDashboard({ initialOverview, initialError }: { initialOverview: Overview; initialError?: string }) {
  const [overview, setOverview] = useState<Overview | null>(null);
  const [error, setError] = useState(initialError);
  const [refreshing, setRefreshing] = useState(false);
  const [dataState, setDataState] = useState<"loading" | "live" | "unavailable">("loading");

  useEffect(() => {
    let mounted = true;
    (async () => {
      try {
        const res = await authFetch("/dashboard/overview", { cache: "no-store" });
        if (!res.ok) throw new Error("Dashboard data is unavailable.");
        const data = (await res.json()) as Overview;
        if (mounted) {
          setOverview(data);
          setDataState("live");
          setError(undefined);
        }
      } catch (cause) {
        if (mounted) {
          setOverview((prev) => prev ?? initialOverview);
          setDataState("unavailable");
          setError(cause instanceof Error ? cause.message : "Dashboard data is unavailable.");
        }
      } finally {
        if (mounted) setRefreshing(false);
      }
    })();
    return () => {
      mounted = false;
    };
  }, [initialOverview]);

  async function refresh() {
    setRefreshing(true);
    setError(undefined);
    try {
      const res = await authFetch("/dashboard/overview", { cache: "no-store" });
      if (!res.ok) throw new Error("Dashboard data is unavailable.");
      const data = (await res.json()) as Overview;
      setOverview(data);
      setDataState("live");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Dashboard data is unavailable.");
      setDataState("unavailable");
    } finally {
      setRefreshing(false);
    }
  }

  const m = overview?.metrics;
  const integrity = overview?.audit_integrity;
  const integrityLabel = integrity?.chain_broken ? "Chain needs attention" : integrity?.verified ? "Verified" : "No verifiable events yet";

  return (
    <main className="px-5 py-7 sm:px-8 lg:px-10">
      <header className="flex flex-col justify-between gap-4 border-b border-slate-200 pb-6 sm:flex-row sm:items-start">
        <div>
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="text-3xl font-semibold tracking-tight">Overview</h1>
            <span className="rounded-full bg-amber-100 px-2.5 py-1 text-xs font-semibold text-amber-800">Test mode</span>
            <span
              className={`rounded-full px-2.5 py-1 text-xs font-semibold ${
                dataState === "live" ? "bg-emerald-100 text-emerald-800" : dataState === "loading" ? "bg-slate-100 text-slate-600" : "bg-rose-100 text-rose-800"
              }`}
            >
              {dataState === "live" ? "Live" : dataState === "loading" ? "Loading" : "Unavailable"}
            </span>
          </div>
          <p className="mt-2 max-w-xl text-sm leading-6 text-slate-600">Live commerce performance, agent decisions, and the controls that keep money movement accountable.</p>
        </div>
        <button onClick={refresh} disabled={refreshing} className="rounded-lg bg-slate-900 px-3.5 py-2 text-sm font-medium text-white hover:bg-slate-700 disabled:cursor-wait disabled:opacity-60">
          {refreshing ? "Refreshing…" : "Refresh"}
        </button>
      </header>

      {error && (
        <div role="alert" className="mt-6 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-950">
          <span>{error} Last known values remain visible.</span>
          <button onClick={refresh} className="font-semibold underline underline-offset-2">Try again</button>
        </div>
      )}

      <section aria-label="Live commerce metrics" className="mt-7">
        <div className="mb-3 flex items-center gap-2">
          <h2 className="text-base font-semibold">Live commerce</h2>
          <span className="rounded bg-emerald-100 px-2 py-0.5 text-xs font-medium text-emerald-800">Live data</span>
        </div>
        {!m ? (
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
            {[0, 1, 2, 3].map((i) => <Skeleton key={i} className="h-28" />)}
          </div>
        ) : (
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
            <MetricCard label="Revenue" value={formatINR(m.revenue)} hint="Captured and completed payments." />
            <MetricCard label="AI-attributed revenue" value={formatINR(m.ai_revenue)} hint="Orders linked to accepted recommendations." />
            <MetricCard label="Conversion" value={formatPct(m.conversion_rate)} hint="Paid orders divided by tracked carts." />
            <MetricCard label="Average order value" value={formatINR(m.average_order_value)} hint="Captured order value per paid order." />
          </div>
        )}
      </section>

      <div className="mt-8 grid gap-8 xl:grid-cols-[minmax(0,1.55fr)_minmax(18rem,1fr)]">
        <section aria-labelledby="activity-heading" className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
          <div className="flex items-baseline justify-between gap-4">
            <h2 id="activity-heading" className="text-base font-semibold">Recent activity</h2>
            <span className="text-xs text-slate-500">Source: audit ledger</span>
          </div>
          {!overview ? (
            <div className="mt-5 space-y-3">{[...Array(4)].map((_, i) => <Skeleton key={i} className="h-12" />)}</div>
          ) : overview.recent_activity.length === 0 ? (
            <p className="mt-8 rounded-lg bg-slate-50 p-5 text-sm leading-6 text-slate-600">No audit activity recorded yet. Completed checkout and policy events will appear here.</p>
          ) : (
            <ol className="mt-5 divide-y divide-slate-100">
              {overview.recent_activity.map((a) => (
                <li key={a.id} className="flex gap-4 py-4 first:pt-0">
                  <span aria-hidden className="mt-1.5 h-2.5 w-2.5 shrink-0 rounded-full bg-slate-900" />
                  <div className="min-w-0 flex-1">
                    <p className="font-medium text-slate-900">{actionLabel(a.action)}</p>
                    <p className="mt-1 truncate text-sm text-slate-600">{a.entity_type}: {a.entity_id}</p>
                    <p className="mt-1 text-xs text-slate-500">{a.actor} · {formatTime(a.occurred_at)}</p>
                  </div>
                </li>
              ))}
            </ol>
          )}
        </section>

        <div className="space-y-5">
          <section aria-labelledby="integrity-heading" className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
            <p className="text-xs font-semibold uppercase tracking-wide text-slate-500">Audit integrity</p>
            <h2 id="integrity-heading" className="mt-2 text-lg font-semibold text-slate-950">{integrityLabel}</h2>
            <p className="mt-2 text-sm leading-6 text-slate-600">{integrity?.rows_checked ?? 0} hash-chained event{(integrity?.rows_checked ?? 0) === 1 ? "" : "s"} checked.</p>
            {integrity?.chain_broken && <p role="alert" className="mt-3 rounded-lg bg-rose-50 p-3 text-sm text-rose-800">The audit chain is broken at event {integrity.broken_at_id}. Investigate before trusting this history.</p>}
          </section>

          <section aria-labelledby="safety-heading" className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
            <p className="text-xs font-semibold uppercase tracking-wide text-slate-500">Agent safety evaluation</p>
            <h2 id="safety-heading" className="mt-2 text-lg font-semibold text-slate-950">{overview?.safety.available ? "Evaluation available" : "Not evaluated yet"}</h2>
            <p className="mt-2 text-sm leading-6 text-slate-600">{overview?.safety.message}</p>
            <Link href="/dashboard/safety" className="mt-4 inline-block text-sm font-semibold text-slate-900 underline underline-offset-4">Open safety controls</Link>
          </section>
        </div>
      </div>

      <section aria-labelledby="actions-heading" className="mt-8 rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
        <div className="flex items-baseline justify-between gap-4">
          <h2 id="actions-heading" className="text-base font-semibold">Latest agent actions</h2>
          <span className="text-xs text-slate-500">Source: policy proposals</span>
        </div>
        {!overview ? (
          <div className="mt-5 space-y-3">{[...Array(3)].map((_, i) => <Skeleton key={i} className="h-10" />)}</div>
        ) : overview.agent_actions.length === 0 ? (
          <p className="mt-5 rounded-lg bg-slate-50 p-5 text-sm leading-6 text-slate-600">No agent proposals yet. Policy-reviewed actions will be listed here, whether approved or rejected.</p>
        ) : (
          <div className="mt-4 overflow-x-auto">
            <table className="w-full min-w-[34rem] text-left text-sm">
              <caption className="sr-only">Latest policy-reviewed agent actions</caption>
              <thead className="border-b border-slate-200 text-xs uppercase tracking-wide text-slate-500">
                <tr><th className="pb-3 font-medium">Action</th><th className="pb-3 font-medium">Merchant</th><th className="pb-3 font-medium">Amount</th><th className="pb-3 text-right font-medium">Time</th></tr>
              </thead>
              <tbody>
                {overview.agent_actions.map((a) => (
                  <tr key={a.id} className="border-b border-slate-100 last:border-0">
                    <td className="py-3 font-medium text-slate-900">{actionLabel(a.action)}</td>
                    <td className="py-3 text-slate-600">{a.merchant}</td>
                    <td className="py-3 text-slate-600">{formatINR(a.amount)}</td>
                    <td className="py-3 text-right text-slate-500">{formatTime(a.occurred_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </main>
  );
}
