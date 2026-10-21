import type { TokenUsage } from "./types";

export type ProcessStatus =
  "pending" | "running" | "waiting" | "succeeded" | "failed" | "cancelled";

export type StepStatus = "running" | "succeeded" | "failed" | "cancelled";

export type ErrorKind = "" | "upstream" | "internal" | "business";

export type WaitKind = "comment" | "signal";

export interface Subject {
  kind: "issue";
  key: string;
  url?: string;
}

export interface Wait {
  kind: WaitKind;
  step: string;
  since: string;
  resume_outcome: string;
}

export type Vars = Record<string, unknown>;

export interface Process {
  id: string;
  workflow_version_id: number;
  workflow_name: string;
  subject: Subject;
  title: string;
  project?: string;
  status: ProcessStatus;
  current_step?: string;
  current_job_id?: number;
  vars: Vars;
  waiting_for?: Wait;
  error?: string;
  error_kind?: ErrorKind;
  usage: TokenUsage;
  created_at: string;
  updated_at: string;
  started_at?: string;
  ended_at?: string;
}

export interface StepRun {
  id: string;
  process_id: string;
  step_id: string;
  uses: string;
  attempt: number;
  iteration: number;
  river_job_id: number;
  worker_id: string;
  status: StepStatus;
  outcome?: string;
  phase?: string;
  error?: string;
  error_kind?: ErrorKind;
  usage: TokenUsage;
  claude_session?: string;
  output?: Vars;
  started_at: string;
  ended_at?: string;
}

export interface WorkflowStepDef {
  uses: string;
  with?: Record<string, unknown>;
  timeout?: string;
  retry?: number;
  on: Record<string, string>;
}

export interface WorkflowDefinition {
  name: string;
  description?: string;
  trigger: { forgejo?: { repos: string[]; label: string; assignee?: string } };
  start: string;
  steps: Record<string, WorkflowStepDef>;
}

export interface WorkflowVersion {
  id: number;
  name: string;
  hash: string;
  definition: WorkflowDefinition;
  source_yaml: string;
  created_at: string;
}

export interface ProcessPage {
  processes: Process[];
  next_cursor?: string;
}

export interface ProcessDetail {
  process: Process;
  step_runs: StepRun[];
  workflow: WorkflowVersion;
}

export interface WorkflowStats {
  workflow: string;
  processes: number;
  active: number;
  succeeded: number;
  failed: number;
  cancelled: number;
  success_rate: number;
  duration_p50_ms: number;
  duration_p95_ms: number;
  usage: TokenUsage;
}

export interface StepStats {
  workflow: string;
  step_id: string;
  uses: string;
  runs: number;
  outcomes: Record<string, number>;
  failure_rate: number;
  retries: number;
  duration_p50_ms: number;
  duration_p95_ms: number;
  usage: TokenUsage;
}

export type Grouping = "day" | "project" | "step";

export interface TokenPoint {
  bucket: string;
  usage: TokenUsage;
}

export interface FailureGroup {
  error_kind: ErrorKind;
  error: string;
  count: number;
  last_at: string;
}
