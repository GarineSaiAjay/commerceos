"use client";

import type { Payment } from "./types";
import { formatINR } from "./helpers";

// Extracted from checkout.tsx's step === "pay" JSX -- shown while the
// Razorpay checkout window is open, waiting on the webhook/redirect to
// resolve. Same JSX, moved; the "Cancel payment" button's inline
// multi-setState arrow function is now a single onCancel callback prop
// (CheckoutFlow's cancelPayment), matching how every other composite
// reset action in this split (PayStep/FailedStep/CompleteStep) is
// passed down rather than threading five individual setters through.
export function PayStep({
  payment,
  onCancel,
}: {
  payment: Payment;
  onCancel: () => void;
}) {
  return (
          <section>
            <h2 className="mb-4 text-lg font-semibold text-slate-900">
              Complete Payment
            </h2>
            <p className="text-sm text-slate-500">
              The Razorpay checkout window should have opened. Complete the
              payment there to finish your order.
            </p>
            <div className="mt-4 rounded-xl border border-slate-200 p-5">
              <p className="text-sm text-slate-500">Amount due</p>
              <p className="text-2xl font-bold text-slate-900">
                {formatINR(payment.amount)}
              </p>
            </div>
            <button
              onClick={onCancel}
              className="mt-4 w-full rounded-xl border border-slate-300 px-5 py-3 font-medium text-slate-700 hover:bg-slate-100"
            >
              Cancel payment
            </button>
          </section>
  );
}
