"use client";

import { useCallback, useEffect, useState } from "react";
import { formatINR } from "../../../lib/format";
import { authFetch } from "../../../lib/auth";

type ApprovalRequest = {
  approval_request_id: string;
  mandate_id: string;
  action: string;
  amount: number;
  currency: string;
  merchant: string;
  items: string[];
  cart_id: string;
  policy_version: string;
  risk_score: number;
  level: number;
  status: string;
  authorization_id: string;
  reason: string;
};

const LEVEL_LABEL: Record<number, string> = {
  1: "Level 1 — Auto-approve",
  2: "Level 2 — Requires confirmation",
  3: "Level 3 — Hard gate",
};

export default function ApprovalsPage() {
  const [requests, setRequests] = useState<ApprovalRequest[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");

  const load = useCallback(() => {
    authFetch("/approval-requests?status=PENDING", { cache: "no-store" })
      .then((r) => r.json())
      .then((data: ApprovalRequest[]) => setRequests(data))
      .catch(() => setError("Could not load approval requests."));
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  // The operator's identity is resolved server-side from the bearer
  // session token (backend/policy/service.go's resolveApprover) -- there
  // is no client-supplied approver field to trust anymore. See
  // files/JUDGE-FACING-GAPS.md P0.3.
  async function act(id: string, endpoint: "approve" | "reject") {
    setBusy(id);
    setError("");
    try {
      const res = await authFetch(`/approval-requests/${id}/${endpoint}`, {
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
      <header className="border-b border-slate-200 pb-6">
        <h1 className="text-3xl font-semibold tracking-tight">Approvals</h1>
        <p className="mt-2 max-w-xl text-sm leading-6 text-slate-600">
          Level 2/3 proposals require durable human approval before an authorization is issued.
          Approving issues a one-time authorization; rejecting blocks the payment entirely.
        </p>
      </header>

      {error && <p role="alert" className="mt-6 rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800">{error}</p>}

      <section className="mt-8">
        {requests.length === 0 ? (
          <div className="rounded-2xl border border-slate-200 bg-white p-8 text-center shadow-sm">
            <p className="text-sm font-medium text-slate-700">No pending approvals</p>
            <p className="mt-2 text-sm text-slate-500">Level 2/3 proposals will appear here waiting for human approval.</p>
          </div>
        ) : (
          <ul className="space-y-4">
            {requests.map((r) => (
              <li key={r.approval_request_id} className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
                <div className="flex flex-wrap items-start justify-between gap-4">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="rounded-full bg-amber-100 px-2.5 py-1 text-xs font-semibold text-amber-800">
                        {LEVEL_LABEL[r.level] ?? `Level ${r.level}`}
                      </span>
                      <span className="font-mono text-xs text-slate-400">{r.approval_request_id}</span>
                    </div>
                    <p className="mt-2 text-lg font-semibold text-slate-900">{r.action} — {formatINR(r.amount)}</p>
                    <p className="mt-1 text-sm text-slate-600">
                      {r.merchant} · items: {r.items.join(", ") || "—"} · {r.currency}
                    </p>
                    <p className="mt-1 text-xs text-slate-500">Risk {r.risk_score.toFixed(3)} · policy {r.policy_version}</p>
                  </div>
                  <div className="flex shrink-0 gap-2">
                    <button
                      onClick={() => act(r.approval_request_id, "approve")}
                      disabled={busy === r.approval_request_id}
                      className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700 disabled:opacity-50"
                    >
                      {busy === r.approval_request_id ? "…" : "Approve"}
                    </button>
                    <button
                      onClick={() => act(r.approval_request_id, "reject")}
                      disabled={busy === r.approval_request_id}
                      className="rounded-lg border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-100 disabled:opacity-50"
                    >
                      Reject
                    </button>
                  </div>
                </div>
              </li>
            ))}
          </ul>
        )}
      </section>
    </main>
  );
}
