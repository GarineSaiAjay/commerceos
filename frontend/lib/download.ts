// Shared CSV-download helper for the merchant dashboard (item 27, P2,
// PLAN-05-SELLER-DASHBOARD.md section 6). The export endpoints
// (GET /dashboard/orders/export, GET /campaigns/export) require the
// operator's bearer token exactly like every other dashboard request,
// so a plain <a href="..."> can't be used directly -- the browser's
// native navigation never attaches the Authorization header authFetch
// adds (lib/auth.ts). Instead: fetch the CSV through authFetch like any
// other authenticated request, then trigger the actual file save with a
// throwaway object URL + a synthetic, immediately-clicked <a download>.
// This is the standard workaround for "authenticated file download" in
// a browser and isn't specific to CSV -- named generically in case a
// future export (e.g. a JSON/PDF report) wants the same helper.
import { authFetch } from "./auth";

export async function downloadFile(path: string, filename: string): Promise<void> {
  const res = await authFetch(path, { cache: "no-store" });
  if (!res.ok) {
    throw new Error(`Export failed (${res.status})`);
  }
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  try {
    const link = document.createElement("a");
    link.href = url;
    link.download = filename;
    // Not attached to the DOM tree anywhere a user or a stylesheet
    // could see it -- click() works on a detached element in every
    // browser this app targets, and it's removed again immediately
    // after, so nothing lingers.
    link.click();
  } finally {
    URL.revokeObjectURL(url);
  }
}
