"use client";

import type { Cart, Product, SuggestResponse } from "./types";
import { avatarStyle, formatINR, initials } from "./helpers";
import { SuggestionCard } from "./SuggestionCard";

// "Your Cart" screen: line items with quantity controls, the shared
// cross-sell SuggestionCard, and the subtotal/checkout actions.
//
// Extracted from checkout.tsx's cart-step JSX as part of item 21
// (PLAN-04-UI-UX-AND-LATENCY.md §A2) -- same JSX, moved. Renders the
// same SuggestionCard the catalog screen uses, at the same position
// (between the item list and the subtotal) the pre-split
// renderSuggestionCard() call occupied here.
//
// Visual pass (UI/UX redesign prompted by real feedback that this
// screen read as flat and "pale" against the page's slate-50
// background): the item list is now a single elevated white card
// (rounded-2xl border + shadow-sm) instead of a bare divided list
// floating directly on the page, each row gets a colored initials chip
// (see avatarStyle/initials in helpers.ts -- there are no product
// images anywhere in this catalog, so this is the only per-item visual
// anchor available), and the subtotal moves into its own dark,
// high-contrast panel so the number buyers actually care about is the
// clear visual endpoint of the list above it, rather than another
// pale bordered box identical in weight to everything else on the
// screen.
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
  const itemCount = cart.items.reduce((sum, item) => sum + item.quantity, 0);

  return (
    <>
      <h2 className="mb-4 text-lg font-semibold text-slate-900">
        Your Cart
      </h2>

      <ul className="divide-y divide-slate-200 rounded-2xl border border-slate-200 bg-white px-5 shadow-sm">
        {cart.items.map((item) => {
          const chip = avatarStyle(item.product_id);
          return (
            <li
              key={item.variant_id}
              className="flex items-center justify-between gap-4 py-4"
            >
              <div className="flex min-w-0 items-center gap-3">
                <span
                  aria-hidden
                  className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-full text-sm font-semibold ${chip.bg} ${chip.text}`}
                >
                  {initials(item.title)}
                </span>
                <div className="min-w-0">
                  <p className="truncate font-semibold text-slate-900">{item.title}</p>
                  <p className="text-sm text-slate-500">
                    {formatINR(item.unit_price)} each
                  </p>
                </div>
              </div>
              <div className="flex shrink-0 items-center gap-4">
                <div className="flex items-center gap-2 rounded-full border border-slate-200 bg-slate-50 px-1.5 py-1">
                  <button
                    onClick={() => onUpdateQuantity(item.variant_id, item.quantity - 1)}
                    disabled={loading || item.quantity <= 1}
                    aria-label={`Decrease quantity of ${item.title}`}
                    className="flex h-6 w-6 items-center justify-center rounded-full text-sm font-medium text-slate-700 transition-colors hover:bg-white disabled:opacity-40"
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
                    className="flex h-6 w-6 items-center justify-center rounded-full text-sm font-medium text-slate-700 transition-colors hover:bg-white disabled:opacity-40"
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
                  className="rounded-lg px-2 py-1 text-xs font-medium text-red-600 transition-colors hover:bg-red-50 hover:text-red-700 disabled:opacity-40"
                >
                  Remove
                </button>
              </div>
            </li>
          );
        })}
      </ul>

      <SuggestionCard
        suggestion={suggestion}
        suggestionLoading={suggestionLoading}
        loading={loading}
        optimistic={optimisticSuggestion}
        onAccept={onAcceptSuggestion}
        onDismiss={onDismissSuggestion}
      />

      <div className="mt-4 flex items-center justify-between rounded-2xl bg-slate-900 p-6 text-white shadow-sm">
        <div>
          <p className="text-sm font-medium text-slate-300">
            Subtotal · {itemCount} item{itemCount === 1 ? "" : "s"}
          </p>
          <p className="mt-1 text-2xl font-bold tracking-tight">
            {formatINR(cart.subtotal)}
          </p>
        </div>
      </div>
      <div className="mt-6 flex gap-3">
        <button
          onClick={onKeepShopping}
          className="rounded-lg border border-slate-300 px-5 py-3 font-medium text-slate-700 transition hover:bg-slate-100"
        >
          Keep shopping
        </button>
        <button
          onClick={onProceedToCheckout}
          disabled={loading || cart.items.length === 0}
          className="rounded-lg bg-black px-5 py-3 font-medium text-white shadow-sm transition hover:bg-slate-800 hover:shadow disabled:opacity-50 disabled:shadow-none"
        >
          Checkout
        </button>
      </div>
    </>
  );
}
