import type { ProcessStatus, StepStatus } from "@/process-types";
import { Badge } from "@/components/ui/badge";
import {
  processStatusLabel,
  processStatusVariant,
  stepStatusLabel,
  stepStatusVariant,
} from "@/lib/status";

type ProcessProps = { status: ProcessStatus };

type StepProps = { status: StepStatus };

export function ProcessStatusBadge({ status }: ProcessProps) {
  return (
    <Badge variant={processStatusVariant[status]}>
      {processStatusLabel[status]}
    </Badge>
  );
}

export function StepStatusBadge({ status }: StepProps) {
  return (
    <Badge variant={stepStatusVariant[status]}>{stepStatusLabel[status]}</Badge>
  );
}
