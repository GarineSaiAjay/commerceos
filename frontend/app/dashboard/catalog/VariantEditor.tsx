"use client";

import { useState } from "react";
import { formatINR } from "../../../lib/format";
import { authFetch } from "../../../lib/auth";

// ProductVariant mirrors backend/commerce/catalog/product.go's
// ProductVariant wire shape exactly (variant_id/product_id/sku/price/
// availability/attributes JSON tags).
export type ProductVariant = {
  variant_id: string;
  product_id: string;
  sku: string;
  price: { amount: number; currency: string };
  availability: number;
  attributes?: Record<string, unknown>;
};

// slugify turns an arbitrary SKU into a URL/id-safe fragment for the
// auto-generated variant id (`${productId}-${slugify(sku)}`) --
// lowercase, non-alphanumerics collapsed to single hyphens, trimmed of
// leading/trailing hyphens.
function slugify(value: string): string {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

function variantToRow(v: ProductVariant) {
  return {
    sku: v.sku,
    priceRupees: (v.price.amount / 100).toString(),
    availability: String(v.availability),
  };
}

// VariantRow renders one existing variant as an inline-editable row --
// per-row local state (keyed by variant_id via the parent's <ul> key)
// so editing one row's inputs never touches sibling rows' state, and
// React's key identity naturally handles add/remove.
function VariantRow({
  variant,
  isOnlyVariant,
  onChanged,
}: {
  variant: ProductVariant;
  isOnlyVariant: boolean;
  onChanged: () => void;
}) {
  const [row, setRow] = useState(() => variantToRow(variant));
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState("");

  async function save() {
    const priceRupees = Number(row.priceRupees);
    if (!row.sku.trim()) {
      setSaveError("SKU is required.");
      return;
    }
    if (!Number.isFinite(priceRupees) || priceRupees < 0) {
      setSaveError("Price must be a non-negative number.");
      return;
    }

    setSaving(true);
    setSaveError("");
    try {
      // Wire shape matches ProductVariant's JSON tags -- UpdateVariant
      // decodes the body directly into that struct (backend/commerce/
      // catalog/handler.go). product_id in the body is ignored: the
      // URL path is authoritative there, a variant can't be moved to a
      // different product through this endpoint.
      const res = await authFetch(`/variants/${variant.variant_id}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          sku: row.sku.trim(),
          price: { amount: Math.round(priceRupees * 100), currency: variant.price.currency },
          availability: Math.max(0, Math.round(Number(row.availability) || 0)),
          attributes: variant.attributes ?? {},
        }),
      });
      if (!res.ok) throw new Error(await res.text());
      onChanged();
    } catch (cause) {
      setSaveError(cause instanceof Error ? cause.message : "Could not save this variant.");
    } finally {
      setSaving(false);
    }
  }

  // remove surfaces ErrVariantInUse's message as-is (backend/commerce/
  // catalog/handler.go returns 409 "variant is referenced by an
  // existing cart or order and cannot be deleted") instead of a
  // generic failure banner, same convention as this page's own product
  // delete.
  async function remove() {
    setConfirmingDelete(false);
    setDeleting(true);
    setDeleteError("");
    try {
      const res = await authFetch(`/variants/${variant.variant_id}`, { method: "DELETE" });
      if (!res.ok) throw new Error(await res.text());
      onChanged();
    } catch (cause) {
      setDeleteError(cause instanceof Error ? cause.message : "Could not delete this variant.");
    } finally {
      setDeleting(false);
    }
  }

  return (
    <li className="rounded-xl border border-slate-200 bg-slate-50 p-3">
      <div className="flex flex-wrap items-end gap-3">
        <div className="min-w-0">
          <span className="block font-mono text-[11px] text-slate-400">{variant.variant_id}</span>
          <label className="mt-1 block text-xs text-slate-600">
            SKU
            <input
              value={row.sku}
              onChange={(e) => setRow({ ...row, sku: e.target.value })}
              className="mt-1 block w-32 rounded-lg border border-slate-300 px-2 py-1 text-sm"
            />
          </label>
        </div>
        <label className="text-xs text-slate-600">
          Price (INR)
          <input
            type="number"
            min={0}
            step="0.01"
            value={row.priceRupees}
            onChange={(e) => setRow({ ...row, priceRupees: e.target.value })}
            className="mt-1 block w-24 rounded-lg border border-slate-300 px-2 py-1 text-sm"
          />
        </label>
        <label className="text-xs text-slate-600">
          Availability
          <input
            type="number"
            min={0}
            value={row.availability}
            onChange={(e) => setRow({ ...row, availability: e.target.value })}
            className="mt-1 block w-20 rounded-lg border border-slate-300 px-2 py-1 text-sm"
          />
        </label>
        <div className="flex gap-2 pb-0.5">
          <button
            onClick={save}
            disabled={saving}
            className="rounded-lg bg-slate-900 px-3 py-1.5 text-xs font-medium text-white hover:bg-slate-700 disabled:opacity-50"
          >
            {saving ? "Saving…" : "Save"}
          </button>
          {confirmingDelete ? (
            <>
              <button
                onClick={remove}
                disabled={deleting}
                className="rounded-lg bg-rose-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-rose-700 disabled:opacity-50"
              >
                {deleting ? "…" : "Confirm"}
              </button>
              <button
                onClick={() => setConfirmingDelete(false)}
                className="rounded-lg border border-slate-300 px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-100"
              >
                Cancel
              </button>
            </>
          ) : (
            <button
              onClick={() => setConfirmingDelete(true)}
              className="rounded-lg border border-rose-200 px-3 py-1.5 text-xs font-medium text-rose-700 hover:bg-rose-50"
            >
              Delete
            </button>
          )}
        </div>
      </div>
      {/* Deleting a product's last variant breaks "Add to cart" for it
          (checkout.tsx's addToCart and the MCP add_item tool both
          assume every product has >= 1 variant) -- the backend
          deliberately doesn't block this (see DeleteVariant's doc
          comment), so this inline warning is the actual mitigation. */}
      {isOnlyVariant && !confirmingDelete && (
        <p className="mt-2 text-xs text-amber-700">
          This is the product&apos;s only variant. Deleting it will break &quot;Add to cart&quot;
          until a new one is added.
        </p>
      )}
      {saveError && <p className="mt-2 text-xs text-rose-600">{saveError}</p>}
      {deleteError && <p className="mt-2 text-xs text-rose-600">{deleteError}</p>}
      {!saveError && !deleteError && (
        <p className="mt-2 text-xs text-slate-400">
          {formatINR(variant.price.amount)} · {variant.availability} in stock
        </p>
      )}
    </li>
  );
}

// AddVariantForm posts a new variant to an existing product. The
// variant id is auto-generated from the SKU (`${productId}-${slugify
// (sku)}`, editable before submit) rather than asked for separately --
// this mirrors how every seeded variant is already named (e.g.
// "airpods-pro-2-default").
function AddVariantForm({ productId, onChanged }: { productId: string; onChanged: () => void }) {
  const [open, setOpen] = useState(false);
  const [sku, setSku] = useState("");
  const [variantId, setVariantId] = useState("");
  const [idTouched, setIdTouched] = useState(false);
  const [priceRupees, setPriceRupees] = useState("");
  const [availability, setAvailability] = useState("0");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  function onSkuChange(value: string) {
    setSku(value);
    if (!idTouched) setVariantId(`${productId}-${slugify(value)}`);
  }

  async function submit() {
    const priceValue = Number(priceRupees);
    if (!sku.trim()) {
      setError("SKU is required.");
      return;
    }
    if (!variantId.trim()) {
      setError("Variant ID is required.");
      return;
    }
    if (!Number.isFinite(priceValue) || priceValue < 0) {
      setError("Price must be a non-negative number.");
      return;
    }

    setSaving(true);
    setError("");
    try {
      const res = await authFetch(`/products/${productId}/variants`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          variant_id: variantId.trim(),
          sku: sku.trim(),
          price: { amount: Math.round(priceValue * 100), currency: "INR" },
          availability: Math.max(0, Math.round(Number(availability) || 0)),
        }),
      });
      if (!res.ok) throw new Error(await res.text());
      setOpen(false);
      setSku("");
      setVariantId("");
      setIdTouched(false);
      setPriceRupees("");
      setAvailability("0");
      onChanged();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not add this variant.");
    } finally {
      setSaving(false);
    }
  }

  if (!open) {
    return (
      <button
        onClick={() => setOpen(true)}
        className="rounded-lg border border-slate-300 px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-100"
      >
        Add variant
      </button>
    );
  }

  return (
    <div className="rounded-xl border border-slate-200 bg-white p-3">
      <div className="flex flex-wrap items-end gap-3">
        <label className="text-xs text-slate-600">
          SKU
          <input
            value={sku}
            onChange={(e) => onSkuChange(e.target.value)}
            className="mt-1 block w-32 rounded-lg border border-slate-300 px-2 py-1 text-sm"
            autoFocus
          />
        </label>
        <label className="text-xs text-slate-600">
          Variant ID
          <input
            value={variantId}
            onChange={(e) => {
              setIdTouched(true);
              setVariantId(e.target.value);
            }}
            className="mt-1 block w-40 rounded-lg border border-slate-300 px-2 py-1 text-sm"
          />
        </label>
        <label className="text-xs text-slate-600">
          Price (INR)
          <input
            type="number"
            min={0}
            step="0.01"
            value={priceRupees}
            onChange={(e) => setPriceRupees(e.target.value)}
            className="mt-1 block w-24 rounded-lg border border-slate-300 px-2 py-1 text-sm"
          />
        </label>
        <label className="text-xs text-slate-600">
          Availability
          <input
            type="number"
            min={0}
            value={availability}
            onChange={(e) => setAvailability(e.target.value)}
            className="mt-1 block w-20 rounded-lg border border-slate-300 px-2 py-1 text-sm"
          />
        </label>
        <div className="flex gap-2 pb-0.5">
          <button
            onClick={submit}
            disabled={saving}
            className="rounded-lg bg-slate-900 px-3 py-1.5 text-xs font-medium text-white hover:bg-slate-700 disabled:opacity-50"
          >
            {saving ? "Adding…" : "Add"}
          </button>
          <button
            onClick={() => setOpen(false)}
            disabled={saving}
            className="rounded-lg border border-slate-300 px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-100 disabled:opacity-50"
          >
            Cancel
          </button>
        </div>
      </div>
      {error && <p className="mt-2 text-xs text-rose-600">{error}</p>}
    </div>
  );
}

// VariantEditor (PLAN-02-CATALOG-AND-COMMERCE.md §5.2 / PLAN-05-
// SELLER-DASHBOARD.md §1's "variant sub-editor" -- item 10 shipped real
// variants with no way to add/edit/delete them from the dashboard
// until this) renders one product's variants inline with per-row
// save/delete and an add-variant form, backed by the /products/{id}/
// variants and /variants/{id} routes. onChanged is called after every
// successful mutation so the parent CatalogPage can refetch (it
// re-reads variants from GET /products, the same embedded field this
// component is seeded from -- there's no separate variant list state
// to keep in sync here beyond React's own key-based reconciliation).
export function VariantEditor({
  productId,
  variants,
  onChanged,
}: {
  productId: string;
  variants: ProductVariant[];
  onChanged: () => void;
}) {
  return (
    <div className="mt-3 rounded-xl border border-slate-200 bg-white p-3">
      <p className="text-xs font-semibold uppercase tracking-wide text-slate-500">
        Variants ({variants.length})
      </p>
      {variants.length === 0 ? (
        <p className="mt-2 text-xs text-slate-500">
          No variants yet -- this product can&apos;t be added to a cart until it has at least one.
        </p>
      ) : (
        <ul className="mt-2 space-y-2">
          {variants.map((v) => (
            <VariantRow
              key={v.variant_id}
              variant={v}
              isOnlyVariant={variants.length === 1}
              onChanged={onChanged}
            />
          ))}
        </ul>
      )}
      <div className="mt-3">
        <AddVariantForm productId={productId} onChanged={onChanged} />
      </div>
    </div>
  );
}
