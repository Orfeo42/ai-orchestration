import type { Phase } from "@/types";

type BadgeVariant =
  | "default"
  | "secondary"
  | "destructive"
  | "outline"
  | "success"
  | "warning"
  | "info";

export const phaseVariant: Record<Phase, BadgeVariant> = {
  assessing: "info",
  implementing: "info",
  testing: "info",
  pushing: "info",
  pr_description: "info",
  reviewing: "info",
  done: "success",
  failed: "destructive",
  cancelled: "warning",
};

export const phaseLabel: Record<Phase, string> = {
  assessing: "Assessing",
  implementing: "Implementing",
  testing: "Testing",
  pushing: "Pushing",
  pr_description: "PR description",
  reviewing: "Reviewing",
  done: "Done",
  failed: "Failed",
  cancelled: "Cancelled",
};

export function isTerminal(p: Phase | undefined): boolean {
  return p === "done" || p === "failed" || p === "cancelled";
}
