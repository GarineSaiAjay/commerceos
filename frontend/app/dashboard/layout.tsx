import Link from "next/link";

const NAV = [
  ["Overview", "/dashboard"],
  ["Analytics", "/dashboard/analytics"],
  ["Approvals", "/dashboard/approvals"],
  ["Runs", "/dashboard/runs"],
  ["Safety", "/dashboard/safety"],
];

export const metadata = { title: "Dashboard — CommerceOS" };

export default function DashboardLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="min-h-screen bg-slate-50 text-slate-900 lg:flex">
      <aside className="border-b border-slate-200 bg-white px-5 py-4 lg:w-56 lg:shrink-0 lg:border-r lg:border-b-0">
        <Link href="/dashboard" className="text-lg font-semibold tracking-tight text-slate-950">
          CommerceOS
        </Link>
        <p className="mt-1 text-sm text-slate-500">Merchant command center</p>
        <nav aria-label="Dashboard" className="mt-5 flex gap-1 overflow-x-auto lg:flex-col lg:gap-1.5">
          {NAV.map(([label, href]) => (
            <Link
              key={href}
              href={href}
              className="shrink-0 rounded-lg px-3 py-2 text-sm font-medium text-slate-600 hover:bg-slate-100 hover:text-slate-950"
            >
              {label}
            </Link>
          ))}
        </nav>
      </aside>
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}