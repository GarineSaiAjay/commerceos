"use client";

import type { ApprovalRequestDetail } from "./types";
import { formatINR } from "./helpers";

// Extracted from checkout.tsx's step === "gate" JSX -- the Level 3
// hard-gate screen (AUTH.md) that cannot be dismissed or skipped.
// Same JSX, moved; approveGateAndPay/rejectApproval/backToOrderFromGate
// (all from usePaymentFlow) are now onApprove/onReject/onBackToOrder
// props. setGateConfirmed is passed through as the raw setter, same
// convention ProductList.tsx already uses for its own filter/sort
// setters -- this is a single plain boolean toggle, not a composite
// action worth its own named callback.
export function GateStep({
  approvalLevel,
  approvalReason,
  approvalSnapshot,
  gateError,
  gateConfirmed,
  setGateConfirmed,
  loading,
  approvalRequestId,
  onApprove,
  onReject,
  onBackToOrder,
}: {
  approvalLevel: number;
  approvalReason: string;
  approvalSnapshot: ApprovalRequestDetail | null;
  gateError: string;
  gateConfirmed: boolean;
  setGateConfirmed: (confirmed: boolean) => void;
  loading: boolean;
  approvalRequestId: string;
  onApprove: () => void;
  onReject: () => void;
  onBackToOrder: () => void;
}) {
  return (
          <section>
            <div className="mb-6 rounded-xl border-2 border-red-600 bg-red-50 p-5">
              <p className="text-xs font-bold uppercase tracking-wide text-red-700">
                Level {approvalLevel} &middot; Hard Gate
              </p>
              <h2 className="mt-1 text-xl font-bold text-red-900">
                This purchase cannot proceed without your explicit, deliberate approval
              </h2>
              <p className="mt-2 text-sm text-red-800">
                {approvalReason} This screen cannot be dismissed or skipped &mdash;
                there is no background action, keyboard shortcut, or cached
                authorization that bypasses it, and the request below is
                re-verified against the server the instant you approve it.
              </p>
            </div>

            {approvalSnapshot && (
              <dl className="grid gap-2 rounded-xl border border-slate-200 p-5 text-sm text-slate-900 sm:grid-cols-2">
                <div><dt className="font-medium text-slate-500">Merchant</dt><dd>{approvalSnapshot.merchant}</dd></div>
                <div><dt className="font-medium text-slate-500">Amount</dt><dd>{formatINR(approvalSnapshot.amount)}</dd></div>
                <div className="sm:col-span-2"><dt className="font-medium text-slate-500">Items</dt><dd>{approvalSnapshot.items.join(", ")}</dd></div>
                <div><dt className="font-medium text-slate-500">Policy version</dt><dd className="font-mono">{approvalSnapshot.policy_version}</dd></div>
                <div><dt className="font-medium text-slate-500">Risk score</dt><dd>{approvalSnapshot.risk_score.toFixed(2)}</dd></div>
                <div className="sm:col-span-2"><dt className="font-medium text-slate-500">Approval request</dt><dd className="font-mono">{approvalRequestId}</dd></div>
              </dl>
            )}

            {gateError && (
              <div className="mt-4 rounded-lg border border-red-300 bg-red-50 p-4 text-sm text-red-800">
                {gateError}
              </div>
            )}

            <label className="mt-6 flex items-start gap-3 rounded-xl border border-slate-300 p-4 text-sm text-slate-800">
              <input
                type="checkbox"
                checked={gateConfirmed}
                onChange={(e) => setGateConfirmed(e.target.checked)}
                disabled={loading}
                className="mt-0.5 h-4 w-4"
              />
              I have reviewed the merchant, amount, and items above and I
              deliberately approve this exact purchase.
            </label>

            <div className="mt-4 space-y-3">
              <button
                onClick={onApprove}
                disabled={loading || !gateConfirmed}
                className="w-full rounded-xl bg-red-700 px-5 py-3.5 font-medium text-white transition hover:bg-red-800 disabled:opacity-50"
              >
                {loading ? "Re-verifying..." : `Approve this exact purchase \u2014 ${approvalSnapshot ? formatINR(approvalSnapshot.amount) : ""}`}
              </button>
              <button
                onClick={onReject}
                disabled={loading}
                className="w-full rounded-xl border border-slate-300 px-5 py-3 font-medium text-slate-700 hover:bg-slate-100"
              >
                Reject
              </button>
              {gateError && (
                <button
                  onClick={onBackToOrder}
                  disabled={loading}
                  className="w-full rounded-xl border border-slate-300 px-5 py-3 font-medium text-slate-700 hover:bg-slate-100"
                >
                  Back to order
                </button>
              )}
            </div>
          </section>
  );
}
