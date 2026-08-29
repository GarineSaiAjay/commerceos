// Operator authentication for the merchant dashboard (P0.3). Buyer
// checkout stays guest -- this only gates the merchant's own back office:
// dashboard data, safety/red-team controls, and the LIST endpoints for
// approval requests and runs. See files/AUTH.md.
import { API_BASE } from "./api";

const TOKEN_KEY = "commerceos_operator_token";
const EXPIRES_KEY = "commerceos_operator_token_expires_at";
const EMAIL_KEY = "commerceos_operator_email";

function isBrowser() {
  return typeof window !== "undefined";
}

// getToken returns the stored bearer token, or null if there isn't one or
// it has passed its own recorded expiry. This is a client-side convenience
// only -- the server is the actual authority and still rejects an expired
// or revoked token with 401 (see authFetch below).
export function getToken(): string | null {
  if (!isBrowser()) return null;
  const token = window.localStorage.getItem(TOKEN_KEY);
  const expiresAt = window.localStorage.getItem(EXPIRES_KEY);
  if (!token || !expiresAt) return null;
  if (Date.now() >= Number(expiresAt)) {
    clearSession();
    return null;
  }
  return token;
}

export function getOperatorEmail(): string | null {
  if (!isBrowser()) return null;
  return window.localStorage.getItem(EMAIL_KEY);
}

export function isAuthenticated(): boolean {
  return getToken() !== null;
}

function setSession(token: string, expiresInSeconds: number, email: string) {
  if (!isBrowser()) return;
  window.localStorage.setItem(TOKEN_KEY, token);
  window.localStorage.setItem(EXPIRES_KEY, String(Date.now() + expiresInSeconds * 1000));
  window.localStorage.setItem(EMAIL_KEY, email);
}

export function clearSession() {
  if (!isBrowser()) return;
  window.localStorage.removeItem(TOKEN_KEY);
  window.localStorage.removeItem(EXPIRES_KEY);
  window.localStorage.removeItem(EMAIL_KEY);
}

type LoginResponse = {
  token: string;
  operator_id: string;
  merchant_id: string;
  email: string;
  expires_in_seconds: number;
};

export async function login(email: string, password: string): Promise<void> {
  const res = await fetch(`${API_BASE}/auth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password }),
  });
  if (!res.ok) throw new Error("Invalid email or password");
  const data = (await res.json()) as LoginResponse;
  setSession(data.token, data.expires_in_seconds, data.email);
}

export async function logout(): Promise<void> {
  const token = getToken();
  clearSession();
  if (!token) return;
  try {
    await fetch(`${API_BASE}/auth/logout`, {
      method: "POST",
      headers: { Authorization: `Bearer ${token}` },
    });
  } catch {
    // best-effort -- the local session is already cleared either way
  }
}

// authFetch attaches the operator's bearer token (if any) to a request
// against the CommerceOS API. A 401 means the session is gone (expired or
// revoked, or never existed) -- clear it locally so AuthGate re-prompts
// for login on the next render.
export async function authFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const token = getToken();
  const headers = new Headers(init.headers);
  if (token) headers.set("Authorization", `Bearer ${token}`);
  const res = await fetch(`${API_BASE}${path}`, { ...init, headers });
  if (res.status === 401) clearSession();
  return res;
}
