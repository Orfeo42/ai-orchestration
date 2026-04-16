import { useEffect, useState } from "react";
import { getDailyTokens } from "../api";
import type { DailyTokensResponse } from "../types";

export default function Tokens() {
  const [data, setData] = useState<DailyTokensResponse | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    getDailyTokens()
      .then(setData)
      .catch((e) => setError(String(e)));
  }, []);

  if (error) return <div className="error">{error}</div>;
  if (!data) return <div>Loading...</div>;

  const days = Object.entries(data.daily).sort(([a], [b]) => a.localeCompare(b));
  const totals = days.reduce(
    (acc, [, u]) => ({
      input: acc.input + u.input,
      output: acc.output + u.output,
      cache_creation_input: acc.cache_creation_input + u.cache_creation_input,
      cache_read_input: acc.cache_read_input + u.cache_read_input,
    }),
    { input: 0, output: 0, cache_creation_input: 0, cache_read_input: 0 },
  );

  return (
    <section className="tokens-page">
      <header className="page-header">
        <h2>Token Usage</h2>
        <div className="meta">
          {data.from} → {data.to}
        </div>
      </header>

      <div className="usage-bar">
        <div>input: {totals.input.toLocaleString()}</div>
        <div>output: {totals.output.toLocaleString()}</div>
        <div>cache read: {totals.cache_read_input.toLocaleString()}</div>
        <div>cache create: {totals.cache_creation_input.toLocaleString()}</div>
      </div>

      <table className="runs-table">
        <thead>
          <tr>
            <th>Date</th>
            <th>Input</th>
            <th>Output</th>
            <th>Cache create</th>
            <th>Cache read</th>
          </tr>
        </thead>
        <tbody>
          {days.map(([date, u]) => (
            <tr key={date}>
              <td>{date}</td>
              <td>{u.input.toLocaleString()}</td>
              <td>{u.output.toLocaleString()}</td>
              <td>{u.cache_creation_input.toLocaleString()}</td>
              <td>{u.cache_read_input.toLocaleString()}</td>
            </tr>
          ))}
          {days.length === 0 && (
            <tr>
              <td colSpan={5} className="empty">
                No usage yet.
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </section>
  );
}
