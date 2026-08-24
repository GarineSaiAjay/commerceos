// Tiny API client / shared types for the dashboard.
const API_BASE = process.env.NEXT_PUBLIC_COMMERCE_URL ?? "http://localhost:8081";

export async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, { cache: "no-store" });
  if (!res.ok) throw new Error(`${path} failed (${res.status})`);
  return (await res.json()) as T;
}

export { API_BASE };