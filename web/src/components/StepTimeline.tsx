import type { StepRun } from "@/process-types";
import { StepStatusBadge } from "@/components/StatusBadge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { formatDurationBetween, formatTokenPair } from "@/lib/format";
import { cn } from "@/lib/utils";

type Props = {
  stepRuns: StepRun[];
  selectedId: string | undefined;
  onSelect: (id: string) => void;
};

export function StepTimeline({ stepRuns, selectedId, onSelect }: Props) {
  if (stepRuns.length === 0) {
    return (
      <p className="py-6 text-center text-sm text-muted-foreground">
        No step runs yet.
      </p>
    );
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Step</TableHead>
          <TableHead>Uses</TableHead>
          <TableHead className="text-right">Attempt</TableHead>
          <TableHead className="text-right">Iteration</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>Outcome</TableHead>
          <TableHead>Phase</TableHead>
          <TableHead className="text-right">Duration</TableHead>
          <TableHead className="text-right">Tokens in / out</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {stepRuns.map((run) => (
          <TableRow
            key={run.id}
            className={cn(run.id === selectedId && "bg-muted/60")}
          >
            <TableCell>
              <button
                type="button"
                aria-pressed={run.id === selectedId}
                onClick={() => {
                  onSelect(run.id);
                }}
                className="font-medium hover:underline"
              >
                {run.step_id}
              </button>
            </TableCell>
            <TableCell className="font-mono text-xs">{run.uses}</TableCell>
            <TableCell className="text-right tabular-nums">
              {run.attempt}
            </TableCell>
            <TableCell className="text-right tabular-nums">
              {run.iteration}
            </TableCell>
            <TableCell>
              <StepStatusBadge status={run.status} />
            </TableCell>
            <TableCell>{run.outcome ?? "-"}</TableCell>
            <TableCell>{run.phase ?? "-"}</TableCell>
            <TableCell className="text-right tabular-nums">
              {formatDurationBetween(run.started_at, run.ended_at)}
            </TableCell>
            <TableCell className="text-right tabular-nums">
              {formatTokenPair(run.usage)}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
