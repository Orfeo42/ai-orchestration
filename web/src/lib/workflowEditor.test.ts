import { describe, expect, it } from "vitest";
import { makeWorkflow } from "@/test/fixtures";
import {
  addStep,
  emptyEditorState,
  formatMatchValue,
  fromDefinition,
  hasErrors,
  parseMatchValue,
  removeStep,
  setTransition,
  setUses,
  stepErrors,
  toDefinition,
  updateStep,
} from "./workflowEditor";

function stepKey(state: ReturnType<typeof fromDefinition>, id: string): string {
  const step = state.steps.find((s) => s.id === id);
  if (step === undefined) throw new Error(`no step ${id}`);
  return step.key;
}

function stepByKey(
  state: ReturnType<typeof fromDefinition>,
  key: string,
): ReturnType<typeof fromDefinition>["steps"][number] {
  const step = state.steps.find((s) => s.key === key);
  if (step === undefined) throw new Error(`no step ${key}`);
  return step;
}

describe("fromDefinition / toDefinition", () => {
  it("round trips a plugin trigger definition", () => {
    const def = makeWorkflow().definition;
    def.trigger = {
      plugin: "forgejo",
      event: "issue_labeled",
      match: { label: "ai", count: 3 },
    };
    def.steps["code"] = {
      description: "write code",
      uses: "claude/implement",
      with: { model: "x" },
      timeout: "30m",
      retry: 2,
      on: { ok: "approve", error: "fail" },
    };

    expect(toDefinition(fromDefinition(def))).toEqual(def);
  });

  it("round trips a forgejo trigger with omitted optional fields", () => {
    const def = makeWorkflow().definition;
    delete def.description;
    def.trigger = { forgejo: { repos: ["a/b", "c/d"], label: "ai" } };

    expect(toDefinition(fromDefinition(def))).toEqual(def);
  });

  it("puts the start step first", () => {
    const def = makeWorkflow().definition;
    def.start = "approve";

    const state = fromDefinition(def);

    expect(state.steps.map((s) => s.id)).toEqual(["approve", "code"]);
  });

  it("omits empty optional step fields", () => {
    const state = addStep(emptyEditorState);
    const key = state.steps[0]?.key ?? "";
    const next = updateStep({ ...state, name: "wf" }, key, {
      id: "a",
      uses: "x/y",
      retry: "0",
    });

    expect(toDefinition(next).steps["a"]).toEqual({ uses: "x/y", on: {} });
  });
});

describe("match values", () => {
  it("parses JSON when valid and keeps strings otherwise", () => {
    expect(parseMatchValue("3")).toBe(3);
    expect(parseMatchValue("true")).toBe(true);
    expect(parseMatchValue("hello")).toBe("hello");
  });

  it("formats values so they parse back to the same value", () => {
    expect(formatMatchValue("hello")).toBe("hello");
    expect(parseMatchValue(formatMatchValue("123"))).toBe("123");
    expect(parseMatchValue(formatMatchValue(5))).toBe(5);
  });
});

describe("updateStep rename", () => {
  it("propagates the new id to start and transitions", () => {
    const state = fromDefinition(makeWorkflow().definition);

    const renamed = updateStep(state, stepKey(state, "approve"), {
      id: "review",
    });
    const def = toDefinition(renamed);

    expect(def.steps["code"]?.on["ok"]).toBe("review");
    expect(def.steps["review"]).toBeDefined();
    expect(def.steps["approve"]).toBeUndefined();

    const renamedStart = updateStep(state, stepKey(state, "code"), {
      id: "build",
    });
    expect(toDefinition(renamedStart).start).toBe("build");
    expect(toDefinition(renamedStart).steps["approve"]?.on["rejected"]).toBe(
      "build",
    );
  });
});

describe("removeStep", () => {
  it("clears transitions and start pointing to the step", () => {
    const state = fromDefinition(makeWorkflow().definition);

    const next = removeStep(state, stepKey(state, "approve"));

    expect(next.steps.map((s) => s.id)).toEqual(["code"]);
    expect(next.steps[0]?.on).toEqual({ error: "fail" });

    const noStart = removeStep(state, stepKey(state, "code"));
    expect(noStart.start).toBe("");
  });
});

describe("setUses", () => {
  it("drops transitions for outcomes the new step does not declare", () => {
    const state = fromDefinition(makeWorkflow().definition);

    const next = setUses(state, stepKey(state, "code"), "other/step", [
      "ok",
      "retry",
    ]);

    expect(next.steps[0]?.uses).toBe("other/step");
    expect(next.steps[0]?.on).toEqual({ ok: state.steps[1]?.key });
  });
});

describe("setTransition", () => {
  it("sets a target for an outcome", () => {
    const state = fromDefinition(makeWorkflow().definition);

    const next = setTransition(state, stepKey(state, "code"), "error", "end");

    expect(toDefinition(next).steps["code"]?.on["error"]).toBe("end");
  });
});

describe("addStep", () => {
  it("generates unique ids and sets start for the first step", () => {
    const first = addStep(emptyEditorState);
    const second = addStep(first);

    expect(first.start).toBe(first.steps[0]?.key);
    expect(second.start).toBe(first.start);
    expect(new Set(second.steps.map((s) => s.id)).size).toBe(2);
  });
});

describe("validation", () => {
  it("flags reserved, duplicate and empty ids", () => {
    const state = fromDefinition(makeWorkflow().definition);
    const code = stepKey(state, "code");

    const reserved = updateStep(state, code, { id: "end" });
    const duplicate = updateStep(state, code, { id: "approve" });
    const empty = updateStep(state, code, { id: " " });

    expect(stepErrors(reserved, stepByKey(reserved, code)).id).toContain(
      "reserved",
    );
    expect(stepErrors(duplicate, stepByKey(duplicate, code)).id).toContain(
      "unique",
    );
    expect(stepErrors(empty, stepByKey(empty, code)).id).toContain("required");
    expect(stepErrors(state, stepByKey(state, code))).toEqual({});
  });

  it("flags invalid JSON in with and bad retry", () => {
    const state = fromDefinition(makeWorkflow().definition);
    const code = stepKey(state, "code");

    const next = updateStep(state, code, { withText: "{", retry: "-1" });
    const errors = stepErrors(next, stepByKey(next, code));

    expect(errors.with).toBeDefined();
    expect(errors.retry).toBeDefined();
    expect(hasErrors(next)).toBe(true);
    expect(hasErrors(state)).toBe(false);
  });

  it("requires a name", () => {
    expect(hasErrors(emptyEditorState)).toBe(true);
  });
});
