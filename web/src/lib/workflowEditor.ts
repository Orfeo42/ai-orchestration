import type { WorkflowDefinition, WorkflowStepDef } from "@/process-types";

export const RESERVED_STEP_IDS: readonly string[] = ["end", "fail"];

export type TriggerKind = "none" | "plugin" | "forgejo";

export interface MatchRow {
  key: string;
  name: string;
  value: string;
}

export interface EditorStep {
  key: string;
  id: string;
  description: string;
  uses: string;
  withText: string;
  timeout: string;
  retry: string;
  on: Record<string, string>;
}

export interface EditorState {
  isNew: boolean;
  name: string;
  description: string;
  triggerKind: TriggerKind;
  pluginTrigger: string;
  matchRows: MatchRow[];
  forgejoRepos: string;
  forgejoLabel: string;
  forgejoAssignee: string;
  start: string;
  steps: EditorStep[];
}

export interface StepErrors {
  id?: string;
  with?: string;
  retry?: string;
}

export const emptyEditorState: EditorState = {
  isNew: true,
  name: "",
  description: "",
  triggerKind: "none",
  pluginTrigger: "",
  matchRows: [],
  forgejoRepos: "",
  forgejoLabel: "",
  forgejoAssignee: "",
  start: "",
  steps: [],
};

type ParsedJSON = { ok: true; value: unknown } | { ok: false; error: string };

export function parseJSON(text: string): ParsedJSON {
  try {
    const value: unknown = JSON.parse(text);
    return { ok: true, value };
  } catch (e: unknown) {
    return {
      ok: false,
      error: e instanceof Error ? e.message : "Invalid JSON",
    };
  }
}

export function parseMatchValue(text: string): unknown {
  const parsed = parseJSON(text);
  return parsed.ok ? parsed.value : text;
}

export function formatMatchValue(value: unknown): string {
  if (typeof value === "string" && !parseJSON(value).ok) return value;
  return JSON.stringify(value);
}

export function newMatchRow(): MatchRow {
  return { key: crypto.randomUUID(), name: "", value: "" };
}

export function fromDefinition(def: WorkflowDefinition): EditorState {
  const entries = Object.entries(def.steps).sort(
    ([a], [b]) => Number(b === def.start) - Number(a === def.start),
  );
  const idToKey = new Map(entries.map(([id]) => [id, crypto.randomUUID()]));
  const steps = entries.map(([id, step]): EditorStep => {
    const on: Record<string, string> = {};
    for (const [outcome, target] of Object.entries(step.on)) {
      on[outcome] = idToKey.get(target) ?? target;
    }
    return {
      key: idToKey.get(id) ?? crypto.randomUUID(),
      id,
      description: step.description ?? "",
      uses: step.uses,
      withText:
        step.with === undefined ? "" : JSON.stringify(step.with, null, 2),
      timeout: step.timeout ?? "",
      retry: step.retry === undefined ? "" : String(step.retry),
      on,
    };
  });

  const state: EditorState = {
    ...emptyEditorState,
    isNew: false,
    name: def.name,
    description: def.description ?? "",
    start: idToKey.get(def.start) ?? "",
    steps,
  };

  if (def.trigger.plugin !== undefined) {
    return {
      ...state,
      triggerKind: "plugin",
      pluginTrigger: `${def.trigger.plugin}/${def.trigger.event ?? ""}`,
      matchRows: Object.entries(def.trigger.match ?? {}).map(
        ([name, value]) => ({
          key: crypto.randomUUID(),
          name,
          value: formatMatchValue(value),
        }),
      ),
    };
  }
  if (def.trigger.forgejo !== undefined) {
    return {
      ...state,
      triggerKind: "forgejo",
      forgejoRepos: def.trigger.forgejo.repos.join(", "),
      forgejoLabel: def.trigger.forgejo.label,
      forgejoAssignee: def.trigger.forgejo.assignee ?? "",
    };
  }
  return state;
}

export function toDefinition(state: EditorState): WorkflowDefinition {
  const keyToId = new Map(state.steps.map((s) => [s.key, s.id.trim()]));
  const steps: Record<string, WorkflowStepDef> = {};

  for (const step of state.steps) {
    const on: Record<string, string> = {};
    for (const [outcome, target] of Object.entries(step.on)) {
      on[outcome] = keyToId.get(target) ?? target;
    }
    const def: WorkflowStepDef = { uses: step.uses, on };
    if (step.description.trim() !== "") def.description = step.description;
    if (step.timeout.trim() !== "") def.timeout = step.timeout.trim();
    if (step.retry.trim() !== "" && Number(step.retry) > 0) {
      def.retry = Number(step.retry);
    }
    if (step.withText.trim() !== "") {
      const parsed = parseJSON(step.withText);
      if (parsed.ok) def.with = parsed.value;
    }
    steps[step.id.trim()] = def;
  }

  const definition: WorkflowDefinition = {
    name: state.name.trim(),
    trigger: {},
    start: keyToId.get(state.start) ?? "",
    steps,
  };
  if (state.description.trim() !== "") {
    definition.description = state.description;
  }

  if (state.triggerKind === "plugin") {
    const slash = state.pluginTrigger.indexOf("/");
    definition.trigger.plugin = state.pluginTrigger.slice(0, slash);
    definition.trigger.event = state.pluginTrigger.slice(slash + 1);
    const rows = state.matchRows.filter((r) => r.name.trim() !== "");
    if (rows.length > 0) {
      definition.trigger.match = Object.fromEntries(
        rows.map((r) => [r.name.trim(), parseMatchValue(r.value)]),
      );
    }
  }
  if (state.triggerKind === "forgejo") {
    definition.trigger.forgejo = {
      repos: state.forgejoRepos
        .split(",")
        .map((r) => r.trim())
        .filter((r) => r !== ""),
      label: state.forgejoLabel.trim(),
    };
    if (state.forgejoAssignee.trim() !== "") {
      definition.trigger.forgejo.assignee = state.forgejoAssignee.trim();
    }
  }
  return definition;
}

export function stepErrors(state: EditorState, step: EditorStep): StepErrors {
  const errors: StepErrors = {};
  const id = step.id.trim();
  if (id === "") {
    errors.id = "Step id is required";
  } else if (RESERVED_STEP_IDS.includes(id)) {
    errors.id = `"${id}" is reserved`;
  } else if (
    state.steps.some((s) => s.key !== step.key && s.id.trim() === id)
  ) {
    errors.id = "Step id must be unique";
  }

  if (step.withText.trim() !== "") {
    const parsed = parseJSON(step.withText);
    if (!parsed.ok) errors.with = parsed.error;
  }

  const retry = step.retry.trim();
  if (
    retry !== "" &&
    !(Number.isInteger(Number(retry)) && Number(retry) >= 0)
  ) {
    errors.retry = "Retry must be a whole number, 0 or more";
  }
  return errors;
}

export function hasErrors(state: EditorState): boolean {
  if (state.name.trim() === "") return true;
  return state.steps.some((s) => Object.keys(stepErrors(state, s)).length > 0);
}

export function updateStep(
  state: EditorState,
  key: string,
  patch: Partial<EditorStep>,
): EditorState {
  return {
    ...state,
    steps: state.steps.map((s) => (s.key === key ? { ...s, ...patch } : s)),
  };
}

export function setUses(
  state: EditorState,
  key: string,
  uses: string,
  outcomes: readonly string[],
): EditorState {
  return {
    ...state,
    steps: state.steps.map((s) => {
      if (s.key !== key) return s;
      const on: Record<string, string> = {};
      for (const [outcome, target] of Object.entries(s.on)) {
        if (outcomes.includes(outcome)) on[outcome] = target;
      }
      return { ...s, uses, on };
    }),
  };
}

export function setTransition(
  state: EditorState,
  key: string,
  outcome: string,
  target: string,
): EditorState {
  return updateStep(state, key, {
    on: {
      ...(state.steps.find((s) => s.key === key)?.on ?? {}),
      [outcome]: target,
    },
  });
}

export function addStep(state: EditorState): EditorState {
  const taken = new Set(state.steps.map((s) => s.id.trim()));
  let n = state.steps.length + 1;
  while (taken.has(`step${String(n)}`)) n++;
  const step: EditorStep = {
    key: crypto.randomUUID(),
    id: `step${String(n)}`,
    description: "",
    uses: "",
    withText: "",
    timeout: "",
    retry: "",
    on: {},
  };
  return {
    ...state,
    start: state.start === "" ? step.key : state.start,
    steps: [...state.steps, step],
  };
}

export function removeStep(state: EditorState, key: string): EditorState {
  return {
    ...state,
    start: state.start === key ? "" : state.start,
    steps: state.steps
      .filter((s) => s.key !== key)
      .map((s) => ({
        ...s,
        on: Object.fromEntries(
          Object.entries(s.on).filter(([, target]) => target !== key),
        ),
      })),
  };
}
