"use client";

import type { Dispatch, SetStateAction } from "react";
import { Skeleton } from "../../lib/format";
import type { Product, Review, SortOption, SuggestResponse } from "./types";
import { defaultVariantFor, formatINR, variantLabel } from "./helpers";

// "Browse Catalog" section of the catalog screen: search/filter/sort
// controls, the product list itself (with per-product variant picker),
// and the product-detail expand panel (item 19, PLAN-02-CATALOG-AND-
// COMMERCE.md §4 / PLAN-03-PROACTIVE-GROWTH-AGENT.md §3).
//
// Extracted from checkout.tsx's catalog-step JSX as part of item 21
// (PLAN-04-UI-UX-AND-LATENCY.md §A2) -- same JSX, moved. Filter/sort/
// variant-picker state stays owned by CheckoutFlow (it's simple control
// state read nowhere else) and is threaded through as raw setState
// functions rather than new wrapper callbacks, to keep this a
// structural move rather than a new abstraction.
export function ProductList({
  products,
  filteredProducts,
  categories,
  searchQuery,
  setSearchQuery,
  selectedCategory,
  setSelectedCategory,
  sortBy,
  setSortBy,
  selectedVariant,
  setSelectedVariant,
  expandedProductId,
  onToggleDetail,
  detailSuggestion,
  detailSuggestionLoading,
  onAcceptDetailSuggestion,
  detailReviews,
  detailReviewsLoading,
  onAddToCart,
  loading,
}: {
  products: Product[];
  filteredProducts: Product[];
  categories: string[];
  searchQuery: string;
  setSearchQuery: Dispatch<SetStateAction<string>>;
  selectedCategory: string | null;
  setSelectedCategory: Dispatch<SetStateAction<string | null>>;
  sortBy: SortOption;
  setSortBy: Dispatch<SetStateAction<SortOption>>;
  selectedVariant: Record<string, string>;
  setSelectedVariant: Dispatch<SetStateAction<Record<string, string>>>;
  expandedProductId: string | null;
  onToggleDetail: (productId: string) => void;
  detailSuggestion: SuggestResponse | null;
  detailSuggestionLoading: boolean;
  onAcceptDetailSuggestion: () => void;
  detailReviews: Review[];
  detailReviewsLoading: boolean;
  onAddToCart: (product: Product, variantId?: string) => void;
  loading: boolean;
}) {
  if (products.length === 0) {
    return (
      <p className="text-sm text-slate-500">
        No products available. Ensure the Commerce Service is running
        and the catalog is seeded.
      </p>
    );
  }

  return (
    <>
      <div className="mb-4 space-y-3">
        <input
          type="text"
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          placeholder="Search by name or feature..."
          className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-slate-500 focus:outline-none"
        />
        <div className="flex flex-wrap items-center gap-2">
          {categories.map((category) => (
            <button
              key={category}
              onClick={() => setSelectedCategory((current) => (current === category ? null : category))}
              className={`rounded-full border px-3 py-1 text-xs font-medium transition ${
                selectedCategory === category
                  ? "border-black bg-black text-white"
                  : "border-slate-300 bg-white text-slate-700 hover:border-slate-400"
              }`}
            >
              {category.replace(/_/g, " ")}
            </button>
          ))}
          <label className="ml-auto flex items-center gap-2 text-sm text-slate-500">
            Sort by
            <select
              value={sortBy}
              onChange={(e) => setSortBy(e.target.value as SortOption)}
              className="rounded-lg border border-slate-300 px-2 py-1.5 text-sm focus:border-slate-500 focus:outline-none"
            >
              <option value="default">Featured</option>
              <option value="price_asc">Price: low to high</option>
              <option value="price_desc">Price: high to low</option>
              <option value="rating">Rating</option>
              <option value="availability">Availability</option>
            </select>
          </label>
        </div>
      </div>

      {filteredProducts.length === 0 ? (
        <p className="text-sm text-slate-500">
          No products match your search or filter.
        </p>
      ) : (
        <ul className="divide-y divide-slate-200">
          {filteredProducts.map((product) => {
            // Only a genuine differentiator (>1 variant) gets a
            // picker rendered -- a product with just its own
            // "<id>-default" entry shows the plain row exactly as
            // before item 10.
            const hasVariants = !!product.variants && product.variants.length > 1;
            const activeVariantId = selectedVariant[product.product_id] ?? defaultVariantFor(product);
            const activeVariant = product.variants?.find((v) => v.variant_id === activeVariantId);
            const displayPrice = activeVariant?.price.amount ?? product.price.amount;
            const displayAvailability = activeVariant?.availability ?? product.availability;

            const isExpanded = expandedProductId === product.product_id;

            return (
              <li key={product.product_id} className="py-4">
                <div className="flex items-center justify-between">
                  <div>
                    <button
                      onClick={() => onToggleDetail(product.product_id)}
                      className="text-left font-semibold text-slate-900 underline-offset-2 hover:underline"
                    >
                      {product.title}
                    </button>
                    <p className="text-sm text-slate-500">
                      {formatINR(displayPrice)} · {displayAvailability} in stock
                      {!!product.review_count && (
                        <>
                          {" "}
                          · <span className="text-amber-600">★ {product.average_rating?.toFixed(1)}</span>{" "}
                          ({product.review_count})
                        </>
                      )}
                    </p>
                    {hasVariants && (
                      <div className="mt-2 flex flex-wrap gap-1">
                        {product.variants!.map((variant) => (
                          <button
                            key={variant.variant_id}
                            onClick={() =>
                              setSelectedVariant((sel) => ({ ...sel, [product.product_id]: variant.variant_id }))
                            }
                            disabled={variant.availability === 0}
                            className={`rounded-full border px-3 py-1 text-xs font-medium transition disabled:cursor-not-allowed disabled:opacity-40 ${
                              variant.variant_id === activeVariantId
                                ? "border-black bg-black text-white"
                                : "border-slate-300 bg-white text-slate-700 hover:border-slate-400"
                            }`}
                          >
                            {variantLabel(variant, product)}
                          </button>
                        ))}
                      </div>
                    )}
                  </div>
                  <button
                    onClick={() => onAddToCart(product, activeVariantId)}
                    disabled={loading || displayAvailability === 0}
                    className="rounded-lg bg-black px-4 py-2 text-sm font-medium text-white transition hover:bg-slate-800 disabled:opacity-50"
                  >
                    {displayAvailability === 0 ? "Out of stock" : "Add to cart"}
                  </button>
                </div>

                {isExpanded && (
                  <div className="mt-3 rounded-lg bg-slate-50 p-4 text-sm text-slate-600">
                    {!!product.features?.length && (
                      <div className="flex flex-wrap gap-1.5">
                        {product.features.map((feature) => (
                          <span
                            key={feature}
                            className="rounded-full border border-slate-200 bg-white px-2.5 py-0.5 text-xs text-slate-600"
                          >
                            {feature.replace(/_/g, " ")}
                          </span>
                        ))}
                      </div>
                    )}
                    <p className="mt-3">
                      {product.return_policy?.days
                        ? `${product.return_policy.days}-day returns`
                        : "No returns"}
                      {!!product.shipping?.estimated_days && (
                        <> · ships in {product.shipping.estimated_days} day{product.shipping.estimated_days > 1 ? "s" : ""}</>
                      )}
                    </p>

                    {/* Reviews (item 13, PLAN-02-CATALOG-AND-COMMERCE.md
                        §4: "and -- once §2 ships -- reviews"). The row's
                        own average_rating/review_count summary above
                        (a live JOIN aggregate, catalog.Product) can lag
                        this list by up to the catalog cache's TTL (item
                        23) -- a brand-new review may not move that
                        number for a few seconds even though it already
                        appears here, since this list is fetched fresh
                        on every expand. */}
                    {detailReviewsLoading && (
                      <div className="mt-3 space-y-2">
                        <Skeleton className="h-3 w-full" />
                        <Skeleton className="h-3 w-2/3" />
                      </div>
                    )}
                    {!detailReviewsLoading && detailReviews.length === 0 && (
                      <p className="mt-3 text-xs text-slate-400">No reviews yet.</p>
                    )}
                    {!detailReviewsLoading && detailReviews.length > 0 && (
                      <ul className="mt-3 divide-y divide-slate-200 border-t border-slate-200">
                        {detailReviews.map((review) => (
                          <li key={review.id} className="py-3">
                            <div className="flex items-center gap-2">
                              <span
                                className="text-amber-500"
                                aria-label={`${review.rating} out of 5 stars`}
                              >
                                <span aria-hidden>{"★".repeat(review.rating)}</span>
                                <span className="text-slate-300" aria-hidden>{"★".repeat(5 - review.rating)}</span>
                              </span>
                              <span className="text-xs font-medium text-slate-700">
                                {review.buyer_reference}
                              </span>
                              {review.verified_purchase && (
                                <span className="rounded-full bg-emerald-50 px-2 py-0.5 text-[10px] font-medium uppercase tracking-wide text-emerald-700">
                                  Verified purchase
                                </span>
                              )}
                              <span className="ml-auto text-xs text-slate-400">
                                {new Date(review.created_at).toLocaleDateString()}
                              </span>
                            </div>
                            {review.comment && (
                              <p className="mt-1 text-sm text-slate-600">{review.comment}</p>
                            )}
                          </li>
                        ))}
                      </ul>
                    )}

                    {detailSuggestionLoading && (
                      <div className="mt-3 rounded-lg border border-indigo-200 bg-indigo-50 p-3">
                        <Skeleton className="h-3 w-24" />
                        <Skeleton className="mt-2 h-4 w-1/2" />
                      </div>
                    )}
                    {!detailSuggestionLoading && detailSuggestion?.product && (
                      <div className="mt-3 rounded-lg border border-indigo-200 bg-indigo-50 p-3">
                        <p className="text-xs font-medium uppercase tracking-wide text-indigo-700">
                          Frequently paired with
                        </p>
                        <div className="mt-1 flex items-center justify-between">
                          <p className="text-sm text-slate-900">
                            {detailSuggestion.product.title} -- {formatINR(detailSuggestion.product.price)}
                          </p>
                          <button
                            onClick={onAcceptDetailSuggestion}
                            disabled={loading}
                            className="rounded-lg bg-indigo-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
                          >
                            Add
                          </button>
                        </div>
                      </div>
                    )}
                  </div>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </>
  );
}
