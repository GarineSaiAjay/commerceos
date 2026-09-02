"use client";

import { useCallback, useEffect, useState } from "react";
import { authFetch, getOperatorEmail } from "../../../lib/auth";

// item 40 (PLAN-05-SELLER-DASHBOARD.md §7, PLAN-06's phasing table:
// "Multi-operator invite flow"): lets the signed-in operator invite a
// teammate (POST /auth/invites), see pending/accepted invites
// (GET /auth/invites), revoke a still-pending one
// (DELETE /auth/invites/{id}), see the current team
// (GET /auth/operators), and remove a teammate
// (DELETE /auth/operators/{id}) -- backend/auth/invite.go. This project
// has no outbound email delivery, so unlike a production invite flow
// the link is shown here as a copyable URL for the inviting operator to
// share directly, rather than sent automatically.

type Invite = {
  id: string;
  email: string;
  invited_by: string;
  expires_at: string;
  accepted_at: string | null;
  status: "pending" | "accepted" | "expired";
};

type OperatorSummary = {
  id: string;
  email: string;
};

export default function TeamSettings() {
  const [operators, setOperators] = useState<OperatorSummary[] | null>(null);
  const [invites, setInvites] = useState<Invite[] | null>(null);
  const [loadError, setLoadError] = useState("");

  const [email, setEmail] = useState("");
  const [inviting, setInviting] = useState(false);
  const [inviteError, setInviteError] = useState("");
  const [newInviteLink, setNewInviteLink] = useState<{ email: string; url: string } | null>(null);

  const [actionError, setActionError] = useState("");

  // getOperatorEmail() reads localStorage; this component only ever
  // renders client-side under DashboardLayout -> AuthGate, which has
  // already resolved authentication before TeamSettings can mount, so
  // there's no hydration-mismatch risk here the way there is in
  // AuthGate itself.
  const selfEmail = getOperatorEmail();

  const load = useCallback(() => {
    setLoadError("");
    Promise.all([
      authFetch("/auth/operators", { cache: "no-store" }).then((r) => {
        if (!r.ok) throw new Error("Could not load the team list.");
        return r.json() as Promise<OperatorSummary[]>;
      }),
      authFetch("/auth/invites", { cache: "no-store" }).then((r) => {
        if (!r.ok) throw new Error("Could not load invites.");
        return r.json() as Promise<Invite[]>;
      }),
    ])
      .then(([ops, invs]) => {
        setOperators(ops ?? []);
        setInvites(invs ?? []);
      })
      .catch((cause) => setLoadError(cause instanceof Error ? cause.message : "Could not load the team."));
  }, []);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    load();
  }, [load]);

  async function sendInvite(e: React.FormEvent) {
    e.preventDefault();
    setInviting(true);
    setInviteError("");
    setNewInviteLink(null);
    try {
      const res = await authFetch("/auth/invites", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email }),
      });
      if (!res.ok) throw new Error(await res.text());
      const data = (await res.json()) as { invite_id: string; email: string; token: string; expires_at: string };
      const url = `${window.location.origin}/accept-invite?token=${encodeURIComponent(data.token)}`;
      setNewInviteLink({ email: data.email, url });
      setEmail("");
      load();
    } catch (cause) {
      setInviteError(cause instanceof Error ? cause.message : "Could not create the invite.");
    } finally {
      setInviting(false);
    }
  }

  async function revokeInvite(id: string) {
    setActionError("");
    try {
      const res = await authFetch(`/auth/invites/${id}`, { method: "DELETE" });
      if (!res.ok && res.status !== 404) throw new Error(await res.text());
      load();
    } catch (cause) {
      setActionError(cause instanceof Error ? cause.message : "Could not revoke the invite.");
    }
  }

  async function removeOperator(id: string) {
    setActionError("");
    try {
      const res = await authFetch(`/auth/operators/${id}`, { method: "DELETE" });
      if (!res.ok) throw new Error(await res.text());
      load();
    } catch (cause) {
      setActionError(cause instanceof Error ? cause.message : "Could not remove that operator.");
    }
  }

  const pendingInvites = (invites ?? []).filter((inv) => inv.status === "pending");

  return (
    <section className="mt-6 rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
      <h2 className="text-sm font-semibold text-slate-900">Team</h2>
      <p className="mt-1 text-sm text-slate-500">
        Invite another operator to sign in to this dashboard with their own account. There is no
        email delivery in this environment -- copy the invite link below and send it to them
        yourself.
      </p>

      {loadError && (
        <div role="alert" className="mt-4 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-950">
          <span>{loadError}</span>
          <button onClick={load} className="font-semibold underline underline-offset-2">Try again</button>
        </div>
      )}

      <form onSubmit={sendInvite} className="mt-4 flex flex-wrap items-end gap-3">
        <label className="text-sm text-slate-700">
          Teammate&apos;s email
          <input
            type="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder="teammate@example.com"
            className="mt-1 block w-64 rounded-lg border border-slate-300 px-3 py-2 text-sm"
          />
        </label>
        <button
          type="submit"
          disabled={inviting}
          className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700 disabled:opacity-50"
        >
          {inviting ? "Sending…" : "Send invite"}
        </button>
      </form>
      {inviteError && (
        <p role="alert" className="mt-3 rounded-xl border border-rose-200 bg-rose-50 p-3 text-sm text-rose-800">
          {inviteError}
        </p>
      )}
      {newInviteLink && (
        <div className="mt-3 rounded-xl border border-emerald-200 bg-emerald-50 p-3 text-sm text-emerald-900">
          <p>
            Invite created for <span className="font-medium">{newInviteLink.email}</span>. Share
            this link with them:
          </p>
          <code className="mt-1 block break-all rounded-lg bg-white px-2 py-1.5 text-xs text-slate-700">
            {newInviteLink.url}
          </code>
        </div>
      )}

      {actionError && (
        <p role="alert" className="mt-4 rounded-xl border border-rose-200 bg-rose-50 p-3 text-sm text-rose-800">
          {actionError}
        </p>
      )}

      <h3 className="mt-6 text-xs font-semibold uppercase tracking-wide text-slate-500">Current operators</h3>
      <ul className="mt-2 divide-y divide-slate-100">
        {(operators ?? []).map((op) => (
          <li key={op.id} className="flex items-center justify-between gap-3 py-2 text-sm">
            <span className="text-slate-800">
              {op.email}
              {op.email === selfEmail && <span className="ml-2 text-xs text-slate-400">(you)</span>}
            </span>
            {op.email !== selfEmail && (operators ?? []).length > 1 && (
              <button
                onClick={() => removeOperator(op.id)}
                className="text-xs font-medium text-rose-700 hover:underline"
              >
                Remove
              </button>
            )}
          </li>
        ))}
        {operators !== null && operators.length === 0 && (
          <li className="py-2 text-sm text-slate-500">No operators found.</li>
        )}
      </ul>

      {pendingInvites.length > 0 && (
        <>
          <h3 className="mt-6 text-xs font-semibold uppercase tracking-wide text-slate-500">Pending invites</h3>
          <ul className="mt-2 divide-y divide-slate-100">
            {pendingInvites.map((inv) => (
              <li key={inv.id} className="flex items-center justify-between gap-3 py-2 text-sm">
                <span className="text-slate-800">
                  {inv.email}
                  <span className="ml-2 text-xs text-slate-400">
                    expires {new Date(inv.expires_at).toLocaleDateString()}
                  </span>
                </span>
                <button
                  onClick={() => revokeInvite(inv.id)}
                  className="text-xs font-medium text-rose-700 hover:underline"
                >
                  Revoke
                </button>
              </li>
            ))}
          </ul>
        </>
      )}
    </section>
  );
}
