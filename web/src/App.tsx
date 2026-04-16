import { Link, Outlet } from "react-router-dom";

export default function App() {
  return (
    <div className="app-shell">
      <nav className="app-nav">
        <Link to="/" className="brand">
          ai-orchestrator
        </Link>
        <Link to="/">Runs</Link>
        <Link to="/tokens">Tokens</Link>
      </nav>
      <main className="app-main">
        <Outlet />
      </main>
    </div>
  );
}
