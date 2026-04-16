export type Service = "orchestrator" | "reviewer";

export type Phase =
  | "assessing"
  | "implementing"
  | "testing"
  | "pushing"
  | "pr_description"
  | "reviewing"
  | "done"
  | "failed"
  | "cancelled";

export interface TokenUsage {
  input: number;
  output: number;
  cache_creation_input: number;
  cache_read_input: number;
}

export interface Run {
  id: string;
  service: Service;
  repo_owner: string;
  repo_name: string;
  issue_number?: number;
  pr_number?: number;
  title: string;
  project?: string;
  phase: Phase;
  attempt: number;
  started_at: string;
  ended_at?: string;
  usage: TokenUsage;
  error?: string;
  claude_session?: string;
  inject_notes?: string[];
}

export type EventKind =
  | "run_start"
  | "phase"
  | "assistant_text"
  | "tool_use"
  | "tool_result"
  | "thinking"
  | "result"
  | "run_end"
  | "cancel"
  | "inject"
  | "error"
  | "init";

export interface RunEvent {
  id?: string;
  run_id: string;
  seq?: number;
  kind: EventKind;
  at: string;
  text?: string;
  tool_name?: string;
  tool_input?: string;
  tool_id?: string;
  phase?: string;
  session_id?: string;
  service?: string;
  usage?: TokenUsage;
  result_text?: string;
}

export interface DailyTokensResponse {
  from: string;
  to: string;
  daily: Record<string, TokenUsage>;
}
