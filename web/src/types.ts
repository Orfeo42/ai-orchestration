export interface TokenUsage {
  input: number;
  output: number;
  cache_creation_input: number;
  cache_read_input: number;
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

export type FileStatus = "added" | "deleted" | "modified" | "renamed";

export interface FileDiff {
  path: string;
  old_path?: string;
  status: FileStatus;
  patch: string;
  additions: number;
  deletions: number;
}

export interface RunDiffResponse {
  pr_number: number;
  files: FileDiff[];
}
