"use client";

import Link from "next/link";
import { useDashboardAlerts } from "./alerts";

const NAV: Array<[string, string]> = [
  ["Overview", "/dashboard"],
  ["Catalog", "/dashboard/catalog"],
  ["Orders", "/dashboard/orders"],
  ["Analytics", "/dashboard/analytics"],
  ["Approvals", "/dashboard/approvals"],
  ["Campaigns", "/dashboard/campaigns"],
  ["Growth", "/dashboard/growth"],
  ["Runs", "/dashboard/runs"],
  ["Safety", "/dashboard/safety"],
];

// Item 26 (PLAN-05-SELLER-DASHBOARD.md §5): a pending-approvals count
// badge on the "Approvals" nav item -- the underlying data already
// existed (GET /approval-requests?status=PENDING, read today only
// after navigating into Approvals itself), this just surfaces the
// count in the nav so it's visible from anywhere in the dashboard.
// Client component (moved out of layout.tsx, which stays a Server
// Component) because useDashboardAlerts needs the operator's bearer
// token, which only exists in the browser.
export default function DashboardNav() {
  const { pendingApprovals } = useDashboardAlerts();

  return (
    <nav aria-label="Dashboard" className="mt-5 flex gap-1 overflow-x-auto lg:flex-col lg:gap-1.5">
      {NAV.map(([label, href]) => (
        <Link
          key={href}
          href={href}
          className="flex shrink-0 items-center justify-between gap-2 rounded-lg px-3 py-2 text-sm font-medium text-slate-600 hover:bg-slate-100 hover:text-slate-950"
        >
          <span>{label}</span>
          {label === "Approvals" && pendingApprovals > 0 && (
            <span
              aria-label={`${pendingApprovals} pending approval${pendingApprovals === 1 ? "" : "s"}`}
              className="rounded-full bg-amber-500 px-1.5 py-0.5 text-xs font-semibold leading-none text-white"
            >
              {pendingApprovals}
            </span>
          )}
        </Link>
      ))}
    </nav>
  );
}
