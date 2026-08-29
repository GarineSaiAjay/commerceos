// Tiny shared API base for the dashboard. Requests go through
// authFetch (lib/auth.ts), which attaches the operator's bearer token;
// this file used to also export a bare getJSON helper, but every caller
// has since moved to authFetch and nothing imports getJSON anymore.
export const API_BASE = process.env.NEXT_PUBLIC_COMMERCE_URL ?? "http://localhost:8081";
