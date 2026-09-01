import Link from "next/link";
import AuthGate from "./auth-gate";
import SignOutButton from "./sign-out-button";
import { DashboardAlertsProvider } from "./alerts";
import DashboardNav from "./dashboard-nav";
import DashboardBanners from "./dashboard-banners";

export const metadata = { title: "Dashboard — CommerceOS" };

export default function DashboardLayout({ children }: { children: React.ReactNode }) {
  return (
    <DashboardAlertsProvider>
      <div className="min-h-screen bg-slate-50 text-slate-900 lg:flex">
        <aside className="border-b border-slate-200 bg-white px-5 py-4 lg:flex lg:w-56 lg:shrink-0 lg:flex-col lg:border-r lg:border-b-0">
          <Link href="/dashboard" className="text-lg font-semibold tracking-tight text-slate-950">
            CommerceOS
          </Link>
          <p className="mt-1 text-sm text-slate-500">Merchant command center</p>
          <DashboardNav />
          <div className="lg:mt-auto">
            <SignOutButton />
          </div>
        </aside>
        <div className="min-w-0 flex-1">
          <AuthGate>
            <DashboardBanners />
            {children}
          </AuthGate>
        </div>
      </div>
    </DashboardAlertsProvider>
  );
}
