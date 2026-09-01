"use client";

import type { Cart, Product, SuggestResponse } from "./types";
import { formatINR } from "./helpers";
import { SuggestionCard } from "./SuggestionCard";

// "Your Cart" screen: line items with quantity controls, the shared
// cross-sell SuggestionCard, and the subtotal/checkout actions.
//
// Extracted from checkout.tsx's cart-step JSX as part of item 21
// (PLAN-04-UI-UX-AND-LATENCY.md §A2) -- same JSX, moved. Renders the
// same SuggestionCard the catalog screen uses, at the same position
// (between the item list and the subtotal) the pre-split
// renderSuggestionCard() call occupied here.
export function CartPanel({
  cart,
  loading,
  onUpdateQuantity,
  onRemoveItem,
  onKeepShopping,
  onProceedToCheckout,
  suggestion,
  suggestionLoading,
  optimisticSuggestion,
  onAcceptSuggestion,
  onDismissSuggestion,
}: {
  cart: Cart;
  loading: boolean;
  onUpdateQuantity: (variantId: string, quantity: number) => void;
  onRemoveItem: (variantId: string) => void;
  onKeepShopping: () => void;
  onProceedToCheckout: () => void;
  suggestion: SuggestResponse | null;
  suggestionLoading: boolean;
  optimisticSuggestion: Product | null;
  onAcceptSuggestion: () => void;
  onDismissSuggestion: () => void;
}) {
  return (
    <>
      <h2 className="mb-4 text-lg font-semibold text-slate-900">
        Your Cart
      </h2>
      <ul className="divide-y divide-slate-200">
        {cart.items.map((item) => (
          <li
            key={item.variant_id}
            className="flex items-center justify-between py-4"
          >
            <div>
              <p className="font-semibold text-slate-900">{item.title}</p>
              <p className="text-sm text-slate-500">
                {formatINR(item.unit_price)} each
              </p>
            </div>
            <div className="flex items-center gap-4">
              <div className="flex items-center gap-2">
                <button
                  onClick={() => onUpdateQuantity(item.variant_id, item.quantity - 1)}
                  disabled={loading || item.quantity <= 1}
                  aria-label={`Decrease quantity of ${item.title}`}
                  className="h-7 w-7 rounded border border-slate-300 text-sm font-medium text-slate-700 hover:bg-slate-100 disabled:opacity-40"
                >
                  −
                </button>
                <span className="w-5 text-center text-sm font-medium text-slate-900">
                  {item.quantity}
                </span>
                <button
                  onClick={() => onUpdateQuantity(item.variant_id, item.quantity + 1)}
                  disabled={loading}
                  aria-label={`Increase quantity of ${item.title}`}
                  className="h-7 w-7 rounded border border-slate-300 text-sm font-medium text-slate-700 hover:bg-slate-100 disabled:opacity-40"
                >
                  +
                </button>
              </div>
              <p className="w-20 text-right font-semibold text-slate-900">
                {formatINR(item.total)}
              </p>
              <button
                onClick={() => onRemoveItem(item.variant_id)}
                disabled={loading}
                className="text-xs font-medium text-red-600 hover:text-red-700 disabled:opacity-40"
              >
                Remove
              </button>
            </div>
          </li>
        ))}
      </ul>

      <SuggestionCard
        suggestion={suggestion}
        suggestionLoading={suggestionLoading}
        loading={loading}
        optimistic={optimisticSuggestion}
        onAccept={onAcceptSuggestion}
        onDismiss={onDismissSuggestion}
      />

      <div className="mt-4 flex items-center justify-between rounded-xl border border-slate-200 p-5">
        <p className="font-semibold text-slate-900">Subtotal</p>
        <p className="text-lg font-semibold text-slate-900">
          {formatINR(cart.subtotal)}
        </p>
      </div>
      <div className="mt-6 flex gap-3">
        <button
          onClick={onKeepShopping}
          className="rounded-lg border border-slate-300 px-5 py-3 font-medium text-slate-700 hover:bg-slate-100"
        >
          Keep shopping
        </button>
        <button
          onClick={onProceedToCheckout}
          disabled={loading || cart.items.length === 0}
          className="rounded-lg bg-black px-5 py-3 font-medium text-white transition hover:bg-slate-800 disabled:opacity-50"
        >
          Checkout
        </button>
      </div>
    </>
  );
}
