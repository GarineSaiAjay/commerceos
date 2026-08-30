"use client";

import { useCallback, useEffect, useState } from "react";
import { formatINR } from "../../../lib/format";
import { authFetch } from "../../../lib/auth";

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
};

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
// Deliberately no variant sub-editor here: PLAN-05 scopes that as a
// separate 1.5-day P1 line once PLAN-02 §1's real variants exist
// (ROADMAP item 10, not yet shipped), so it stays out of this page's
// 2-day cut.
export default function CatalogPage() {
  const [products, setProducts] = useState<Product[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  // editingId: null = form closed, "" = creating a new product, any
  // other string = editing that product_id.
  const [editingId, setEditingId] = useState<string | null>(null);
  const [form, setForm] = useState<FormState>(EMPTY_FORM);
  const [saving, setSaving] = useState(false);
  const [formError, setFormError] = useState("");

  const [confirmingId, setConfirmingId] = useState<string | null>(null);
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const [deleteError, setDeleteError] = useState<Record<string, string>>({});

  const load = useCallback(() => {
    authFetch("/products", { cache: "no-store" })
      .then((r) => r.json())
      .then((data: Product[]) => setProducts(data ?? []))
      .catch(() => setError("Could not load the catalog."))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  function startCreate() {
    setEditingId("");
    setForm(EMPTY_FORM);
    setFormError("");
  }

  function startEdit(p: Product) {
    setEditingId(p.product_id);
    setForm(productToForm(p));
    setFormError("");
  }

  function cancelForm() {
    setEditingId(null);
    setForm(EMPTY_FORM);
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
      load();
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
      load();
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
              </li>
            ))}
          </ul>
        )}
      </section>
    </main>
  );
}
