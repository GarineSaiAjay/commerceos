"use client";

import { logout } from "../../lib/auth";

export default function SignOutButton() {
  return (
    <button
      onClick={async () => {
        await logout();
        window.location.reload();
      }}
      className="mt-4 w-full shrink-0 rounded-lg px-3 py-2 text-left text-sm font-medium text-slate-500 hover:bg-slate-100 hover:text-slate-950"
    >
      Sign out
    </button>
  );
}
