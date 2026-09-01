"use client";

import { useCallback, useEffect, useState } from "react";
import { formatINR, formatTime } from "../../../lib/format";
import { authFetch } from "../../../lib/auth";
import { downloadFile } from "../../../lib/download";

type Campaign = {
  campaign_id: string;
  merchant_id: string;
  product_id: string;
  discount_percent: number;
  budget_cap: number;
  spent: number;
  duration_days: number;
  starts_at?: string;
  ends_at?: string;
  status: string;
  policy_version: string;
  rejected_demand_count: number;
  reasoning: string;
  approved_by?: string;
  rejected_reason?: string;
  created_at: string;
  updated_at: string;
};

type Decision = {
  decision: string;
  policy_version: string;
  failed_check?: string;
  reason?: string;
};

const STATUS_STYLE: Record<string, string> = {
  PROPOSED: "bg-amber-100 text-amber-800",
  ACTIVE: "bg-emerald-100 text-emerald-800",
  APPROVED: "bg-emerald-100 text-emerald-800",
  REJECTED: "bg-slate-200 text-slate-600",
  COMPLETED: "bg-slate-200 text-slate-600",
  EXPIRED: "bg-slate-200 text-slate-600",
};

function StatusBadge({ status }: { status: string }) {
  return (
    <span className={`rounded-full px-2.5 py-1 text-xs font-semibold ${STATUS_STYLE[status] ?? "bg-slate-200 text-slate-600"}`}>
      {status}
    </span>
  );
}

// BudgetBar shows spend against budget_cap as a simple two-div fill bar
// -- no existing analog to copy in this dashboard, campaigns are the
// first feature here with a running spend total to visualize.
function BudgetBar({ spent, budgetCap }: { spent: number; budgetCap: number }) {
  const pct = budgetCap > 0 ? Math.min(100, Math.round((spent / budgetCap) * 100)) : 0;
  const exhausted = spent >= budgetCap;
  return (
    <div>
      <div className="h-2 w-full overflow-hidden rounded-full bg-slate-100">
        <div
          className={`h-full rounded-full ${exhausted ? "bg-rose-500" : "bg-slate-900"}`}
          style={{ width: `${pct}%` }}
        />
      </div>
      <p className="mt-1 text-xs text-slate-500">
        {formatINR(spent)} of {formatINR(budgetCap)} spent ({pct}%){exhausted ? " — budget exhausted" : ""}
      </p>
    </div>
  );
}

export default function CampaignsPage() {
  const [campaigns, setCampaigns] = useState<Campaign[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");
  const [exporting, setExporting] = useState(false);

  const [windowDays, setWindowDays] = useState(7);
  const [discountPercent, setDiscountPercent] = useState(15);
  const [durationDays, setDurationDays] = useState(7);
  const [proposing, setProposing] = useState(false);
  const [proposeResult, setProposeResult] = useState<{ campaign: Campaign; decision: Decision } | null>(null);
  const [proposeError, setProposeError] = useState("");

  const load = useCallback(() => {
    authFetch("/campaigns", { cache: "no-store" })
      .then((r) => r.json())
      .then((data: Campaign[]) => setCampaigns(data))
      .catch(() => setError("Could not load campaigns."))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  async function propose() {
    setProposing(true);
    setProposeError("");
    setProposeResult(null);
    try {
      const res = await authFetch("/campaigns/propose", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          window_days: windowDays,
          discount_percent: discountPercent,
          duration_days: durationDays,
        }),
      });
      if (!res.ok) throw new Error(await res.text());
      const data = (await res.json()) as { campaign: Campaign; decision: Decision };
      setProposeResult(data);
      load();
    } catch (cause) {
      setProposeError(cause instanceof Error ? cause.message : "Could not propose a campaign");
    } finally {
      setProposing(false);
    }
  }

  // item 27 (P2, PLAN-05-SELLER-DASHBOARD.md section 6): export every
  // campaign this page is showing as a CSV, via the same operator-
  // scoped GET /campaigns/export the list above reads from -- so this
  // can never disagree with what's on screen. Reuses the same `error`
  // state act()'s failures already render, rather than a dedicated
  // export-only error slot, since both are just "something the
  // operator clicked on this page failed."
  async function exportCSV() {
    setExporting(true);
    setError("");
    try {
      await downloadFile("/campaigns/export", "campaigns.csv");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not export campaigns");
    } finally {
      setExporting(false);
    }
  }

  async function act(id: string, endpoint: "approve" | "reject") {
    setBusy(id);
    setError("");
    try {
      const res = await authFetch(`/campaigns/${id}/${endpoint}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(endpoint === "reject" ? { reason: "rejected from dashboard" } : {}),
      });
      if (!res.ok) throw new Error(await res.text());
      load();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Action failed");
    } finally {
      setBusy("");
    }
  }

  return (
    <main className="px-5 py-7 sm:px-8 lg:px-10">
      <header className="flex flex-wrap items-start justify-between gap-4 border-b border-slate-200 pb-6">
        <div>
          <h1 className="text-3xl font-semibold tracking-tight">Campaigns</h1>
          <p className="mt-2 max-w-xl text-sm leading-6 text-slate-600">
            Bounded discount campaigns proposed from real rejected cross-sell demand (customers the
            growth agent wanted to upsell but couldn&apos;t, due to budget). Every proposal is sized to
            observed volume and gated by a deterministic policy engine, then requires your approval
            before it can discount anything at checkout.
          </p>
        </div>
        <button
          onClick={exportCSV}
          disabled={exporting || campaigns.length === 0}
          className="shrink-0 rounded-lg border border-slate-300 px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 disabled:opacity-50"
        >
          {exporting ? "Exporting…" : "Export CSV"}
        </button>
      </header>

      <section className="mt-8 rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
        <h2 className="text-sm font-semibold text-slate-900">Propose a campaign</h2>
        <p className="mt-1 text-sm text-slate-500">
          Looks at rejected cross-sell recommendations from the last N days, picks the product with
          the most rejected demand, and sizes a budget to the observed volume. Not run automatically
          — trigger it here.
        </p>
        <div className="mt-4 flex flex-wrap items-end gap-4">
          <label className="text-sm text-slate-700">
            Lookback (days)
            <input
              type="number"
              min={1}
              value={windowDays}
              onChange={(e) => setWindowDays(Number(e.target.value))}
              className="mt-1 block w-24 rounded-lg border border-slate-300 px-3 py-2 text-sm"
            />
          </label>
          <label className="text-sm text-slate-700">
            Discount %
            <input
              type="number"
              min={1}
              max={100}
              value={discountPercent}
              onChange={(e) => setDiscountPercent(Number(e.target.value))}
              className="mt-1 block w-24 rounded-lg border border-slate-300 px-3 py-2 text-sm"
            />
          </label>
          <label className="text-sm text-slate-700">
            Duration (days)
            <input
              type="number"
              min={1}
              value={durationDays}
              onChange={(e) => setDurationDays(Number(e.target.value))}
              className="mt-1 block w-24 rounded-lg border border-slate-300 px-3 py-2 text-sm"
            />
          </label>
          <button
            onClick={propose}
            disabled={proposing}
            className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700 disabled:opacity-50"
          >
            {proposing ? "Proposing…" : "Propose campaign"}
          </button>
        </div>

        {proposeError && (
          <p role="alert" className="mt-4 rounded-xl border border-rose-200 bg-rose-50 p-3 text-sm text-rose-800">
            {proposeError}
          </p>
        )}
        {proposeResult && (
          <div className="mt-4 rounded-xl border border-slate-200 bg-slate-50 p-3 text-sm text-slate-700">
            <p>
              Proposal for <span className="font-mono">{proposeResult.campaign.product_id}</span> was{" "}
              <span className="font-semibold">{proposeResult.decision.decision}</span>
              {proposeResult.decision.reason ? ` — ${proposeResult.decision.reason}` : ""}.
            </p>
          </div>
        )}
      </section>

      {error && (
        <p role="alert" className="mt-6 rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800">
          {error}
        </p>
      )}

      <section className="mt-8">
        {loading ? (
          <div className="space-y-4">
            <div className="h-32 animate-pulse rounded-2xl bg-slate-100" />
            <div className="h-32 animate-pulse rounded-2xl bg-slate-100" />
          </div>
        ) : campaigns.length === 0 ? (
          <div className="rounded-2xl border border-slate-200 bg-white p-8 text-center shadow-sm">
            <p className="text-sm font-medium text-slate-700">No campaigns yet</p>
            <p className="mt-2 text-sm text-slate-500">
              Propose one above once there is enough rejected cross-sell demand for a product.
            </p>
          </div>
        ) : (
          <ul className="space-y-4">
            {campaigns.map((c) => (
              <li key={c.campaign_id} className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
                <div className="flex flex-wrap items-start justify-between gap-4">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <StatusBadge status={c.status} />
                      <span className="font-mono text-xs text-slate-400">{c.campaign_id}</span>
                    </div>
                    <p className="mt-2 text-lg font-semibold text-slate-900">
                      {c.product_id} — {c.discount_percent}% off
                    </p>
                    <p className="mt-1 text-sm leading-6 text-slate-600">{c.reasoning}</p>
                    <p className="mt-2 text-xs text-slate-500">
                      {c.rejected_demand_count} rejected recommendation
                      {c.rejected_demand_count === 1 ? "" : "s"} observed · {c.duration_days}-day
                      duration · policy {c.policy_version}
                      {c.rejected_reason ? ` · rejected: ${c.rejected_reason}` : ""}
                      {c.approved_by ? ` · approved by ${c.approved_by}` : ""}
                    </p>
                    <p className="mt-1 text-xs text-slate-400">Proposed {formatTime(c.created_at)}</p>
                    <div className="mt-3 max-w-xs">
                      <BudgetBar spent={c.spent} budgetCap={c.budget_cap} />
                    </div>
                  </div>
                  {c.status === "PROPOSED" && (
                    <div className="flex shrink-0 gap-2">
                      <button
                        onClick={() => act(c.campaign_id, "approve")}
                        disabled={busy === c.campaign_id}
                        className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700 disabled:opacity-50"
                      >
                        {busy === c.campaign_id ? "…" : "Approve"}
                      </button>
                      <button
                        onClick={() => act(c.campaign_id, "reject")}
                        disabled={busy === c.campaign_id}
                        className="rounded-lg border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-100 disabled:opacity-50"
                      >
                        Reject
                      </button>
                    </div>
                  )}
                </div>
              </li>
            ))}
          </ul>
        )}
      </section>
    </main>
  );
}
