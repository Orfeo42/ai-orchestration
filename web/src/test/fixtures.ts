import type {
  Process,
  ProcessDetail,
  StepRun,
  WorkflowVersion,
} from "@/process-types";
import type { Catalog, PluginRecord, TokenUsage } from "@/types";

export const usage: TokenUsage = {
  input: 1200,
  output: 300,
  cache_creation_input: 10,
  cache_read_input: 20,
};

export function makeProcess(overrides: Partial<Process> = {}): Process {
  return {
    id: "p1",
    workflow_version_id: 3,
    workflow_name: "implement",
    subject: {
      kind: "issue",
      key: "acme/repo#7",
      url: "https://git.example/acme/repo/issues/7",
    },
    title: "Fix the widget",
    project: "billing",
    status: "running",
    current_step: "code",
    vars: {},
    usage,
    created_at: "2026-10-01T10:00:00Z",
    updated_at: "2026-10-01T10:05:00Z",
    started_at: "2026-10-01T10:00:00Z",
    ...overrides,
  };
}

export function makeStepRun(overrides: Partial<StepRun> = {}): StepRun {
  return {
    id: "s1",
    process_id: "p1",
    step_id: "code",
    uses: "claude/implement",
    attempt: 1,
    iteration: 0,
    river_job_id: 5,
    worker_id: "w1",
    status: "succeeded",
    outcome: "ok",
    usage,
    started_at: "2026-10-01T10:00:00Z",
    ended_at: "2026-10-01T10:02:00Z",
    ...overrides,
  };
}

export function makeWorkflow(
  overrides: Partial<WorkflowVersion> = {},
): WorkflowVersion {
  return {
    id: 3,
    name: "implement",
    hash: "abcdef1234567890",
    definition: {
      name: "implement",
      description: "Implement an issue",
      trigger: {},
      start: "code",
      steps: {
        code: {
          uses: "claude/implement",
          on: { ok: "approve", error: "fail" },
        },
        approve: {
          uses: "human/approve",
          on: { approved: "end", rejected: "code" },
        },
      },
    },
    source_yaml: "name: implement\nstart: code\n",
    created_at: "2026-10-01T09:00:00Z",
    ...overrides,
  };
}

export function makeDetail(
  process: Partial<Process> = {},
  stepRuns: StepRun[] = [makeStepRun()],
): ProcessDetail {
  return {
    process: makeProcess(process),
    step_runs: stepRuns,
    workflow: makeWorkflow(),
  };
}

export function makeCatalog(): Catalog {
  return {
    steps: [
      {
        name: "claude/implement",
        description: "Implement the issue",
        queue: "claude",
        outcomes: ["ok", "error"],
      },
      {
        name: "human/approve",
        description: "Wait for approval",
        queue: "default",
        outcomes: ["approved", "rejected"],
      },
    ],
    triggers: [
      {
        plugin: "forgejo",
        name: "issue_labeled",
        description: "Issue labeled",
        payload_schema: {},
      },
    ],
  };
}

export function makePlugin(
  overrides: Partial<PluginRecord> = {},
): PluginRecord {
  return {
    name: "forgejo",
    base_url: "http://forgejo:8080",
    manifest: {
      protocol: "v1",
      name: "forgejo",
      version: "1.0.0",
      description: "Forgejo plugin",
      actions: [
        {
          name: "comment",
          description: "Post a comment",
          mode: "sync",
          outcomes: ["ok", "error"],
        },
      ],
      triggers: [
        {
          name: "issue_labeled",
          description: "Issue labeled",
          payload_schema: {},
        },
      ],
    },
    status: "ok",
    checked_at: "2026-10-01T10:00:00Z",
    created_at: "2026-10-01T09:00:00Z",
    updated_at: "2026-10-01T10:00:00Z",
    ...overrides,
  };
}
