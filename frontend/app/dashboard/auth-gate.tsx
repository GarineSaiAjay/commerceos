"use client";

import { useEffect, useState } from "react";
import { isAuthenticated, login } from "../../lib/auth";

// AuthGate is the merchant dashboard's login wall (P0.3). Everything
// under /dashboard is merchant-only back office -- live metrics, the
// approval queue, agent runs, safety/red-team controls -- and none of it
// should be reachable without a real operator session. Buyer checkout
// (/checkout, /) is unaffected and stays guest.
export default function AuthGate({ children }: { children: React.ReactNode }) {
  const [authed, setAuthed] = useState<boolean | null>(null);
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    // Reading localStorage has to happen in an effect (not during render)
    // to avoid a server/client hydration mismatch -- there is no external
    // subscription to attach here, just a one-time read of client-only
    // state on mount.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setAuthed(isAuthenticated());
  }, []);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setLoading(true);
    setError("");
    try {
      await login(email, password);
      setAuthed(true);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Login failed");
    } finally {
      setLoading(false);
    }
  }

  if (authed === null) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center text-sm text-slate-500">
        Loading…
      </div>
    );
  }

  if (!authed) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center px-4 py-10">
        <form onSubmit={handleSubmit} className="w-full max-w-sm rounded-2xl border border-slate-200 bg-white p-6 shadow-sm">
          <h1 className="text-lg font-semibold text-slate-950">Operator sign in</h1>
          <p className="mt-1 text-sm leading-6 text-slate-500">
            Merchant back office — live metrics, approvals, agent runs, and safety controls. Buyer checkout does not need this.
          </p>
          <label className="mt-5 block">
            <span className="text-sm font-medium text-slate-600">Email</span>
            <input
              type="email"
              required
              autoComplete="username"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              className="mt-1 w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
            />
          </label>
          <label className="mt-3 block">
            <span className="text-sm font-medium text-slate-600">Password</span>
            <input
              type="password"
              required
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className="mt-1 w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
            />
          </label>
          {error && (
            <p role="alert" className="mt-3 text-sm text-rose-700">
              {error}
            </p>
          )}
          <button
            type="submit"
            disabled={loading}
            className="mt-5 w-full rounded-lg bg-slate-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-slate-700 disabled:cursor-wait disabled:opacity-60"
          >
            {loading ? "Signing in…" : "Sign in"}
          </button>
        </form>
      </div>
    );
  }

  return <>{children}</>;
}
