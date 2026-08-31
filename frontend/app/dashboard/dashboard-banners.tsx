"use client";

import Link from "next/link";
import { formatINR } from "../../lib/format";
import { useDashboardAlerts } from "./alerts";

// Item 26 (PLAN-05-SELLER-DASHBOARD.md §5): two persistent banners,
// rendered once in layout.tsx above every /dashboard/* page's own
// content -- not just Overview's -- so both conditions are visible no
// matter which page an operator happens to be on. Renders nothing at
// all when neither condition holds, so this is invisible on the
// common path.
export default function DashboardBanners() {
  const { exhaustedCampaigns, chainBroken, brokenAtId } = useDashboardAlerts();

  if (!chainBroken && exhaustedCampaigns.length === 0) return null;

  return (
    <div className="space-y-3 px-5 pt-6 sm:px-8 lg:px-10">
      {chainBroken && (
        <div role="alert" className="rounded-xl border border-rose-300 bg-rose-50 p-4 text-sm text-rose-900">
          <p className="font-semibold">
            The audit chain is broken{brokenAtId !== undefined ? ` at event ${brokenAtId}` : ""}.
          </p>
          <p className="mt-1 text-rose-800">
            This is serious enough to show on every dashboard page: investigate before trusting order/policy
            history.{" "}
            <Link href="/dashboard" className="font-semibold underline underline-offset-2">
              Open Overview
            </Link>
          </p>
        </div>
      )}

      {exhaustedCampaigns.length > 0 && (
        <div role="alert" className="rounded-xl border border-amber-300 bg-amber-50 p-4 text-sm text-amber-950">
          <p className="font-semibold">
            {exhaustedCampaigns.length === 1
              ? "A campaign has exhausted its budget."
              : `${exhaustedCampaigns.length} campaigns have exhausted their budget.`}
          </p>
          <ul className="mt-1 space-y-0.5 text-amber-900">
            {exhaustedCampaigns.map((c) => (
              <li key={c.campaign_id}>
                {c.product_id}: {formatINR(c.spent)} of {formatINR(c.budget_cap)} spent
              </li>
            ))}
          </ul>
          <Link href="/dashboard/campaigns" className="mt-1 inline-block font-semibold underline underline-offset-2">
            Open Campaigns
          </Link>
        </div>
      )}
    </div>
  );
}
