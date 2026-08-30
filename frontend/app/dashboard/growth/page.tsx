"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { formatINR, formatPct, formatTime } from "../../../lib/format";
import { authFetch } from "../../../lib/auth";

// Mirrors backend/growth/dashboard.go's GrowthOverview and its nested
// types field-for-field (json tags) -- this page is a thin read view
// over GET /dashboard/growth, no client-side aggregation of its own.
type FunnelSummary = {
  shown: number;
  accepted: number;
  dismissed: number;
};

type ProductAcceptance = {
  product_id: string;
  title: string;
  shown: number;
  accepted: number;
  acceptance_rate: number;
};

type RejectedDemandSummary = {
  product_id: string;
  title: string;
  reject_count: number;
  avg_price: number;
};

type GrowthOverview = {
  window_days: number;
  funnel: FunnelSummary;
  top_products: ProductAcceptance[];
  rejected_demand: RejectedDemandSummary[];
  generated_at: string;
};

function FunnelCard({ label, value, hint }: { label: string; value: number; hint: string }) {
  return (
    <section className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
      <p className="text-sm font-medium text-slate-600">{label}</p>
      <p className="mt-3 text-2xl font-semibold tracking-tight text-slate-950">{value}</p>
      <p className="mt-2 text-xs leading-5 text-slate-500">{hint}</p>
    </section>
  );
}

// AcceptanceBar visualizes acceptance_rate the same way campaigns/page.tsx's
// BudgetBar visualizes spend against budget_cap -- a simple two-div fill,
// this dashboard's established shape for "one ratio, at a glance."
function AcceptanceBar({ rate }: { rate: number }) {
  const pct = Math.round(Math.min(1, Math.max(0, rate)) * 100);
  return (
    <div>
      <div className="h-2 w-full overflow-hidden rounded-full bg-slate-100">
        <div className="h-full rounded-full bg-slate-900" style={{ width: `${pct}%` }} />
      </div>
      <p className="mt-1 text-xs text-slate-500">{formatPct(rate)} acceptance rate</p>
    </div>
  );
}

export default function GrowthPage() {
  const [overview, setOverview] = useState<GrowthOverview | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [windowDaysInput, setWindowDaysInput] = useState(7);

  useEffect(() => {
    let mounted = true;
    (async () => {
      try {
        const res = await authFetch("/dashboard/growth?window_days=7", { cache: "no-store" });
        if (!res.ok) throw new Error("Could not load growth data.");
        const data = (await res.json()) as GrowthOverview;
        if (mounted) {
          setOverview(data);
          setError("");
        }
      } catch (cause) {
        if (mounted) setError(cause instanceof Error ? cause.message : "Could not load growth data.");
      } finally {
        if (mounted) setLoading(false);
      }
    })();
    return () => {
      mounted = false;
    };
  }, []);

  async function refresh() {
    setLoading(true);
    setError("");
    try {
      const res = await authFetch(`/dashboard/growth?window_days=${windowDaysInput}`, { cache: "no-store" });
      if (!res.ok) throw new Error("Could not load growth data.");
      const data = (await res.json()) as GrowthOverview;
      setOverview(data);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not load growth data.");
    } finally {
      setLoading(false);
    }
  }

  return (
    <main className="px-5 py-7 sm:px-8 lg:px-10">
      <header className="border-b border-slate-200 pb-6">
        <h1 className="text-3xl font-semibold tracking-tight">Growth</h1>
        <p className="mt-2 max-w-xl text-sm leading-6 text-slate-600">
          How the cross-sell agent is actually performing: how many suggestions were shown across
          the cart, product detail, and post-checkout surfaces, how many buyers accepted them, which
          products convert best when recommended, and where rejected demand is piling up.
        </p>
      </header>

      <section className="mt-8 rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
        <h2 className="text-sm font-semibold text-slate-900">Window</h2>
        <p className="mt-1 text-sm text-slate-500">
          Every number below is scoped to this many trailing days.
        </p>
        <div className="mt-4 flex flex-wrap items-end gap-4">
          <label className="text-sm text-slate-700">
            Lookback (days)
            <input
              type="number"
              min={1}
              value={windowDaysInput}
              onChange={(e) => setWindowDaysInput(Number(e.target.value))}
              className="mt-1 block w-24 rounded-lg border border-slate-300 px-3 py-2 text-sm"
            />
          </label>
          <button
            onClick={refresh}
            disabled={loading}
            className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700 disabled:opacity-50"
          >
            {loading ? "Loading…" : "Refresh"}
          </button>
        </div>
      </section>

      {error && (
        <p role="alert" className="mt-6 rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800">
          {error}
        </p>
      )}

      <section className="mt-8">
        <h2 className="text-lg font-semibold text-slate-900">Suggestion funnel</h2>
        <p className="mt-1 text-sm text-slate-500">
          Accepted and dismissed both necessarily undercount shown -- the gap between them is a
          buyer who saw a suggestion and simply ignored it.
        </p>
        {loading && !overview ? (
          <div className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-3">
            <div className="h-28 animate-pulse rounded-2xl bg-slate-100" />
            <div className="h-28 animate-pulse rounded-2xl bg-slate-100" />
            <div className="h-28 animate-pulse rounded-2xl bg-slate-100" />
          </div>
        ) : (
          <div className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-3">
            <FunnelCard
              label="Shown"
              value={overview?.funnel.shown ?? 0}
              hint="Cross-sell suggestions shown across cart, product detail, and post-checkout."
            />
            <FunnelCard
              label="Accepted"
              value={overview?.funnel.accepted ?? 0}
              hint="Suggestions the buyer actually added to their cart."
            />
            <FunnelCard
              label="Dismissed"
              value={overview?.funnel.dismissed ?? 0}
              hint="Suggestions the buyer explicitly said no to."
            />
          </div>
        )}
      </section>

      <section className="mt-8">
        <h2 className="text-lg font-semibold text-slate-900">Top products by acceptance rate</h2>
        <p className="mt-1 text-sm text-slate-500">
          Ranked by how often a suggestion converts when shown, not just by volume.
        </p>
        <div className="mt-4">
          {loading && !overview ? (
            <div className="space-y-4">
              <div className="h-24 animate-pulse rounded-2xl bg-slate-100" />
              <div className="h-24 animate-pulse rounded-2xl bg-slate-100" />
            </div>
          ) : !overview || overview.top_products.length === 0 ? (
            <div className="rounded-2xl border border-slate-200 bg-white p-8 text-center shadow-sm">
              <p className="text-sm font-medium text-slate-700">No suggestions shown yet</p>
              <p className="mt-2 text-sm text-slate-500">
                Once buyers see cross-sell suggestions in this window, they&apos;ll rank here by
                acceptance rate.
              </p>
            </div>
          ) : (
            <ul className="space-y-4">
              {overview.top_products.map((p) => (
                <li key={p.product_id} className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
                  <div className="flex flex-wrap items-start justify-between gap-4">
                    <div className="min-w-0">
                      <p className="text-lg font-semibold text-slate-900">{p.title || p.product_id}</p>
                      <p className="mt-1 font-mono text-xs text-slate-400">{p.product_id}</p>
                      <p className="mt-2 text-xs text-slate-500">
                        {p.accepted} accepted of {p.shown} shown
                      </p>
                    </div>
                    <div className="w-full max-w-xs shrink-0 sm:w-48">
                      <AcceptanceBar rate={p.acceptance_rate} />
                    </div>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </div>
      </section>

      <section className="mt-8">
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <h2 className="text-lg font-semibold text-slate-900">Rejected demand</h2>
          <Link href="/dashboard/campaigns" className="text-sm font-medium text-slate-700 underline hover:text-slate-900">
            Review campaigns →
          </Link>
        </div>
        <p className="mt-1 text-sm text-slate-500">
          Products the growth agent wanted to suggest but couldn&apos;t, due to budget -- the same
          data the Campaign Orchestrator reads to size a discount campaign.
        </p>
        <div className="mt-4">
          {loading && !overview ? (
            <div className="space-y-4">
              <div className="h-20 animate-pulse rounded-2xl bg-slate-100" />
              <div className="h-20 animate-pulse rounded-2xl bg-slate-100" />
            </div>
          ) : !overview || overview.rejected_demand.length === 0 ? (
            <div className="rounded-2xl border border-slate-200 bg-white p-8 text-center shadow-sm">
              <p className="text-sm font-medium text-slate-700">No rejected demand in this window</p>
              <p className="mt-2 text-sm text-slate-500">
                Nothing was budget-rejected recently -- there&apos;s nothing for a campaign to
                recover yet.
              </p>
            </div>
          ) : (
            <ul className="space-y-4">
              {overview.rejected_demand.map((d) => (
                <li key={d.product_id} className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
                  <div className="flex flex-wrap items-start justify-between gap-4">
                    <div className="min-w-0">
                      <p className="text-lg font-semibold text-slate-900">{d.title || d.product_id}</p>
                      <p className="mt-1 font-mono text-xs text-slate-400">{d.product_id}</p>
                    </div>
                    <p className="text-sm text-slate-600">
                      {d.reject_count} rejection{d.reject_count === 1 ? "" : "s"} · avg{" "}
                      {formatINR(d.avg_price)}
                    </p>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </div>
      </section>

      {overview && (
        <p className="mt-8 text-xs text-slate-400">
          Last computed {formatTime(overview.generated_at)} · {overview.window_days}-day window.
        </p>
      )}
    </main>
  );
}
