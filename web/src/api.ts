import type { DailyTokensResponse, Run, RunEvent } from "./types";

async function getJSON<T>(path: string): Promise<T> {
  const resp = await fetch(path);
  if (!resp.ok) {
    throw new Error(`${resp.status} ${resp.statusText}`);
  }
  return (await resp.json()) as T;
}

async function postJSON(path: string, body?: unknown): Promise<void> {
  const resp = await fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (!resp.ok) {
    throw new Error(`${resp.status} ${resp.statusText}`);
  }
}

export async function listRuns(params: {
  service?: string;
  phase?: string;
  limit?: number;
  cursor?: number;
}): Promise<Run[]> {
  const q = new URLSearchParams();
  if (params.service) q.set("service", params.service);
  if (params.phase) q.set("phase", params.phase);
  if (params.limit) q.set("limit", String(params.limit));
  if (params.cursor) q.set("cursor", String(params.cursor));
  const data = await getJSON<{ runs: Run[] }>(`/api/runs?${q.toString()}`);
  return data.runs ?? [];
}

export async function getRun(id: string): Promise<Run> {
  return getJSON<Run>(`/api/runs/${id}`);
}

export async function getRunEvents(id: string, since = 0): Promise<RunEvent[]> {
  const data = await getJSON<{ events: RunEvent[] }>(
    `/api/runs/${id}/events?since=${since}`,
  );
  return data.events ?? [];
}

export async function cancelRun(id: string): Promise<void> {
  return postJSON(`/api/runs/${id}/cancel`);
}

export async function injectNote(id: string, note: string): Promise<void> {
  return postJSON(`/api/runs/${id}/inject`, { note });
}

export async function getDailyTokens(
  from?: string,
  to?: string,
): Promise<DailyTokensResponse> {
  const q = new URLSearchParams();
  if (from) q.set("from", from);
  if (to) q.set("to", to);
  return getJSON<DailyTokensResponse>(`/api/stats/tokens?${q.toString()}`);
}
