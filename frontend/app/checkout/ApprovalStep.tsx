"use client";

import type { Order } from "./types";
import { formatINR } from "./helpers";

// Extracted from checkout.tsx's step === "approval" JSX as part of
// item 21's follow-up (PLAN-04-UI-UX-AND-LATENCY.md §A2 -- the six
// per-step screens the original split left inline). Same JSX, moved;
// approveAndPay/rejectApproval are now onApprove/onReject callback
// props instead of closures over CheckoutFlow's usePaymentFlow
// destructure, matching the onX convention every other extracted
// panel in this directory already uses (CartPanel's onAcceptSuggestion/
// onDismissSuggestion, SuggestionCard's onAccept/onDismiss, ...).
//
// Level 1/2 auto-approved orders never reach this screen at all --
// see AUTH.md for the policy levels -- so `order`/`approvalRequestId`
// being possibly unset this early in the flow is handled the same way
// the original inline JSX did (a conditional `{order && (...)}` block),
// not hidden behind a stricter prop type.
export function ApprovalStep({
  order,
  approvalLevel,
  approvalReason,
  approvalRequestId,
  loading,
  onApprove,
  onReject,
}: {
  order: Order | null;
  approvalLevel: number;
  approvalReason: string;
  approvalRequestId: string;
  loading: boolean;
  onApprove: () => void;
  onReject: () => void;
}) {
  return (
          <section>
            <h2 className="mb-4 text-lg font-semibold text-slate-900">
              Purchase Requires Approval
            </h2>
            <div className="rounded-xl border border-amber-200 bg-amber-50 p-5">
              <p className="text-xs font-semibold uppercase tracking-wide text-amber-700">
                Level {approvalLevel} confirmation
              </p>
              <p className="mt-1 text-sm text-amber-900">
                This order is above the auto-approval threshold. An operator
                must approve it before payment can be initiated. {approvalReason}
              </p>
              {order && (
                <dl className="mt-4 grid gap-2 text-sm text-amber-900 sm:grid-cols-2">
                  <div><dt className="font-medium">Order</dt><dd>{order.order_id}</dd></div>
                  <div><dt className="font-medium">Total</dt><dd>{formatINR(order.subtotal)}</dd></div>
                  <div><dt className="font-medium">Approval request</dt><dd className="font-mono">{approvalRequestId}</dd></div>
                </dl>
              )}
            </div>
            <div className="mt-6 space-y-3">
              <button
                onClick={onApprove}
                disabled={loading}
                className="w-full rounded-xl bg-black px-5 py-3.5 font-medium text-white transition hover:bg-slate-800 disabled:opacity-50"
              >
                {loading ? "Approving..." : `Approve & Pay ${order ? formatINR(order.subtotal) : ""}`}
              </button>
              <button
                onClick={onReject}
                disabled={loading}
                className="w-full rounded-xl border border-slate-300 px-5 py-3 font-medium text-slate-700 hover:bg-slate-100"
              >
                Reject
              </button>
            </div>
          </section>
  );
}
