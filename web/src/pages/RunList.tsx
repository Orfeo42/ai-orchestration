import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { listRuns } from "../api";
import type { Phase, Run, Service } from "../types";

const phaseClass: Record<Phase, string> = {
  assessing: "phase-active",
  implementing: "phase-active",
  testing: "phase-active",
  pushing: "phase-active",
  pr_description: "phase-active",
  reviewing: "phase-active",
  done: "phase-done",
  failed: "phase-failed",
  cancelled: "phase-cancelled",
};

export default function RunList() {
  const [runs, setRuns] = useState<Run[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [service, setService] = useState<Service | "">("");
  const [phase, setPhase] = useState<Phase | "">("");

  useEffect(() => {
    let cancelled = false;
    async function load() {
      try {
        const data = await listRuns({ service, phase, limit: 100 });
        if (!cancelled) setRuns(data);
      } catch (e) {
        if (!cancelled) setError(String(e));
      }
    }
    load();
    const id = window.setInterval(load, 5000);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, [service, phase]);

  return (
    <section className="runs-page">
      <header className="page-header">
        <h2>Runs</h2>
        <div className="filters">
          <select value={service} onChange={(e) => setService(e.target.value as Service | "")}>
            <option value="">All services</option>
            <option value="orchestrator">Orchestrator</option>
            <option value="reviewer">Reviewer</option>
          </select>
          <select value={phase} onChange={(e) => setPhase(e.target.value as Phase | "")}>
            <option value="">All phases</option>
            <option value="assessing">Assessing</option>
            <option value="implementing">Implementing</option>
            <option value="testing">Testing</option>
            <option value="pushing">Pushing</option>
            <option value="reviewing">Reviewing</option>
            <option value="done">Done</option>
            <option value="failed">Failed</option>
            <option value="cancelled">Cancelled</option>
          </select>
        </div>
      </header>
      {error && <div className="error">{error}</div>}
      <table className="runs-table">
        <thead>
          <tr>
            <th>Service</th>
            <th>Repo</th>
            <th>Issue/PR</th>
            <th>Title</th>
            <th>Phase</th>
            <th>Tokens</th>
            <th>Started</th>
          </tr>
        </thead>
        <tbody>
          {runs.map((r) => (
            <tr key={r.id}>
              <td>{r.service}</td>
              <td>
                {r.repo_owner}/{r.repo_name}
              </td>
              <td>
                {r.issue_number ? `#${r.issue_number}` : ""}
                {r.pr_number ? ` PR#${r.pr_number}` : ""}
              </td>
              <td>
                <Link to={`/runs/${r.id}`}>{r.title || r.id}</Link>
              </td>
              <td>
                <span className={`phase-badge ${phaseClass[r.phase] ?? ""}`}>{r.phase}</span>
              </td>
              <td>{r.usage.input + r.usage.output}</td>
              <td>{new Date(r.started_at).toLocaleString()}</td>
            </tr>
          ))}
          {runs.length === 0 && (
            <tr>
              <td colSpan={7} className="empty">
                No runs yet.
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </section>
  );
}
