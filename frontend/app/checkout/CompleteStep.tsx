"use client";

import { Skeleton } from "../../lib/format";
import type { Order, Payment, ReviewEntry, Run, SuggestResponse } from "./types";
import { formatINR } from "./helpers";
import { AuditTrailPanel } from "./AuditTrailPanel";

// Extracted from checkout.tsx's step === "complete" JSX -- the
// post-checkout screen: payment confirmation, the audit trail, the
// per-item "Rate your order" prompt (PLAN-02-CATALOG-AND-COMMERCE.md
// §2), and the "Complete the set" post-checkout cross-sell
// (PLAN-03-PROACTIVE-GROWTH-AGENT.md §4). Same JSX, moved;
// onRate/onComment/onSubmitReview/onAcceptPostCheckoutSuggestion/
// onStartNewOrder are now callback props instead of closures over
// CheckoutFlow's rateProduct/commentOnProduct/submitReview/
// acceptPostCheckoutSuggestion and the "Start a new order" button's
// inline multi-setState reset.
//
// The wrapper condition in checkout.tsx (`step === "complete" &&
// payment`) only guarantees payment, not order -- the review section
// below already null-checks it (`{order && order.items.length > 0 &&
// (...)}`), so order stays Order | null here, unlike
// PolicyRejectedStep's order (guaranteed non-null by its own wrapper).
export function CompleteStep({
  payment,
  runId,
  run,
  runLoading,
  order,
  reviews,
  onRate,
  onComment,
  onSubmitReview,
  postCheckoutSuggestionLoading,
  postCheckoutSuggestion,
  onAcceptPostCheckoutSuggestion,
  loading,
  onStartNewOrder,
}: {
  payment: Payment;
  runId: string;
  run: Run | null;
  runLoading: boolean;
  order: Order | null;
  reviews: Record<string, ReviewEntry>;
  onRate: (productId: string, rating: number) => void;
  onComment: (productId: string, comment: string) => void;
  onSubmitReview: (orderId: string, productId: string) => void;
  postCheckoutSuggestionLoading: boolean;
  postCheckoutSuggestion: SuggestResponse | null;
  onAcceptPostCheckoutSuggestion: () => void;
  loading: boolean;
  onStartNewOrder: () => void;
}) {
  return (
          <section>
            <div className="rounded-xl border border-slate-200 p-6">
              <p className="text-sm text-slate-500">Payment status</p>
              <p className="mt-1 text-xl font-bold text-slate-900">
                {payment.status}
              </p>
              <p className="mt-4 text-sm text-slate-500">
                Payment ID: {payment.payment_id}
              </p>
              <p className="text-sm text-slate-500">
                Order ID: {payment.order_id}
              </p>
            </div>
            <AuditTrailPanel runId={runId} run={run} runLoading={runLoading} />
            {order && order.items.length > 0 && (
              <div className="mt-6 rounded-xl border border-slate-200 p-6">
                <p className="text-sm font-semibold text-slate-900">Rate your order</p>
                <p className="mt-1 text-xs text-slate-500">
                  Optional -- helps other buyers, and the seller sees it too.
                </p>
                <ul className="mt-4 divide-y divide-slate-100">
                  {order.items.map((item) => {
                    const entry: ReviewEntry = reviews[item.product_id] ?? {
                      rating: 0,
                      comment: "",
                      submitting: false,
                      submitted: false,
                      error: "",
                    };
                    return (
                      <li key={item.product_id} className="py-4 first:pt-0 last:pb-0">
                        <p className="text-sm font-medium text-slate-900">{item.title}</p>
                        {entry.submitted ? (
                          <p className="mt-2 text-sm text-emerald-700">Thanks for your review!</p>
                        ) : (
                          <>
                            <div className="mt-2 flex gap-1">
                              {[1, 2, 3, 4, 5].map((n) => (
                                <button
                                  key={n}
                                  type="button"
                                  onClick={() => onRate(item.product_id, n)}
                                  aria-label={`Rate ${item.title} ${n} star${n > 1 ? "s" : ""}`}
                                  className={`text-lg leading-none ${
                                    n <= entry.rating ? "text-amber-500" : "text-slate-300"
                                  }`}
                                >
                                  ★
                                </button>
                              ))}
                            </div>
                            <textarea
                              value={entry.comment}
                              onChange={(e) => onComment(item.product_id, e.target.value)}
                              placeholder="Optional comment"
                              rows={2}
                              className="mt-2 w-full rounded-lg border border-slate-200 px-3 py-2 text-sm text-slate-900 focus:border-slate-400 focus:outline-none"
                            />
                            {entry.error && (
                              <p className="mt-1 text-xs text-rose-600">{entry.error}</p>
                            )}
                            <button
                              onClick={() => onSubmitReview(order.order_id, item.product_id)}
                              disabled={entry.rating < 1 || entry.submitting}
                              className="mt-2 rounded-lg bg-slate-900 px-3 py-1.5 text-xs font-medium text-white transition hover:bg-slate-800 disabled:opacity-40"
                            >
                              {entry.submitting ? "Submitting..." : "Submit review"}
                            </button>
                          </>
                        )}
                      </li>
                    );
                  })}
                </ul>
              </div>
            )}

            {postCheckoutSuggestionLoading && (
              <div className="mt-6 rounded-xl border border-indigo-200 bg-indigo-50 p-6">
                <Skeleton className="h-3 w-28" />
                <Skeleton className="mt-2 h-4 w-2/3" />
                <Skeleton className="mt-3 h-9 w-40" />
              </div>
            )}
            {!postCheckoutSuggestionLoading && postCheckoutSuggestion?.product && (
              <div className="mt-6 rounded-xl border border-indigo-200 bg-indigo-50 p-6">
                <p className="text-xs font-medium uppercase tracking-wide text-indigo-700">
                  Complete the set
                </p>
                <p className="mt-1 font-semibold text-slate-900">
                  Add {postCheckoutSuggestion.product.title} -- {formatINR(postCheckoutSuggestion.product.price)}
                </p>
                {postCheckoutSuggestion.recommendation && (
                  <p className="mt-1 text-sm text-indigo-800">{postCheckoutSuggestion.recommendation.reason}</p>
                )}
                <button
                  onClick={onAcceptPostCheckoutSuggestion}
                  disabled={loading}
                  className="mt-3 rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
                >
                  Add -- starts a new order
                </button>
              </div>
            )}

            <button
              onClick={onStartNewOrder}
              className="mt-6 w-full rounded-xl bg-black px-5 py-3.5 font-medium text-white transition hover:bg-slate-800"
            >
              Start a new order
            </button>
          </section>
  );
}
