import MerchantDashboard, { type Overview } from "./merchant-dashboard";

export const dynamic = "force-dynamic";

const API_BASE = process.env.NEXT_PUBLIC_COMMERCE_URL ?? "http://localhost:8081";

const emptyOverview: Overview = {
  metrics: { revenue: 0, ai_revenue: 0, conversion_rate: 0, average_order_value: 0, simulated: false },
  recent_activity: [],
  agent_actions: [],
  audit_integrity: { verified: false, chain_broken: false, rows_checked: 0 },
  safety: { available: false, message: "Dashboard data is unavailable." },
  generated_at: new Date(0).toISOString(),
};

export default async function DashboardPage() {
  let initialOverview = emptyOverview;
  let initialError: string | undefined;

  try {
    const response = await fetch(`${API_BASE}/dashboard/overview`, { cache: "no-store" });
    if (!response.ok) initialError = "The dashboard could not load live data.";
    else initialOverview = (await response.json()) as Overview;
  } catch {
    initialError = "The dashboard could not connect to CommerceOS.";
  }

  return <MerchantDashboard initialOverview={initialOverview} initialError={initialError} />;
}