"use client";

import type { Payment, Recovery, Run } from "./types";
import { formatINR } from "./helpers";
import { AuditTrailPanel } from "./AuditTrailPanel";

// Extracted from checkout.tsx's step === "failed" JSX -- a Razorpay
// payment failure, with the graceful-recovery options
// (backend/agents/rejection_recovery.go's cart-reservation window --
// retry, change method, or remove an accessory to reduce the total)
// PLAN-01-AGENTIC-CORE.md §6 added. Same JSX, moved (including its
// pre-existing tab indentation -- left as-is rather than reformatted,
// to keep this purely a move); onRetryPayment/onChangePaymentMethod/
// onCancel/onRemoveAccessory are now callback props instead of closures
// over CheckoutFlow's startPayment/setMessage/setStep/.../
// removeAccessoryAndRetry.
export function FailedStep({
  recovery,
  payment,
  runId,
  run,
  runLoading,
  loading,
  onRemoveAccessory,
  onRetryPayment,
  onChangePaymentMethod,
  onCancel,
}: {
  recovery: Recovery | null;
  payment: Payment | null;
  runId: string;
  run: Run | null;
  runLoading: boolean;
  loading: boolean;
  onRemoveAccessory: (variantId: string) => void;
  onRetryPayment: () => void;
  onChangePaymentMethod: () => void;
  onCancel: () => void;
}) {
  return (
		<section>
			<h2 className="mb-4 text-lg font-semibold text-slate-900">
				Payment wasn&apos;t completed
			</h2>
			<div className="rounded-xl border border-amber-200 bg-amber-50 p-5">
				<p className="text-sm text-amber-900">
					{recovery ? recovery.safe_message : "Razorpay reported that the payment failed. Your order has not been charged twice. The cart remains reserved for 9 minutes."}
				</p>
			</div>

			<AuditTrailPanel runId={runId} run={run} runLoading={runLoading} />

			{(payment || (recovery && recovery.cart.subtotal > 0)) && (
				<div className="mt-6 rounded-xl border border-slate-200 p-5">
					<p className="text-sm text-slate-500">Amount due</p>
					<p className="text-2xl font-bold text-slate-900">
						{formatINR((payment ? payment.amount : recovery!.cart.subtotal) || 0)}
					</p>
				</div>
			)}

			{recovery && recovery.removable_items.length > 0 && (
				<div className="mt-6 rounded-xl border border-slate-200 p-5">
					<p className="text-sm font-semibold text-slate-900">Remove an item to reduce the total</p>
					<p className="mt-1 text-xs text-slate-500">
						Removing an item recomputes your total from the catalog and
						re-runs policy on the smaller order before payment.
					</p>
					<ul className="mt-3 divide-y divide-slate-200">
						{recovery.cart.items
							.filter((item) => recovery.removable_items.includes(item.variant_id))
							.map((item) => (
								<li key={item.variant_id} className="flex items-center justify-between py-3">
									<div>
										<p className="text-sm font-medium text-slate-900">{item.title}</p>
										<p className="text-xs text-slate-500">Qty {item.quantity} &middot; {formatINR(item.total)}</p>
									</div>
									<button
										onClick={() => onRemoveAccessory(item.variant_id)}
										disabled={loading}
										className="rounded-lg border border-slate-300 px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-100 disabled:opacity-50"
									>
										Remove
									</button>
								</li>
							))}
					</ul>
				</div>
			)}

			<div className="mt-6 space-y-3">
				<button
					onClick={onRetryPayment}
					disabled={loading || (recovery ? !recovery.retry_allowed : false)}
					className="w-full rounded-xl bg-black px-5 py-3 font-medium text-white transition hover:bg-slate-800 disabled:opacity-50"
				>
					{recovery && !recovery.retry_allowed ? "Reservation expired — start a new cart" : "Retry payment"}
				</button>
				<button
					onClick={onChangePaymentMethod}
					disabled={loading || (recovery ? !recovery.retry_allowed : false)}
					className="w-full rounded-xl border border-slate-300 px-5 py-3 font-medium text-slate-700 hover:bg-slate-100"
				>
					Change payment method
				</button>
				<button
					onClick={onCancel}
					className="w-full rounded-xl border border-slate-300 px-5 py-3 font-medium text-slate-700 hover:bg-slate-100"
				>
					Cancel
				</button>
			</div>
		</section>
  );
}
