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

export type PluginStatus = "ok" | "degraded" | "down" | "unreachable";

export type ActionMode = "sync" | "async";

export interface ActionSpec {
  name: string;
  description: string;
  mode: ActionMode;
  outcomes: string[];
  capabilities?: string[];
  default_timeout_seconds?: number;
}

export interface TriggerSpec {
  name: string;
  description: string;
  payload_schema?: unknown;
}

export interface PluginManifest {
  protocol: string;
  name: string;
  version?: string;
  description: string;
  actions: ActionSpec[];
  triggers?: TriggerSpec[];
}

export interface PluginRecord {
  name: string;
  base_url: string;
  manifest: PluginManifest;
  status: PluginStatus;
  error?: string;
  checked_at: string;
  created_at: string;
  updated_at: string;
}

export interface CatalogStep {
  name: string;
  description: string;
  queue: string;
  outcomes: string[];
  input_schema?: unknown;
  plugin?: string;
}

export interface CatalogTrigger {
  plugin: string;
  name: string;
  description: string;
  payload_schema: unknown;
}

export interface Catalog {
  steps: CatalogStep[];
  triggers: CatalogTrigger[];
}
