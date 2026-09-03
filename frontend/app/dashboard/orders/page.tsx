"use client";

import { useCallback, useEffect, useState } from "react";
import { actionLabel, formatINR, formatTime } from "../../../lib/format";
import { authFetch } from "../../../lib/auth";
import { downloadFile } from "../../../lib/download";

// Mirrors backend/commerce/order/model.go's Order/OrderItem JSON shape.
// payment_status (item 15, PLAN-05-SELLER-DASHBOARD.md §2) comes from a
// LEFT JOIN in GetOrder/ListOrders -- empty string, not "captured" or any
// other value, until a payment has actually been created for the order.
type OrderItem = {
  product_id: string;
  variant_id: string;
  title: string;
  quantity: number;
  unit_price: number;
  total: number;
};

type Order = {
  order_id: string;
  merchant_id: string;
  cart_id: string;
  currency: string;
  subtotal: number;
  discount_amount: number;
  campaign_id?: string;
  status: string;
  items: OrderItem[];
  created_at: string;
  payment_status?: string;
  // Populated once the order's payment was authorized and created
  // (payment.Service.CreatePaymentOrder tags it there) -- empty for a
  // draft order that never reached a successful payment. PLAN-05-
  // SELLER-DASHBOARD.md §2's "Orders -> Runs audit-trail link".
  run_id?: string;
};

// Mirrors backend/commerce/payment/model.go's Payment JSON shape,
// fetched separately per order (GET /orders/{id}/payment, new in item
// 15) -- not every order has one yet (e.g. a draft order that never
// reached checkout), so this stays a distinct fetch from the order
// itself rather than something ListOrders/GetOrder always embeds.
type Payment = {
  payment_id: string;
  order_id: string;
  provider: string;
  provider_order_id: string;
  provider_payment_id: string;
  amount: number;
  currency: string;
  status: string;
};

const ORDER_STATUS_STYLE: Record<string, string> = {
  paid: "bg-emerald-100 text-emerald-800",
  completed: "bg-emerald-100 text-emerald-800",
  fulfillment_pending: "bg-sky-100 text-sky-800",
  payment_pending: "bg-amber-100 text-amber-800",
  authorized: "bg-amber-100 text-amber-800",
  draft: "bg-slate-200 text-slate-600",
  failed: "bg-rose-100 text-rose-800",
  cancelled: "bg-slate-200 text-slate-600",
};

const PAYMENT_STATUS_STYLE: Record<string, string> = {
  paid: "bg-emerald-100 text-emerald-800",
  captured: "bg-emerald-100 text-emerald-800",
  completed: "bg-emerald-100 text-emerald-800",
  authorized: "bg-amber-100 text-amber-800",
  pending: "bg-amber-100 text-amber-800",
  attempted: "bg-amber-100 text-amber-800",
  created: "bg-slate-200 text-slate-600",
  failed: "bg-rose-100 text-rose-800",
  refunded: "bg-slate-200 text-slate-600",
};

function StatusBadge({ status, palette }: { status: string; palette: Record<string, string> }) {
  return (
    <span className={`rounded-full px-2 py-0.5 text-xs font-semibold ${palette[status] ?? "bg-slate-200 text-slate-600"}`}>
      {status ? actionLabel(status) : "—"}
    </span>
  );
}

export default function OrdersPage() {
  const [orders, setOrders] = useState<Order[]>([]);
  const [error, setError] = useState("");
  const [selected, setSelected] = useState<Order | null>(null);
  const [payment, setPayment] = useState<Payment | null>(null);
  const [paymentState, setPaymentState] = useState<"idle" | "loading" | "found" | "none">("idle");
  const [exporting, setExporting] = useState(false);
  const [exportError, setExportError] = useState("");

  const loadOrders = useCallback(() => {
    authFetch("/dashboard/orders", { cache: "no-store" })
      .then((r) => {
        if (!r.ok) throw new Error("Could not load orders.");
        return r.json();
      })
      .then((data: Order[]) => setOrders(data ?? []))
      .catch(() => setError("Could not load orders."));
  }, []);

  useEffect(() => {
    loadOrders();
  }, [loadOrders]);

  // item 27 (P2, PLAN-05-SELLER-DASHBOARD.md section 6): export every
  // order this page is showing as a CSV, via the same operator-scoped
  // GET /dashboard/orders/export the list above reads from -- so this
  // can never disagree with what's on screen.
  async function exportCSV() {
    setExporting(true);
    setExportError("");
    try {
      await downloadFile("/dashboard/orders/export", "orders.csv");
    } catch (cause) {
      setExportError(cause instanceof Error ? cause.message : "Could not export orders");
    } finally {
      setExporting(false);
    }
  }

  async function selectOrder(order: Order) {
    setSelected(order);
    setPayment(null);
    setPaymentState("loading");
    try {
      const res = await authFetch(`/orders/${order.order_id}/payment`, { cache: "no-store" });
      if (res.status === 404) {
        setPaymentState("none");
        return;
      }
      if (!res.ok) throw new Error("payment lookup failed");
      setPayment((await res.json()) as Payment);
      setPaymentState("found");
    } catch {
      setPaymentState("none");
    }
  }

  return (
    <main className="px-5 py-7 sm:px-8 lg:px-10">
      <header className="flex flex-wrap items-start justify-between gap-4 border-b border-slate-200 pb-6">
        <div>
          <h1 className="text-3xl font-semibold tracking-tight">Orders</h1>
          <p className="mt-2 max-w-xl text-sm leading-6 text-slate-600">
            Every order placed against this merchant, most recent first, with its linked payment
            record. Read-only -- no refund or cancel action exists in the payment service yet beyond
            the recovery/retry flow for a failed payment attempt.
          </p>
        </div>
        <button
          onClick={exportCSV}
          disabled={exporting || orders.length === 0}
          className="shrink-0 rounded-lg border border-slate-300 px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 disabled:opacity-50"
        >
          {exporting ? "Exporting…" : "Export CSV"}
        </button>
      </header>

      {error && (
        <p role="alert" className="mt-6 rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800">
          {error}
        </p>
      )}

      {exportError && (
        <p role="alert" className="mt-6 rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800">
          {exportError}
        </p>
      )}

      <div className="mt-8 grid gap-6 lg:grid-cols-2">
        <section className="rounded-2xl border border-slate-200 bg-white shadow-sm">
          <div className="border-b border-slate-100 px-5 py-4">
            <h2 className="text-base font-semibold">All orders</h2>
          </div>
          {orders.length === 0 ? (
            <p className="p-5 text-sm text-slate-600">No orders yet.</p>
          ) : (
            <ul className="max-h-[32rem] divide-y divide-slate-100 overflow-y-auto">
              {orders.map((order) => (
                <li key={order.order_id}>
                  <button
                    onClick={() => selectOrder(order)}
                    className={`flex w-full items-center justify-between gap-4 px-5 py-4 text-left hover:bg-slate-50 ${
                      selected?.order_id === order.order_id ? "bg-slate-50" : ""
                    }`}
                  >
                    <div className="min-w-0">
                      <p className="truncate font-medium text-slate-900">
                        {order.items.length} item{order.items.length === 1 ? "" : "s"} · {order.order_id}
                      </p>
                      <p className="mt-0.5 text-xs text-slate-500">{formatTime(order.created_at)}</p>
                      <div className="mt-1.5 flex flex-wrap gap-1.5">
                        <StatusBadge status={order.status} palette={ORDER_STATUS_STYLE} />
                        {!!order.payment_status && <StatusBadge status={order.payment_status} palette={PAYMENT_STATUS_STYLE} />}
                      </div>
                    </div>
                    <p className="shrink-0 text-sm font-semibold text-slate-900">{formatINR(order.subtotal)}</p>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </section>

        <section className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
          <h2 className="text-base font-semibold">Order detail</h2>
          {!selected ? (
            <p className="mt-4 rounded-lg bg-slate-50 p-5 text-sm leading-6 text-slate-600">
              Select an order on the left to see its full line items and linked payment record.
            </p>
          ) : (
            <div className="mt-4 space-y-5 text-sm">
              <dl className="space-y-4">
                <div>
                  <dt className="text-xs font-medium uppercase tracking-wide text-slate-500">Order ID</dt>
                  <dd className="mt-1 font-mono text-xs">{selected.order_id}</dd>
                </div>
                <div>
                  <dt className="text-xs font-medium uppercase tracking-wide text-slate-500">Status</dt>
                  <dd className="mt-1">
                    <StatusBadge status={selected.status} palette={ORDER_STATUS_STYLE} />
                  </dd>
                </div>
                <div>
                  <dt className="text-xs font-medium uppercase tracking-wide text-slate-500">Items</dt>
                  <dd className="mt-1 space-y-1.5">
                    {selected.items.map((item) => (
                      <p key={item.variant_id} className="flex items-center justify-between gap-4">
                        <span className="text-slate-700">
                          {item.title} × {item.quantity}
                        </span>
                        <span className="shrink-0 font-medium text-slate-900">{formatINR(item.total)}</span>
                      </p>
                    ))}
                  </dd>
                </div>
                {selected.discount_amount > 0 && (
                  <div>
                    <dt className="text-xs font-medium uppercase tracking-wide text-slate-500">Discount</dt>
                    <dd className="mt-1">
                      -{formatINR(selected.discount_amount)}
                      {selected.campaign_id && <span className="text-slate-500"> · campaign {selected.campaign_id}</span>}
                    </dd>
                  </div>
                )}
                <div>
                  <dt className="text-xs font-medium uppercase tracking-wide text-slate-500">Subtotal charged</dt>
                  <dd className="mt-1 text-base font-semibold text-slate-900">{formatINR(selected.subtotal)}</dd>
                </div>
                <div>
                  <dt className="text-xs font-medium uppercase tracking-wide text-slate-500">Placed</dt>
                  <dd className="mt-1">{formatTime(selected.created_at)}</dd>
                </div>
              </dl>

              <div className="border-t border-slate-100 pt-4">
                <h3 className="text-xs font-medium uppercase tracking-wide text-slate-500">Linked payment record</h3>
                {paymentState === "loading" && <p className="mt-2 text-xs text-slate-400">Loading payment record...</p>}
                {paymentState === "none" && (
                  <p className="mt-2 text-sm text-slate-600">No payment has been created for this order yet.</p>
                )}
                {paymentState === "found" && payment && (
                  <dl className="mt-2 space-y-2">
                    <div className="flex items-center justify-between">
                      <dt className="text-slate-500">Status</dt>
                      <dd>
                        <StatusBadge status={payment.status} palette={PAYMENT_STATUS_STYLE} />
                      </dd>
                    </div>
                    <div className="flex items-center justify-between">
                      <dt className="text-slate-500">Provider</dt>
                      <dd className="capitalize">{payment.provider}</dd>
                    </div>
                    <div className="flex items-center justify-between gap-4">
                      <dt className="shrink-0 text-slate-500">Provider order ID</dt>
                      <dd className="truncate font-mono text-xs">{payment.provider_order_id}</dd>
                    </div>
                    {!!payment.provider_payment_id && (
                      <div className="flex items-center justify-between gap-4">
                        <dt className="shrink-0 text-slate-500">Provider payment ID</dt>
                        <dd className="truncate font-mono text-xs">{payment.provider_payment_id}</dd>
                      </div>
                    )}
                    <div className="flex items-center justify-between">
                      <dt className="text-slate-500">Amount</dt>
                      <dd className="font-medium text-slate-900">{formatINR(payment.amount)}</dd>
                    </div>
                  </dl>
                )}
              </div>

              {selected.run_id ? (
                <div className="border-t border-slate-100 pt-4">
                  <h3 className="text-xs font-medium uppercase tracking-wide text-slate-500">
                    Policy audit trail
                  </h3>
                  <p className="mt-2 text-sm leading-6 text-slate-600">
                    This order&apos;s payment was authorized by{" "}
                    <a
                      href={`/dashboard/runs?run_id=${selected.run_id}`}
                      className="font-mono text-xs underline hover:text-slate-900"
                    >
                      {selected.run_id}
                    </a>
                    . Open it in Agent Runs to replay the full proposed → risk-assessed →
                    policy-evaluated → authorized sequence.
                  </p>
                </div>
              ) : (
                <p className="border-t border-slate-100 pt-4 text-xs leading-5 text-slate-400">
                  {selected.payment_status
                    ? "This order predates run-linked payments, so its specific authorizing run can't be looked up."
                    : "This order has no payment yet, so it isn't tied to an authorizing run."}{" "}
                  Browse{" "}
                  <a href="/dashboard/runs" className="underline hover:text-slate-600">
                    Agent Runs
                  </a>{" "}
                  to review the audit trail generally.
                </p>
              )}
            </div>
          )}
        </section>
      </div>
    </main>
  );
}
