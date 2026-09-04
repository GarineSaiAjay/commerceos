"use client";

import { useCallback, useEffect, useState } from "react";
import { formatINR } from "../../../lib/format";
import { authFetch } from "../../../lib/auth";
import { VariantEditor, type ProductVariant } from "./VariantEditor";

// Product mirrors backend/commerce/catalog/product.go's wire shape.
// average_rating/review_count (PLAN-02-CATALOG-AND-COMMERCE.md §2, item
// 11) are already computed server-side by every GET -- this page just
// displays them, same as checkout.tsx's buyer-facing catalog list.
type Product = {
  product_id: string;
  title: string;
  price: { amount: number; currency: string };
  availability: number;
  features: string[];
  compatibility: string[];
  use_cases: string[];
  merchant: { id: string };
  return_policy: { days: number };
  shipping: { estimated_days: number };
  average_rating: number;
  review_count: number;
  // Present on every GET /products response (catalog.Product.Variants,
  // json:"variants,omitempty") -- optional here only because a stale
  // cached entry (see cachedProducts below) predates this field.
  variants?: ProductVariant[];
  // attributes/purchase_constraints (backend/commerce/catalog/product.go)
  // have no editor UI on this page yet -- see editingAttributes/
  // editingPurchaseConstraints below for why this page still has to
  // carry them through untouched on every save regardless.
  attributes?: Record<string, unknown>;
  purchase_constraints?: Record<string, unknown>;
};

// Item 23 (PLAN-04-UI-UX-AND-LATENCY.md §B2, "client-side" layer): a
// short in-memory TTL cache so navigating away from this tab and back
// (a genuine remount -- CatalogPage's useEffect re-runs load() every
// time) doesn't refetch the whole catalog for no reason. Module-level,
// not component state, so it survives the remount itself; a page
// reload still clears it, same as any in-memory cache. Deliberately
// NOT applied to the buyer-facing checkout.tsx catalog -- that page
// has no client-side refetch at all to cache against (it receives
// initialProducts once, server-rendered by frontend/app/page.tsx,
// which gets its own module-level TTL cache instead).
let cachedProducts: { data: Product[]; expiresAt: number } | null = null;
const PRODUCTS_CACHE_TTL_MS = 30_000;

// Single-merchant demo -- same hardcoded convention checkout.tsx's
// MERCHANT_ID and growth/suggest.go's DemoBudgetCeiling already use.
// There is no multi-merchant operator selection anywhere in this
// project yet (files/AUTH.md).
const MERCHANT_ID = "merchant_001";

type FormState = {
  productId: string;
  title: string;
  priceRupees: string;
  availability: string;
  features: string;
  useCases: string;
  compatibility: string;
  returnDays: string;
  shippingDays: string;
};

const EMPTY_FORM: FormState = {
  productId: "",
  title: "",
  priceRupees: "",
  availability: "0",
  features: "",
  useCases: "",
  compatibility: "",
  returnDays: "7",
  shippingDays: "3",
};

// toTags parses the comma-entry tag inputs PLAN-05-SELLER-DASHBOARD.md
// §1 calls for -- "features/use_cases/compatibility as tag inputs
// (comma-entry, matching the JSON array shape the backend already
// expects)".
function toTags(value: string): string[] {
  return value
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
}

function productToForm(p: Product): FormState {
  return {
    productId: p.product_id,
    title: p.title,
    priceRupees: (p.price.amount / 100).toString(),
    availability: String(p.availability),
    features: p.features?.join(", ") ?? "",
    useCases: p.use_cases?.join(", ") ?? "",
    compatibility: p.compatibility?.join(", ") ?? "",
    returnDays: String(p.return_policy?.days ?? 0),
    shippingDays: String(p.shipping?.estimated_days ?? 0),
  };
}

// /dashboard/catalog (PLAN-05-SELLER-DASHBOARD.md §1, ROADMAP-
// PRIORITIZED.md P1 item 14): list + create/edit + delete over the
// catalog CRUD routes that already exist in backend/commerce/catalog
// and have been operator-gated since the P0 auth fix
// (fix/authenticate-product-mutation-routes) -- this page is the
// legitimate front door to that now-gated write path, not a new one.
// Each product row can expand into its own VariantEditor (PLAN-05 §1's
// previously-deferred "variant sub-editor" line, plus PLAN-02 §5.2 --
// item 10 shipped real variants with no dashboard editor for them
// until this) for inline SKU/price/availability add-edit-delete.
export default function CatalogPage() {
  const [products, setProducts] = useState<Product[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  // editingId: null = form closed, "" = creating a new product, any
  // other string = editing that product_id.
  const [editingId, setEditingId] = useState<string | null>(null);
  const [form, setForm] = useState<FormState>(EMPTY_FORM);
  // editingAttributes/editingPurchaseConstraints: full-codebase re-audit,
  // P1. This form has no fields for catalog.Product's attributes/
  // purchase_constraints (no editor UI for either exists yet), but
  // UpdateProduct (backend/commerce/catalog/handler.go) is a full
  // replace, not a partial merge -- it decodes the whole Product from
  // the PATCH body and repository.go's UpdateProduct unconditionally
  // overwrites both columns with whatever came in. Omitting these keys
  // from save()'s body decodes as Go nil maps, so saving *any* other
  // field via this form (even just a price change) was silently
  // wiping a product's attributes and purchase_constraints. Captured
  // from the product being edited and re-sent as-is on every save so
  // this page can safely omit editor fields for them without losing
  // data it doesn't have UI for -- reset to {} on create (nothing to
  // preserve for a brand new product).
  const [editingAttributes, setEditingAttributes] = useState<Record<string, unknown>>({});
  const [editingPurchaseConstraints, setEditingPurchaseConstraints] = useState<Record<string, unknown>>({});
  const [saving, setSaving] = useState(false);
  const [formError, setFormError] = useState("");

  const [confirmingId, setConfirmingId] = useState<string | null>(null);
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const [deleteError, setDeleteError] = useState<Record<string, string>>({});

  // expandedId tracks which single product's VariantEditor is open --
  // one at a time, same "one thing expanded" convention confirmingId
  // already uses for delete confirmation.
  const [expandedId, setExpandedId] = useState<string | null>(null);

  // skipCache forces a fresh, uncached fetch -- always passed true
  // after this page's own save()/remove() below, so an operator's own
  // edit is never masked by a stale cached list for up to the TTL
  // (the same correctness requirement catalog/service.go's Redis
  // cache invalidation enforces server-side).
  const load = useCallback((skipCache = false) => {
    if (!skipCache && cachedProducts && cachedProducts.expiresAt > Date.now()) {
      const cached = cachedProducts.data;
      // Deferred via Promise.resolve() so a cache hit still resolves
      // in a microtask rather than synchronously inside the mount
      // effect below (react-hooks/set-state-in-effect) -- mirrors the
      // async boundary the network path already has naturally via
      // .then().
      Promise.resolve().then(() => {
        setProducts(cached);
        setLoading(false);
      });
      return;
    }
    setLoading(true);
    authFetch("/products", { cache: "no-store" })
      .then((r) => r.json())
      .then((data: Product[]) => {
        const list: Product[] = data ?? [];
        cachedProducts = { data: list, expiresAt: Date.now() + PRODUCTS_CACHE_TTL_MS };
        setProducts(list);
      })
      .catch(() => setError("Could not load the catalog."))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  function startCreate() {
    setEditingId("");
    setForm(EMPTY_FORM);
    setEditingAttributes({});
    setEditingPurchaseConstraints({});
    setFormError("");
  }

  function startEdit(p: Product) {
    setEditingId(p.product_id);
    setForm(productToForm(p));
    setEditingAttributes(p.attributes ?? {});
    setEditingPurchaseConstraints(p.purchase_constraints ?? {});
    setFormError("");
  }

  function cancelForm() {
    setEditingId(null);
    setForm(EMPTY_FORM);
    setEditingAttributes({});
    setEditingPurchaseConstraints({});
    setFormError("");
  }

  async function save() {
    const isCreate = editingId === "";
    const priceRupees = Number(form.priceRupees);

    if (!form.title.trim()) {
      setFormError("Title is required.");
      return;
    }
    if (isCreate && !form.productId.trim()) {
      setFormError("Product ID is required.");
      return;
    }
    if (!Number.isFinite(priceRupees) || priceRupees < 0) {
      setFormError("Price must be a non-negative number.");
      return;
    }

    // Wire shape matches catalog.Product's JSON tags exactly --
    // CreateProduct/UpdateProduct decode the request body directly into
    // that struct (backend/commerce/catalog/handler.go).
    const body = {
      product_id: isCreate ? form.productId.trim() : editingId,
      title: form.title.trim(),
      price: { amount: Math.round(priceRupees * 100), currency: "INR" },
      availability: Math.max(0, Math.round(Number(form.availability) || 0)),
      features: toTags(form.features),
      use_cases: toTags(form.useCases),
      compatibility: toTags(form.compatibility),
      merchant: { id: MERCHANT_ID },
      return_policy: { days: Math.max(0, Math.round(Number(form.returnDays) || 0)) },
      shipping: { estimated_days: Math.max(0, Math.round(Number(form.shippingDays) || 0)) },
      attributes: editingAttributes,
      purchase_constraints: editingPurchaseConstraints,
    };

    setSaving(true);
    setFormError("");
    try {
      const res = await authFetch(isCreate ? "/products" : `/products/${editingId}`, {
        method: isCreate ? "POST" : "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      if (!res.ok) throw new Error(await res.text());
      cancelForm();
      load(true);
    } catch (cause) {
      setFormError(cause instanceof Error ? cause.message : "Could not save the product.");
    } finally {
      setSaving(false);
    }
  }

  // remove surfaces ErrProductInUse's message as-is (backend/commerce/
  // catalog/handler.go already returns "product is referenced by an
  // existing cart or order and cannot be deleted" with 409, per
  // PLAN-05 §1's "surfaced as a clear message rather than a raw error"
  // ask) instead of a generic failure banner.
  async function remove(productId: string) {
    setConfirmingId(null);
    setDeletingId(productId);
    setDeleteError((prev) => ({ ...prev, [productId]: "" }));
    try {
      const res = await authFetch(`/products/${productId}`, { method: "DELETE" });
      if (!res.ok) throw new Error(await res.text());
      load(true);
    } catch (cause) {
      setDeleteError((prev) => ({
        ...prev,
        [productId]: cause instanceof Error ? cause.message : "Could not delete the product.",
      }));
    } finally {
      setDeletingId(null);
    }
  }

  return (
    <main className="px-5 py-7 sm:px-8 lg:px-10">
      <header className="border-b border-slate-200 pb-6">
        <h1 className="text-3xl font-semibold tracking-tight">Catalog</h1>
        <p className="mt-2 max-w-xl text-sm leading-6 text-slate-600">
          Add, edit, and remove products. Changes take effect immediately for both buyer checkout
          and the shopping agent.
        </p>
      </header>

      {error && (
        <p role="alert" className="mt-6 rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800">
          {error}
        </p>
      )}

      <section className="mt-8">
        {editingId === null ? (
          <button
            onClick={startCreate}
            className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700"
          >
            Add product
          </button>
        ) : (
          <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
            <h2 className="text-sm font-semibold text-slate-900">
              {editingId === "" ? "New product" : `Edit ${editingId}`}
            </h2>
            <div className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
              {editingId === "" && (
                <label className="text-sm text-slate-700">
                  Product ID
                  <input
                    value={form.productId}
                    onChange={(e) => setForm({ ...form, productId: e.target.value })}
                    placeholder="e.g. airpods-pro-2"
                    className="mt-1 block w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
                  />
                </label>
              )}
              <label className="text-sm text-slate-700">
                Title
                <input
                  value={form.title}
                  onChange={(e) => setForm({ ...form, title: e.target.value })}
                  className="mt-1 block w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
                />
              </label>
              <label className="text-sm text-slate-700">
                Price (INR)
                <input
                  type="number"
                  min={0}
                  step="0.01"
                  value={form.priceRupees}
                  onChange={(e) => setForm({ ...form, priceRupees: e.target.value })}
                  className="mt-1 block w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
                />
              </label>
              <label className="text-sm text-slate-700">
                Availability
                <input
                  type="number"
                  min={0}
                  value={form.availability}
                  onChange={(e) => setForm({ ...form, availability: e.target.value })}
                  className="mt-1 block w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
                />
              </label>
              <label className="text-sm text-slate-700">
                Return policy (days)
                <input
                  type="number"
                  min={0}
                  value={form.returnDays}
                  onChange={(e) => setForm({ ...form, returnDays: e.target.value })}
                  className="mt-1 block w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
                />
              </label>
              <label className="text-sm text-slate-700">
                Shipping estimate (days)
                <input
                  type="number"
                  min={0}
                  value={form.shippingDays}
                  onChange={(e) => setForm({ ...form, shippingDays: e.target.value })}
                  className="mt-1 block w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
                />
              </label>
              <label className="text-sm text-slate-700 sm:col-span-2">
                Features (comma-separated)
                <input
                  value={form.features}
                  onChange={(e) => setForm({ ...form, features: e.target.value })}
                  placeholder="active_noise_cancellation, battery_life"
                  className="mt-1 block w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
                />
              </label>
              <label className="text-sm text-slate-700 sm:col-span-2">
                Use cases (comma-separated)
                <input
                  value={form.useCases}
                  onChange={(e) => setForm({ ...form, useCases: e.target.value })}
                  placeholder="earbuds, travel"
                  className="mt-1 block w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
                />
              </label>
              <label className="text-sm text-slate-700 sm:col-span-2">
                Compatibility (comma-separated)
                <input
                  value={form.compatibility}
                  onChange={(e) => setForm({ ...form, compatibility: e.target.value })}
                  placeholder="ios, macos"
                  className="mt-1 block w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
                />
              </label>
            </div>

            {formError && (
              <p role="alert" className="mt-4 rounded-xl border border-rose-200 bg-rose-50 p-3 text-sm text-rose-800">
                {formError}
              </p>
            )}

            <div className="mt-5 flex gap-2">
              <button
                onClick={save}
                disabled={saving}
                className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700 disabled:opacity-50"
              >
                {saving ? "Saving…" : editingId === "" ? "Create product" : "Save changes"}
              </button>
              <button
                onClick={cancelForm}
                disabled={saving}
                className="rounded-lg border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-100 disabled:opacity-50"
              >
                Cancel
              </button>
            </div>
          </div>
        )}
      </section>

      <section className="mt-8">
        {loading ? (
          <div className="space-y-4">
            <div className="h-20 animate-pulse rounded-2xl bg-slate-100" />
            <div className="h-20 animate-pulse rounded-2xl bg-slate-100" />
          </div>
        ) : products.length === 0 ? (
          <div className="rounded-2xl border border-slate-200 bg-white p-8 text-center shadow-sm">
            <p className="text-sm font-medium text-slate-700">No products yet</p>
            <p className="mt-2 text-sm text-slate-500">Add one above to get started.</p>
          </div>
        ) : (
          <ul className="space-y-3">
            {products.map((p) => (
              <li key={p.product_id} className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
                <div className="flex flex-wrap items-start justify-between gap-4">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-mono text-xs text-slate-400">{p.product_id}</span>
                      {p.availability <= 0 && (
                        <span className="rounded-full bg-rose-100 px-2.5 py-1 text-xs font-semibold text-rose-800">
                          Out of stock
                        </span>
                      )}
                    </div>
                    <p className="mt-1 text-lg font-semibold text-slate-900">{p.title}</p>
                    <p className="mt-1 text-sm text-slate-600">
                      {formatINR(p.price.amount)} · {p.availability} in stock
                      {p.review_count > 0 && (
                        <>
                          {" "}
                          · <span className="text-amber-600">★ {p.average_rating.toFixed(1)}</span>{" "}
                          ({p.review_count})
                        </>
                      )}
                    </p>
                    {deleteError[p.product_id] && (
                      <p className="mt-2 text-xs text-rose-600">{deleteError[p.product_id]}</p>
                    )}
                  </div>
                  <div className="flex shrink-0 gap-2">
                    <button
                      onClick={() => setExpandedId(expandedId === p.product_id ? null : p.product_id)}
                      className="rounded-lg border border-slate-300 px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-100"
                    >
                      Variants ({p.variants?.length ?? 0})
                    </button>
                    <button
                      onClick={() => startEdit(p)}
                      className="rounded-lg border border-slate-300 px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-100"
                    >
                      Edit
                    </button>
                    {confirmingId === p.product_id ? (
                      <>
                        <button
                          onClick={() => remove(p.product_id)}
                          disabled={deletingId === p.product_id}
                          className="rounded-lg bg-rose-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-rose-700 disabled:opacity-50"
                        >
                          {deletingId === p.product_id ? "…" : "Confirm delete"}
                        </button>
                        <button
                          onClick={() => setConfirmingId(null)}
                          className="rounded-lg border border-slate-300 px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-100"
                        >
                          Cancel
                        </button>
                      </>
                    ) : (
                      <button
                        onClick={() => setConfirmingId(p.product_id)}
                        className="rounded-lg border border-rose-200 px-3 py-1.5 text-xs font-medium text-rose-700 hover:bg-rose-50"
                      >
                        Delete
                      </button>
                    )}
                  </div>
                </div>
                {expandedId === p.product_id && (
                  <VariantEditor
                    productId={p.product_id}
                    variants={p.variants ?? []}
                    onChanged={() => load(true)}
                  />
                )}
              </li>
            ))}
          </ul>
        )}
      </section>
    </main>
  );
}
