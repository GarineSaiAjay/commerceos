"use client";

import { Skeleton } from "../../lib/format";
import type { Order } from "./types";
import { formatINR } from "./helpers";

// "Order History" screen: GET /orders?merchant_id=... -- merchant-
// scoped for now since there is no buyer identity yet (files/AUTH.md);
// every order for this single-merchant demo qualifies as "history".
//
// Extracted from checkout.tsx's orders-step JSX as part of item 21
// (PLAN-04-UI-UX-AND-LATENCY.md §A2) -- same JSX, moved.
export function OrderHistoryPanel({
  ordersLoading,
  ordersError,
  orders,
  onRetry,
}: {
  ordersLoading: boolean;
  ordersError: string;
  orders: Order[];
  onRetry: () => void;
}) {
  return (
    <>
      {ordersLoading && (
        <div className="space-y-4">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-24 w-full" />
          ))}
        </div>
      )}
      {!ordersLoading && ordersError && (
        <p className="text-sm text-rose-700">
          {ordersError} --{" "}
          <button
            onClick={onRetry}
            className="underline underline-offset-2 hover:text-rose-900"
          >
            try again
          </button>
        </p>
      )}
      {!ordersLoading && !ordersError && orders.length === 0 && (
        <p className="text-sm text-slate-500">
          No orders yet -- completed checkouts will show up here.
        </p>
      )}
      <ul className="space-y-4">
        {orders.map((o) => (
          <li key={o.order_id} className="rounded-xl border border-slate-200 p-5">
            <div className="flex items-start justify-between">
              <div>
                <p className="font-semibold text-slate-900">{o.order_id}</p>
                {o.created_at && (
                  <p className="text-xs text-slate-500">
                    {new Date(o.created_at).toLocaleString()}
                  </p>
                )}
              </div>
              <div className="text-right">
                <p className="text-xs font-medium uppercase tracking-wide text-slate-500">
                  {o.status}
                </p>
                <p className="font-semibold text-slate-900">{formatINR(o.subtotal)}</p>
              </div>
            </div>
            <ul className="mt-3 divide-y divide-slate-100 border-t border-slate-100 pt-3">
              {o.items.map((item) => (
                <li
                  key={item.variant_id}
                  className="flex items-center justify-between py-1.5 text-sm"
                >
                  <span className="text-slate-700">
                    {item.title} × {item.quantity}
                  </span>
                  <span className="text-slate-500">{formatINR(item.total)}</span>
                </li>
              ))}
            </ul>
          </li>
        ))}
      </ul>
    </>
  );
}
