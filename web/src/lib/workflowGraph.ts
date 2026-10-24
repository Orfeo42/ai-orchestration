import type { WorkflowDefinition } from "@/process-types";

const TERMINALS: Record<string, { node: string; label: string }> = {
  end: { node: "t_end", label: "end" },
  fail: { node: "t_fail", label: "fail" },
};

function nodeId(stepId: string): string {
  return `n_${stepId.replace(/[^A-Za-z0-9_]/g, "_")}`;
}

function escapeLabel(text: string): string {
  return text.replace(/"/g, "'");
}

export function workflowGraphSource(def: WorkflowDefinition): string {
  const lines = ["flowchart TD", `  t_start((start)) --> ${nodeId(def.start)}`];
  const terminals = new Set<string>();

  for (const [stepId, step] of Object.entries(def.steps)) {
    lines.push(
      `  ${nodeId(stepId)}["${escapeLabel(stepId)}<br/>${escapeLabel(step.uses)}"]`,
    );
    for (const [outcome, target] of Object.entries(step.on)) {
      const terminal =
        def.steps[target] === undefined ? TERMINALS[target] : undefined;
      if (terminal) terminals.add(target);
      const targetNode = terminal ? terminal.node : nodeId(target);
      lines.push(
        `  ${nodeId(stepId)} -->|"${escapeLabel(outcome)}"| ${targetNode}`,
      );
    }
  }

  for (const key of terminals) {
    const terminal = TERMINALS[key];
    if (terminal) lines.push(`  ${terminal.node}([${terminal.label}])`);
  }
  return lines.join("\n");
}
