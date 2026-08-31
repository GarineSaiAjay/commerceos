"use client";

import { createContext, useCallback, useContext, useEffect, useState } from "react";
import { authFetch, AUTH_CHANGED_EVENT, isAuthenticated } from "../../lib/auth";

// Item 26 (ROADMAP-PRIORITIZED.md P2, PLAN-05-SELLER-DASHBOARD.md §5):
// a single client-side source of truth for the three "something needs
// attention" signals the nav badge and the persistent top-of-dashboard
// banners both read from, so a pending approval, an exhausted campaign,
// or a broken audit chain is visible no matter which /dashboard/* page
// an operator is currently on -- not just after navigating into the
// one page that happens to show it.

export type ExhaustedCampaign = {
  campaign_id: string;
  product_id: string;
  budget_cap: number;
  spent: number;
};

type DashboardAlerts = {
  pendingApprovals: number;
  exhaustedCampaigns: ExhaustedCampaign[];
  chainBroken: boolean;
  brokenAtId?: number;
};

const EMPTY_ALERTS: DashboardAlerts = {
  pendingApprovals: 0,
  exhaustedCampaigns: [],
  chainBroken: false,
};

const AlertsContext = createContext<DashboardAlerts>(EMPTY_ALERTS);

// Polling, not a push channel -- there is no websocket/SSE surface in
// this backend to subscribe to instead, and 20s is frequent enough for
// a merchant back office without hammering three endpoints. The
// AUTH_CHANGED_EVENT listener below covers the one case a fixed poll
// interval handles badly: an operator who just signed in shouldn't
// have to wait up to 20s to see their real badge/banner state.
const POLL_INTERVAL_MS = 20_000;

type CampaignListItem = {
  campaign_id: string;
  product_id: string;
  budget_cap: number;
  spent: number;
  status: string;
};

type OverviewIntegrity = {
  audit_integrity: { chain_broken: boolean; broken_at_id?: number };
};

export function DashboardAlertsProvider({ children }: { children: React.ReactNode }) {
  const [alerts, setAlerts] = useState<DashboardAlerts>(EMPTY_ALERTS);

  const load = useCallback(async () => {
    // Skip the round trip entirely while signed out (the login screen,
    // or the instant before AuthGate has read localStorage) -- these
    // three endpoints are all RequireOperator-gated on the backend and
    // would just 401, same as merchant-dashboard.tsx's own pre-login
    // fetch already treats as an expected, not an error, state.
    if (!isAuthenticated()) {
      setAlerts(EMPTY_ALERTS);
      return;
    }
    try {
      const [approvalsRes, campaignsRes, overviewRes] = await Promise.all([
        authFetch("/approval-requests?status=PENDING", { cache: "no-store" }),
        authFetch("/campaigns", { cache: "no-store" }),
        authFetch("/dashboard/overview", { cache: "no-store" }),
      ]);

      const pendingApprovals = approvalsRes.ok ? ((await approvalsRes.json()) as unknown[]).length : 0;

      const campaigns = campaignsRes.ok ? ((await campaignsRes.json()) as CampaignListItem[]) : [];
      // Same "spent >= budget_cap" definition of exhausted that
      // BudgetBar (frontend/app/dashboard/campaigns/page.tsx) already
      // uses -- there is no separate `exhausted` field from the
      // backend to read instead. Scoped to ACTIVE campaigns only: a
      // REJECTED/COMPLETED/EXPIRED campaign being over its old budget
      // isn't something an operator needs to act on right now.
      const exhaustedCampaigns = campaigns
        .filter((c) => c.status === "ACTIVE" && c.budget_cap > 0 && c.spent >= c.budget_cap)
        .map((c) => ({ campaign_id: c.campaign_id, product_id: c.product_id, budget_cap: c.budget_cap, spent: c.spent }));

      const overview = overviewRes.ok ? ((await overviewRes.json()) as OverviewIntegrity) : null;

      setAlerts({
        pendingApprovals,
        exhaustedCampaigns,
        chainBroken: overview?.audit_integrity.chain_broken ?? false,
        brokenAtId: overview?.audit_integrity.broken_at_id,
      });
    } catch {
      // Best-effort -- the nav badge and banners just keep showing
      // their last known values rather than flashing to empty.
    }
  }, []);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    load();
    const intervalId = window.setInterval(load, POLL_INTERVAL_MS);
    window.addEventListener(AUTH_CHANGED_EVENT, load);
    return () => {
      window.clearInterval(intervalId);
      window.removeEventListener(AUTH_CHANGED_EVENT, load);
    };
  }, [load]);

  return <AlertsContext.Provider value={alerts}>{children}</AlertsContext.Provider>;
}

export function useDashboardAlerts(): DashboardAlerts {
  return useContext(AlertsContext);
}
