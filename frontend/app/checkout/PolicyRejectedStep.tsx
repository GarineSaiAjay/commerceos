"use client";

import type { Order, RejectionRecoverySuggestion, Run } from "./types";
import { formatINR } from "./helpers";
import { AuditTrailPanel } from "./AuditTrailPanel";

// Extracted from checkout.tsx's step === "policy_rejected" JSX -- a
// purchase the Policy Engine itself refused (before any Razorpay call
// was made), with the same graceful-recovery options FailedStep offers
// plus a proactive in-budget substitute suggestion
// (backend/agents/rejection_recovery.go's second recovery path,
// PLAN-01-AGENTIC-CORE.md §6). Same JSX, moved (including its
// pre-existing tab indentation, same reasoning as FailedStep.tsx);
// onAcceptSubstitute/onRemoveAccessory are now callback props.
// onReturnToCatalog is usePaymentFlow's resetToCatalog passed straight
// through (unlike PayStep/FailedStep/CompleteStep's single-purpose
// onCancel/onStartNewOrder, resetToCatalog already takes the message to
// show as its own argument, so there's nothing to wrap).
//
// The wrapper condition in checkout.tsx (`step === "policy_rejected"
// && order`) guarantees order is set before this ever renders, so it's
// typed as Order here, not Order | null -- unlike CompleteStep, whose
// wrapper only guarantees payment, not order.
export function PolicyRejectedStep({
  order,
  policyRejectionReason,
  runId,
  run,
  runLoading,
  substituteSuggestionLoading,
  substituteSuggestion,
  onAcceptSubstitute,
  loading,
  removingVariantId,
  onRemoveAccessory,
  onReturnToCatalog,
}: {
  order: Order;
  policyRejectionReason: string;
  runId: string;
  run: Run | null;
  runLoading: boolean;
  substituteSuggestionLoading: boolean;
  substituteSuggestion: RejectionRecoverySuggestion | null;
  onAcceptSubstitute: () => void;
  loading: boolean;
  removingVariantId: string | null;
  onRemoveAccessory: (variantId: string) => void;
  onReturnToCatalog: (message: string) => void;
}) {
  return (
		<section>
			<h2 className="mb-4 text-lg font-semibold text-slate-900">
				Payment wasn&apos;t authorized
			</h2>
			<div className="rounded-xl border border-rose-200 bg-rose-50 p-5">
				{/* item 28 (P2, PLAN-04-UI-UX-AND-LATENCY.md §A5) extension:
				    the single most consequential message on this screen --
				    role="alert" (implicit aria-live="assertive" + atomic)
				    so a screen reader announces it immediately rather than
				    only if the user happens to navigate onto it. */}
				<p role="alert" className="text-sm text-rose-900">
					{policyRejectionReason}
				</p>
				<p className="mt-2 text-xs text-rose-700">
					No payment was attempted -- the policy engine rejected this
					purchase before any Razorpay call was made. Your cart is
					unaffected.
				</p>
			</div>

			{/* item 38 (P3, PLAN-06-ADDITIONAL-OPPORTUNITIES.md §4): the
			    rejection screen previously never showed this, even though a
			    rejected action gets a real audit run row just like a
			    successful one -- see the fetchRun effect above. Added so the
			    guided demo walkthrough's last step is reachable from here,
			    not only from the complete/failed screens. */}
			<AuditTrailPanel runId={runId} run={run} runLoading={runLoading} />

			{substituteSuggestionLoading && (
				<div className="mt-6 rounded-xl border border-slate-200 p-5">
					<p className="text-sm text-slate-500">Checking for an in-budget substitute...</p>
				</div>
			)}

			{!substituteSuggestionLoading && substituteSuggestion?.available && substituteSuggestion.replaced_item && substituteSuggestion.substitute && (
				<div className="mt-6 rounded-xl border border-emerald-200 bg-emerald-50 p-5">
					<p className="text-sm font-semibold text-emerald-900">We found an in-budget substitute</p>
					{substituteSuggestion.reasoning && (
						<p className="mt-1 text-xs text-emerald-700">{substituteSuggestion.reasoning}</p>
					)}
					<div className="mt-3 flex items-center justify-between rounded-lg bg-white/60 p-3">
						<div>
							<p className="text-xs text-slate-500 line-through">{substituteSuggestion.replaced_item.title}</p>
							<p className="text-sm font-medium text-slate-900">{substituteSuggestion.substitute.title}</p>
						</div>
						<div className="text-right">
							<p className="text-xs text-slate-500 line-through">{formatINR(substituteSuggestion.replaced_item.price)}</p>
							<p className="text-sm font-semibold text-emerald-900">{formatINR(substituteSuggestion.substitute.price)}</p>
						</div>
					</div>
					{typeof substituteSuggestion.new_subtotal === "number" && (
						<p className="mt-2 text-xs text-emerald-700">
							New order total: {formatINR(substituteSuggestion.new_subtotal)}
						</p>
					)}
					<button
						onClick={onAcceptSubstitute}
						disabled={loading}
						className="mt-3 w-full rounded-lg bg-emerald-700 px-4 py-2 text-sm font-medium text-white transition hover:bg-emerald-800 disabled:opacity-50"
					>
						Swap &amp; continue
					</button>
				</div>
			)}

			{order.items.length > 1 && (
				<div className="mt-6 rounded-xl border border-slate-200 p-5">
					<p className="text-sm font-semibold text-slate-900">Remove an item and try again</p>
					<p className="mt-1 text-xs text-slate-500">
						Removing an item recomputes your total from the catalog and
						re-runs policy on the smaller order before payment.
					</p>
					<ul className="mt-3 divide-y divide-slate-200">
						{order.items.map((item) => (
							<li
								key={item.variant_id}
								className={`flex items-center justify-between py-3 transition-opacity duration-150 ${
									removingVariantId === item.variant_id ? "opacity-30" : ""
								}`}
							>
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
					onClick={() => onReturnToCatalog("Purchase not authorized. Your cart was not charged.")}
					className="w-full rounded-xl bg-black px-5 py-3 font-medium text-white transition hover:bg-slate-800"
				>
					Return to catalog
				</button>
			</div>
		</section>
  );
}
