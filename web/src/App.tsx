import { Link, NavLink, Outlet } from "react-router-dom";
import { Activity, Coins, Workflow } from "lucide-react";
import { cn } from "@/lib/utils";

const navItems = [
  { to: "/", label: "Runs", icon: Activity, end: true },
  { to: "/tokens", label: "Tokens", icon: Coins, end: false },
];

export default function App() {
  return (
    <div className="flex min-h-screen w-full bg-background text-foreground">
      <aside className="hidden w-60 shrink-0 border-r bg-card md:flex md:flex-col">
        <Link
          to="/"
          className="flex h-14 items-center gap-2 border-b px-5 font-semibold"
        >
          <Workflow className="h-5 w-5 text-primary" />
          <span>ai-orchestrator</span>
        </Link>
        <nav className="flex flex-col gap-1 p-3">
          {navItems.map(({ to, label, icon: Icon, end }) => (
            <NavLink
              key={to}
              to={to}
              end={end}
              className={({ isActive }) =>
                cn(
                  "flex items-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition-colors",
                  isActive
                    ? "bg-secondary text-foreground"
                    : "text-muted-foreground hover:bg-accent hover:text-foreground",
                )
              }
            >
              <Icon className="h-4 w-4" />
              {label}
            </NavLink>
          ))}
        </nav>
      </aside>
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 items-center gap-3 border-b px-6 md:hidden">
          <Workflow className="h-5 w-5 text-primary" />
          <span className="font-semibold">ai-orchestrator</span>
        </header>
        <main className="flex-1 overflow-auto p-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
