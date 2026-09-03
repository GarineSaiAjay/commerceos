"use client";

import { useCallback, useEffect, useState } from "react";
import { authFetch } from "../../../lib/auth";
import { formatTime } from "../../../lib/format";

type ExperimentReport = {
  experiment_id: string;
  name: string;
  population: number;
  control_size: number;
  treatment_size: number;
  control_value: number;
  treatment_value: number;
  lift: number;
  ci_lower: number;
  ci_upper: number;
  source: string;
  // When this experiment name was last run -- distinct from the row's
  // creation time, which stays fixed so re-running doesn't reorder the
  // history list (backend/analytics/experiment.go's List doc comment).
  updated_at: string;
};

function fmt(n: number) {
  return new Intl.NumberFormat("en-IN", { maximumFractionDigits: 2 }).format(n);
}

export default function AnalyticsPage() {
  const [name, setName] = useState("ai_cross_sell");
  const [seed, setSeed] = useState("42");
  const [treatment, setTreatment] = useState("1.5");
  const [report, setReport] = useState<ExperimentReport | null>(null);
  const [error, setError] = useState("");
  const [running, setRunning] = useState(false);
  const [history, setHistory] = useState<ExperimentReport[]>([]);
  const [historyError, setHistoryError] = useState("");

  // GET /dashboard/experiments -- the persisted history the backend
  // already wrote to on every run (Run() upserts into `experiments`),
  // just never previously exposed. Same load-on-mount + reload-after-
  // action pattern the Safety page uses for its evaluation history.
  const loadHistory = useCallback(() => {
    authFetch("/dashboard/experiments", { cache: "no-store" })
      .then((r) => {
        if (!r.ok) throw new Error("history request failed");
        return r.json();
      })
      .then((d: ExperimentReport[]) => setHistory(d ?? []))
      .catch(() => setHistoryError("Could not load experiment history."));
  }, []);

  useEffect(() => {
    loadHistory();
  }, [loadHistory]);

  async function runExperiment() {
    setRunning(true);
    setError("");
    setReport(null);
    try {
      const response = await authFetch("/dashboard/experiment", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name, seed: Number(seed), treatment_multiplier: Number(treatment) }),
      });
      if (!response.ok) throw new Error("Experiment failed to run");
      setReport((await response.json()) as ExperimentReport);
      loadHistory();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Experiment failed to run");
    } finally {
      setRunning(false);
    }
  }

  return (
    <main className="px-5 py-7 sm:px-8 lg:px-10">
      <header className="border-b border-slate-200 pb-6">
        <h1 className="text-3xl font-semibold tracking-tight">Analytics & Experimentation</h1>
        <p className="mt-2 max-w-xl text-sm leading-6 text-slate-600">
          Simulated A/B experiments over the merchant simulator population. Results are labeled <strong>Simulated</strong> — never confused with live data.
        </p>
      </header>

      <section className="mt-8 rounded-2xl border border-slate-200 bg-white p-6 shadow-sm">
        <div className="grid gap-4 sm:grid-cols-[1fr_7rem_7rem_auto]">
          <label className="block">
            <span className="text-sm font-medium text-slate-600">Name</span>
            <input value={name} onChange={(e) => setName(e.target.value)} className="mt-1 w-full rounded-lg border border-slate-300 px-3 py-2 text-sm" />
          </label>
          <label className="block">
            <span className="text-sm font-medium text-slate-600">Seed</span>
            <input value={seed} onChange={(e) => setSeed(e.target.value)} className="mt-1 w-full rounded-lg border border-slate-300 px-3 py-2 text-sm" />
          </label>
          <label className="block">
            <span className="text-sm font-medium text-slate-600">Multiplier</span>
            <input value={treatment} onChange={(e) => setTreatment(e.target.value)} className="mt-1 w-full rounded-lg border border-slate-300 px-3 py-2 text-sm" />
          </label>
          <button onClick={runExperiment} disabled={running} className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700 disabled:cursor-wait disabled:opacity-60 sm:mt-6">
            {running ? "Running…" : "Run experiment"}
          </button>
        </div>
      </section>

      {error && <p role="alert" className="mt-6 rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800">{error}</p>}

      {report && (
        <section className="mt-6 rounded-2xl border border-amber-200 bg-amber-50 p-6 shadow-sm">
          <div className="flex items-center justify-between gap-4">
            <h2 className="text-lg font-semibold text-amber-950">Experiment: {report.name}</h2>
            <span className="rounded-full bg-amber-200 px-3 py-1 text-xs font-semibold text-amber-900">Simulated</span>
          </div>
          <dl className="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <div><dt className="text-xs font-medium uppercase tracking-wide text-amber-700">Population</dt><dd className="mt-1 text-xl font-semibold text-amber-950">{report.population.toLocaleString()} sessions</dd></div>
            <div><dt className="text-xs font-medium uppercase tracking-wide text-amber-700">Split</dt><dd className="mt-1 text-xl font-semibold text-amber-950">{report.control_size.toLocaleString()} / {report.treatment_size.toLocaleString()}</dd></div>
            <div><dt className="text-xs font-medium uppercase tracking-wide text-amber-700">Revenue / session</dt><dd className="mt-1 text-xl font-semibold text-amber-950">₹{fmt(report.control_value)} → ₹{fmt(report.treatment_value)}</dd></div>
            <div><dt className="text-xs font-medium uppercase tracking-wide text-amber-700">Lift</dt><dd className="mt-1 text-xl font-semibold text-amber-950">{fmt(report.lift * 100)}%</dd></div>
          </dl>
          <p className="mt-4 rounded-xl bg-white/70 p-4 text-sm text-amber-900">
            <span className="font-semibold">95% CI:</span> {fmt(report.ci_lower * 100)}% to {fmt(report.ci_upper * 100)}%. Real calculation from the split population, not an asserted percentage.
          </p>
        </section>
      )}

      <section className="mt-8">
        <h2 className="text-base font-semibold">Experiment history</h2>
        <p className="mt-1 text-sm text-slate-600">
          One row per experiment name -- re-running the same name updates its result rather than adding a new entry.
        </p>
        {historyError && <p role="alert" className="mt-4 rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800">{historyError}</p>}
        {history.length === 0 ? (
          <div className="mt-4 rounded-2xl border border-slate-200 bg-white p-8 text-center shadow-sm">
            <p className="text-sm font-medium text-slate-700">No experiments yet</p>
            <p className="mt-2 text-sm text-slate-500">Run one above to see it listed here.</p>
          </div>
        ) : (
          <ul className="mt-4 space-y-3">
            {history.map((h) => (
              <li key={h.experiment_id} className="rounded-xl border border-slate-200 bg-white p-4 text-sm shadow-sm">
                <div className="flex flex-wrap items-center gap-3">
                  <span className="font-semibold text-slate-900">{h.name}</span>
                  <span className="rounded-full bg-amber-100 px-2.5 py-0.5 text-xs font-semibold text-amber-800">{h.source}</span>
                  <span className="text-slate-600">{h.population.toLocaleString()} sessions</span>
                  <span className="text-slate-600">lift {fmt(h.lift * 100)}%</span>
                  <span className="text-slate-500">CI {fmt(h.ci_lower * 100)}% to {fmt(h.ci_upper * 100)}%</span>
                  <span className="text-xs text-slate-400">last run {formatTime(h.updated_at)}</span>
                </div>
              </li>
            ))}
          </ul>
        )}
      </section>
    </main>
  );
}
