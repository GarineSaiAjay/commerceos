"use client";

import { useState } from "react";
import { authFetch } from "../../../lib/auth";

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
    </main>
  );
}
