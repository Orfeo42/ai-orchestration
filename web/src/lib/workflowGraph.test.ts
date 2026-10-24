import { describe, expect, it } from "vitest";
import { makeWorkflow } from "@/test/fixtures";
import { workflowGraphSource } from "./workflowGraph";

describe("workflowGraphSource", () => {
  const def = makeWorkflow().definition;

  it("starts a top-down flowchart at the start step", () => {
    const lines = workflowGraphSource(def).split("\n");

    expect(lines[0]).toBe("flowchart TD");
    expect(lines[1]).toBe("  t_start((start)) --> n_code");
  });

  it("declares a node per step with its uses", () => {
    const source = workflowGraphSource(def);

    expect(source).toContain('n_code["code<br/>claude/implement"]');
    expect(source).toContain('n_approve["approve<br/>human/approve"]');
  });

  it("labels edges with the outcome", () => {
    const source = workflowGraphSource(def);

    expect(source).toContain('n_code -->|"ok"| n_approve');
    expect(source).toContain('n_approve -->|"rejected"| n_code');
  });

  it("routes end and fail targets to terminal nodes", () => {
    const source = workflowGraphSource(def);

    expect(source).toContain('n_approve -->|"approved"| t_end');
    expect(source).toContain('n_code -->|"error"| t_fail');
    expect(source).toContain("t_end([end])");
    expect(source).toContain("t_fail([fail])");
  });

  it("omits terminals that are never referenced", () => {
    const source = workflowGraphSource({
      ...def,
      steps: { code: { uses: "claude/implement", on: { ok: "end" } } },
    });

    expect(source).toContain("t_end([end])");
    expect(source).not.toContain("t_fail");
  });

  it("sanitizes step ids and quotes in labels", () => {
    const source = workflowGraphSource({
      ...def,
      start: "my-step",
      steps: { "my-step": { uses: 'a"b', on: { 'o"k': "end" } } },
    });

    expect(source).toContain("t_start((start)) --> n_my_step");
    expect(source).toContain(`n_my_step["my-step<br/>a'b"]`);
    expect(source).toContain(`n_my_step -->|"o'k"| t_end`);
  });
});
