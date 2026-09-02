"use client";

// Public "finish creating your operator account" page for item 40's
// invite flow (backend/auth/invite.go). Reached via the link an
// existing operator copies out of Settings -> Team
// (frontend/app/dashboard/settings/team.tsx) and shares with a new
// teammate directly -- this project has no outbound email delivery, so
// unlike a production invite flow nothing sends this link on its own.
// Deliberately outside app/dashboard/, mirroring app/trust/page.tsx's
// reasoning: the person opening this link has no account and no
// session yet, so AuthGate (which assumes an existing operator) would
// only be in the way.
import { useEffect, useState } from "react";
import Link from "next/link";
import { acceptInvite } from "../../lib/auth";

export default function AcceptInvitePage() {
  const [token, setToken] = useState<string | null>(null);
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState<{ signedIn: boolean; email: string } | null>(null);

  useEffect(() => {
    // Reading the URL's query string has to happen client-side -- there
    // is no server-rendered value to hydrate against -- same reasoning
    // as AuthGate's own effect-only localStorage read.
    const params = new URLSearchParams(window.location.search);
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setToken(params.get("token") ?? "");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    if (!token) {
      setError("This invite link is missing its token.");
      return;
    }
    if (password !== confirmPassword) {
      setError("Passwords don't match.");
      return;
    }
    setSubmitting(true);
    try {
      const outcome = await acceptInvite(token, password);
      setResult(outcome);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not accept this invite.");
    } finally {
      setSubmitting(false);
    }
  }

  if (result) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center px-4 py-10">
        <div className="w-full max-w-sm rounded-2xl border border-slate-200 bg-white p-6 text-center shadow-sm">
          <h1 className="text-lg font-semibold text-slate-950">You&apos;re in</h1>
          <p className="mt-2 text-sm leading-6 text-slate-600">
            {result.signedIn
              ? `Your operator account (${result.email}) is set up and you're signed in.`
              : `Your operator account (${result.email}) is set up. Sign in with your new password to continue.`}
          </p>
          <Link
            href="/dashboard"
            className="mt-5 inline-block w-full rounded-lg bg-slate-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-slate-700"
          >
            Go to dashboard
          </Link>
        </div>
      </div>
    );
  }

  if (token === null) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center text-sm text-slate-500">
        Loading…
      </div>
    );
  }

  if (token === "") {
    return (
      <div className="flex min-h-[60vh] items-center justify-center px-4 py-10 text-center">
        <p className="text-sm text-slate-600">
          This link is missing an invite token. Ask whoever invited you for a fresh link from
          Settings → Team.
        </p>
      </div>
    );
  }

  return (
    <div className="flex min-h-[60vh] items-center justify-center px-4 py-10">
      <form onSubmit={handleSubmit} className="w-full max-w-sm rounded-2xl border border-slate-200 bg-white p-6 shadow-sm">
        <h1 className="text-lg font-semibold text-slate-950">Set up your operator account</h1>
        <p className="mt-1 text-sm leading-6 text-slate-500">
          Choose a password to finish joining this merchant&apos;s CommerceOS dashboard.
        </p>
        <label className="mt-5 block">
          <span className="text-sm font-medium text-slate-600">Password</span>
          <input
            type="password"
            required
            minLength={12}
            autoComplete="new-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="mt-1 w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
          />
          <span className="mt-1 block text-xs text-slate-500">At least 12 characters.</span>
        </label>
        <label className="mt-3 block">
          <span className="text-sm font-medium text-slate-600">Confirm password</span>
          <input
            type="password"
            required
            minLength={12}
            autoComplete="new-password"
            value={confirmPassword}
            onChange={(e) => setConfirmPassword(e.target.value)}
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
          disabled={submitting}
          className="mt-5 w-full rounded-lg bg-slate-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-slate-700 disabled:cursor-wait disabled:opacity-60"
        >
          {submitting ? "Setting up…" : "Create account"}
        </button>
      </form>
    </div>
  );
}
