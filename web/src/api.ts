import type {
  FailureGroup,
  Grouping,
  Process,
  ProcessDetail,
  ProcessPage,
  ProcessStatus,
  StepRun,
  StepStats,
  TokenPoint,
  WorkflowDefinition,
  WorkflowStats,
  WorkflowVersion,
} from "./process-types";
import type { Catalog, PluginRecord, RunDiffResponse, RunEvent } from "./types";

export class ApiError extends Error {
  readonly status: number;
  readonly problems?: string[];

  constructor(status: number, message: string, problems?: string[]) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    if (problems !== undefined) this.problems = problems;
  }
}

type QueryParams = Record<string, string | number | undefined>;

function withQuery(path: string, params: QueryParams): string {
  const q = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === "") continue;
    q.set(key, String(value));
  }
  const qs = q.toString();
  return qs === "" ? path : `${path}?${qs}`;
}

interface ErrorDetails {
  message: string;
  problems?: string[];
}

async function readError(resp: Response): Promise<ErrorDetails> {
  const fallback = `${String(resp.status)} ${resp.statusText}`.trim();
  try {
    const body: unknown = await resp.json();
    if (typeof body !== "object" || body === null) return { message: fallback };
    const message =
      "error" in body && typeof body.error === "string" && body.error !== ""
        ? body.error
        : fallback;
    const raw: unknown = "problems" in body ? body.problems : undefined;
    if (!Array.isArray(raw)) return { message };
    const problems = raw.filter((p): p is string => typeof p === "string");
    return problems.length === 0 ? { message } : { message, problems };
  } catch {
    return { message: fallback };
  }
}

async function send(path: string, init?: RequestInit): Promise<Response> {
  const resp = await fetch(path, init);
  if (!resp.ok) {
    const details = await readError(resp);
    throw new ApiError(resp.status, details.message, details.problems);
  }
  return resp;
}

async function getJSON<T>(path: string): Promise<T> {
  const resp = await send(path);
  const data: unknown = await resp.json();
  return data as T;
}

async function postJSON<T>(path: string, body: unknown): Promise<T> {
  const resp = await send(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  const data: unknown = await resp.json();
  return data as T;
}

async function postNoContent(path: string, body?: unknown): Promise<void> {
  await send(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
}

export interface ProcessFilters {
  status?: ProcessStatus;
  workflow?: string;
  project?: string;
}

export interface ListProcessesParams extends ProcessFilters {
  limit?: number;
  cursor?: string;
}

export function listProcesses(
  params: ListProcessesParams,
): Promise<ProcessPage> {
  return getJSON<ProcessPage>(withQuery("/api/processes", { ...params }));
}

export function getProcess(id: string): Promise<ProcessDetail> {
  return getJSON<ProcessDetail>(`/api/processes/${encodeURIComponent(id)}`);
}

export function startProcess(
  workflow: string,
  subjectKey: string,
): Promise<Process> {
  return postJSON<Process>("/api/processes", {
    workflow,
    subject_key: subjectKey,
  });
}

export function cancelProcess(id: string): Promise<void> {
  return postNoContent(`/api/processes/${encodeURIComponent(id)}/cancel`);
}

export function retryProcess(id: string, step?: string): Promise<void> {
  return postNoContent(
    `/api/processes/${encodeURIComponent(id)}/retry`,
    step === undefined || step === "" ? {} : { step },
  );
}

export function signalProcess(id: string, outcome: string): Promise<void> {
  return postNoContent(`/api/processes/${encodeURIComponent(id)}/signal`, {
    outcome,
  });
}

export function getStepRun(id: string): Promise<StepRun> {
  return getJSON<StepRun>(`/api/step-runs/${encodeURIComponent(id)}`);
}

export async function listStepRunEvents(
  id: string,
  since = 0,
  limit?: number,
): Promise<RunEvent[]> {
  const data = await getJSON<{ events: RunEvent[] | null }>(
    withQuery(`/api/step-runs/${encodeURIComponent(id)}/events`, {
      since,
      limit,
    }),
  );
  return data.events ?? [];
}

export function cancelStepRun(id: string): Promise<void> {
  return postNoContent(`/api/step-runs/${encodeURIComponent(id)}/cancel`);
}

export function injectNote(id: string, note: string): Promise<void> {
  return postNoContent(`/api/step-runs/${encodeURIComponent(id)}/inject`, {
    note,
  });
}

export function getStepRunDiff(id: string): Promise<RunDiffResponse> {
  return getJSON<RunDiffResponse>(
    `/api/step-runs/${encodeURIComponent(id)}/diff`,
  );
}

export async function listWorkflows(): Promise<WorkflowVersion[]> {
  const data = await getJSON<{ workflows: WorkflowVersion[] | null }>(
    "/api/workflows",
  );
  return data.workflows ?? [];
}

export async function listWorkflowVersions(
  name: string,
): Promise<WorkflowVersion[]> {
  const data = await getJSON<{ versions: WorkflowVersion[] | null }>(
    `/api/workflows/${encodeURIComponent(name)}`,
  );
  return data.versions ?? [];
}

export interface DateRange {
  from: string;
  to: string;
}

export async function getWorkflowStats(
  range: DateRange,
): Promise<WorkflowStats[]> {
  const data = await getJSON<{ workflows: WorkflowStats[] | null }>(
    withQuery("/api/analytics/workflows", { ...range }),
  );
  return data.workflows ?? [];
}

export async function getStepStats(
  workflow: string,
  range: DateRange,
): Promise<StepStats[]> {
  const data = await getJSON<{ steps: StepStats[] | null }>(
    withQuery("/api/analytics/steps", { workflow, ...range }),
  );
  return data.steps ?? [];
}

export async function getTokenPoints(
  range: DateRange,
  group: Grouping,
): Promise<TokenPoint[]> {
  const data = await getJSON<{ points: TokenPoint[] | null }>(
    withQuery("/api/analytics/tokens", { ...range, group }),
  );
  return data.points ?? [];
}

export async function getFailures(range: DateRange): Promise<FailureGroup[]> {
  const data = await getJSON<{ failures: FailureGroup[] | null }>(
    withQuery("/api/analytics/failures", { ...range }),
  );
  return data.failures ?? [];
}

export async function getCatalog(): Promise<Catalog> {
  const data = await getJSON<{
    steps: Catalog["steps"] | null;
    triggers: Catalog["triggers"] | null;
  }>("/api/catalog");
  return { steps: data.steps ?? [], triggers: data.triggers ?? [] };
}

export async function listPlugins(): Promise<PluginRecord[]> {
  const data = await getJSON<PluginRecord[] | null>("/api/plugins");
  return data ?? [];
}

export function registerPlugin(
  name: string,
  baseUrl: string,
): Promise<PluginRecord> {
  return postJSON<PluginRecord>("/api/plugins", {
    name,
    base_url: baseUrl,
  });
}

export async function refreshPlugin(name: string): Promise<PluginRecord> {
  const resp = await send(`/api/plugins/${encodeURIComponent(name)}/refresh`, {
    method: "POST",
  });
  const data: unknown = await resp.json();
  return data as PluginRecord;
}

export async function removePlugin(name: string): Promise<void> {
  await send(`/api/plugins/${encodeURIComponent(name)}`, {
    method: "DELETE",
  });
}

export function saveWorkflow(
  definition: WorkflowDefinition,
): Promise<WorkflowVersion> {
  return postJSON<WorkflowVersion>("/api/workflows", definition);
}

export async function deleteWorkflow(name: string): Promise<void> {
  await send(`/api/workflows/${encodeURIComponent(name)}`, {
    method: "DELETE",
  });
}
