import type { ProcessStatus, StepStatus } from "@/process-types";

export type BadgeVariant =
  | "default"
  | "secondary"
  | "destructive"
  | "outline"
  | "success"
  | "warning"
  | "info";

export const processStatuses = [
  "pending",
  "running",
  "waiting",
  "succeeded",
  "failed",
  "cancelled",
] as const satisfies readonly ProcessStatus[];

export const processStatusVariant: Record<ProcessStatus, BadgeVariant> = {
  pending: "secondary",
  running: "info",
  waiting: "warning",
  succeeded: "success",
  failed: "destructive",
  cancelled: "outline",
};

export const processStatusLabel: Record<ProcessStatus, string> = {
  pending: "Pending",
  running: "Running",
  waiting: "Waiting",
  succeeded: "Succeeded",
  failed: "Failed",
  cancelled: "Cancelled",
};

export const stepStatusVariant: Record<StepStatus, BadgeVariant> = {
  running: "info",
  succeeded: "success",
  failed: "destructive",
  cancelled: "outline",
};

export const stepStatusLabel: Record<StepStatus, string> = {
  running: "Running",
  succeeded: "Succeeded",
  failed: "Failed",
  cancelled: "Cancelled",
};

export function isProcessTerminal(status: ProcessStatus): boolean {
  return (
    status === "succeeded" || status === "failed" || status === "cancelled"
  );
}

export function isProcessRetryable(status: ProcessStatus): boolean {
  return status === "failed" || status === "cancelled";
}
