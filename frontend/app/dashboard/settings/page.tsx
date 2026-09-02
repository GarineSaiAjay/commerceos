"use client";

import { useCallback, useEffect, useState } from "react";
import { authFetch } from "../../../lib/auth";
import TeamSettings from "./team";

// Item 25 (ROADMAP-PRIORITIZED.md P2, PLAN-05-SELLER-DASHBOARD.md §4):
// a window into the policy engine's live configuration -- ceiling,
// budget tolerance, allowed currencies/merchants -- backed by
// GET/PATCH /dashboard/settings/policy (backend/policy/handler.go).
// Still validated deterministically server-side by policy.Engine
// exactly as before: this page changes nothing about HOW policy
// decides, only WHAT the numbers/lists it decides against are.

type PolicySettings = {
  ceiling: number; // paise
  budget_tolerance: number; // fraction, e.g. 0.10 = +10%
  allowed_currencies: string[];
  allowed_merchants: string[];
};

function splitList(value: string): string[] {
  return value
    .split(",")
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
}

export default function SettingsPage() {
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");
  const [savedAt, setSavedAt] = useState<number | null>(null);

  // Form fields, kept as their own state (not derived live from a
  // PolicySettings object) so an operator's in-progress edits survive
  // re-renders and aren't clobbered until they actually save.
  const [ceilingRupees, setCeilingRupees] = useState("0");
  const [budgetTolerancePercent, setBudgetTolerancePercent] = useState("0");
  const [allowedCurrencies, setAllowedCurrencies] = useState("");
  const [allowedMerchants, setAllowedMerchants] = useState("");

  const applySettings = useCallback((s: PolicySettings) => {
    setCeilingRupees(String(s.ceiling / 100));
    setBudgetTolerancePercent(String(s.budget_tolerance * 100));
    setAllowedCurrencies(s.allowed_currencies.join(", "));
    setAllowedMerchants(s.allowed_merchants.join(", "));
  }, []);

  const load = useCallback(() => {
    setLoading(true);
    setLoadError("");
    authFetch("/dashboard/settings/policy", { cache: "no-store" })
      .then((r) => {
        if (!r.ok) throw new Error("Could not load policy settings.");
        return r.json() as Promise<PolicySettings>;
      })
      .then((data) => applySettings(data))
      .catch((cause) => setLoadError(cause instanceof Error ? cause.message : "Could not load policy settings."))
      .finally(() => setLoading(false));
  }, [applySettings]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    load();
  }, [load]);

  async function save() {
    setSaving(true);
    setSaveError("");
    setSavedAt(null);
    try {
      const ceiling = Math.round(Number(ceilingRupees) * 100);
      const budgetTolerance = Number(budgetTolerancePercent) / 100;
      if (!Number.isFinite(ceiling) || ceiling <= 0) {
        throw new Error("Ceiling must be a positive amount.");
      }
      if (!Number.isFinite(budgetTolerance) || budgetTolerance < 0) {
        throw new Error("Budget tolerance cannot be negative.");
      }
      const currencies = splitList(allowedCurrencies);
      const merchants = splitList(allowedMerchants);
      if (currencies.length === 0) {
        throw new Error("Allowed currencies cannot be empty.");
      }
      if (merchants.length === 0) {
        throw new Error("Allowed merchants cannot be empty.");
      }

      const res = await authFetch("/dashboard/settings/policy", {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          ceiling,
          budget_tolerance: budgetTolerance,
          allowed_currencies: currencies,
          allowed_merchants: merchants,
        }),
      });
      if (!res.ok) throw new Error(await res.text());
      const updated = (await res.json()) as PolicySettings;
      applySettings(updated);
      setSavedAt(Date.now());
    } catch (cause) {
      setSaveError(cause instanceof Error ? cause.message : "Could not save policy settings.");
    } finally {
      setSaving(false);
    }
  }

  return (
    <main className="px-5 py-7 sm:px-8 lg:px-10">
      <header className="border-b border-slate-200 pb-6">
        <h1 className="text-3xl font-semibold tracking-tight">Settings</h1>
        <p className="mt-2 max-w-xl text-sm leading-6 text-slate-600">
          The policy engine&apos;s live configuration. Every checkout is still evaluated
          deterministically by the same policy engine as always -- this page is a window into that
          configuration, not a separate authority. Changes take effect immediately for every proposal
          evaluated after you save.
        </p>
      </header>

      {loadError && (
        <div role="alert" className="mt-6 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-950">
          <span>{loadError}</span>
          <button onClick={load} className="font-semibold underline underline-offset-2">Try again</button>
        </div>
      )}

      <section className="mt-8 rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
        <h2 className="text-sm font-semibold text-slate-900">Policy configuration</h2>
        <p className="mt-1 text-sm text-slate-500">
          Ceiling and budget tolerance are enforced on every proposal (policy.CheckAmountCeiling,
          policy.CheckBudgetTolerance). Allowed currencies/merchants gate which currency and which
          merchant a proposal can be for.
        </p>

        {loading ? (
          <div className="mt-4 grid gap-4 sm:grid-cols-2">
            {[0, 1, 2, 3].map((i) => (
              <div key={i} className="h-16 animate-pulse rounded-lg bg-slate-100" />
            ))}
          </div>
        ) : (
          <div className="mt-4 grid gap-4 sm:grid-cols-2">
            <label className="text-sm text-slate-700">
              Amount ceiling (₹)
              <input
                type="number"
                min={0}
                step="0.01"
                value={ceilingRupees}
                onChange={(e) => setCeilingRupees(e.target.value)}
                className="mt-1 block w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
              />
              <span className="mt-1 block text-xs text-slate-500">
                The maximum a single proposal can be for. Stored as paise; policy.CheckAmountCeiling.
              </span>
            </label>

            <label className="text-sm text-slate-700">
              Budget tolerance (%)
              <input
                type="number"
                min={0}
                step="1"
                value={budgetTolerancePercent}
                onChange={(e) => setBudgetTolerancePercent(e.target.value)}
                className="mt-1 block w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
              />
              <span className="mt-1 block text-xs text-slate-500">
                Slack above a mandate&apos;s maximum_amount still allowed. policy.CheckBudgetTolerance.
              </span>
            </label>

            <label className="text-sm text-slate-700">
              Allowed currencies
              <input
                type="text"
                value={allowedCurrencies}
                onChange={(e) => setAllowedCurrencies(e.target.value)}
                placeholder="INR"
                className="mt-1 block w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
              />
              <span className="mt-1 block text-xs text-slate-500">
                Comma-separated (e.g. INR, USD). policy.CheckCurrencyAllowed.
              </span>
            </label>

            <label className="text-sm text-slate-700">
              Allowed merchants
              <input
                type="text"
                value={allowedMerchants}
                onChange={(e) => setAllowedMerchants(e.target.value)}
                placeholder="merchant_001"
                className="mt-1 block w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
              />
              <span className="mt-1 block text-xs text-slate-500">
                Comma-separated. policy.CheckMerchantAllowlisted.
              </span>
            </label>
          </div>
        )}

        {saveError && (
          <p role="alert" className="mt-4 rounded-xl border border-rose-200 bg-rose-50 p-3 text-sm text-rose-800">
            {saveError}
          </p>
        )}
        {savedAt && !saveError && (
          <p className="mt-4 rounded-xl border border-emerald-200 bg-emerald-50 p-3 text-sm text-emerald-800">
            Saved. The policy engine is enforcing these values now.
          </p>
        )}

        <button
          onClick={save}
          disabled={loading || saving}
          className="mt-5 rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700 disabled:opacity-50"
        >
          {saving ? "Saving…" : "Save changes"}
        </button>
      </section>

      <TeamSettings />

      <section className="mt-6 rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
        <h2 className="text-sm font-semibold text-slate-900">Not editable here</h2>
        <p className="mt-2 text-sm leading-6 text-slate-600">
          The permitted-products list is not shown above: in production it is superseded by a live
          catalog check (every real product is automatically purchasable, including ones added from{" "}
          <span className="font-medium">Catalog</span>), so a control here would not actually change
          what is enforced.
        </p>
        <p className="mt-2 text-sm leading-6 text-slate-600">
          Each buyer&apos;s mandate also carries its own allowed-categories list and confirmation
          threshold, set per checkout rather than here -- those describe what that one buyer consented
          to for that one cart, not merchant-wide policy, so they belong to the checkout flow, not this
          page.
        </p>
      </section>
    </main>
  );
}
