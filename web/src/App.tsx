import { Link, NavLink, Outlet } from "react-router";
import { Activity, ChartColumn, GitBranch, Workflow } from "lucide-react";
import { cn } from "@/lib/utils";
import { ThemeToggle } from "@/components/ThemeToggle";

const navItems = [
  { to: "/", label: "Processes", icon: Activity, end: true },
  { to: "/workflows", label: "Workflows", icon: GitBranch, end: false },
  { to: "/analytics", label: "Analytics", icon: ChartColumn, end: false },
];

export function App() {
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
        <nav className="flex flex-1 flex-col gap-1 p-3">
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
        <div className="border-t p-3">
          <ThemeToggle />
        </div>
      </aside>
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 items-center gap-3 border-b px-6 md:hidden">
          <Workflow className="h-5 w-5 text-primary" />
          <span className="font-semibold">ai-orchestrator</span>
          <div className="ml-auto">
            <ThemeToggle className="h-8 w-auto gap-2" />
          </div>
        </header>
        <main className="flex-1 overflow-auto p-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
