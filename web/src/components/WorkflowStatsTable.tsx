import type { WorkflowStats } from "@/process-types";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  formatCount,
  formatDuration,
  formatPercent,
  formatTokenPair,
} from "@/lib/format";

type Props = {
  stats: WorkflowStats[];
};

export function WorkflowStatsTable({ stats }: Props) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Workflow</TableHead>
          <TableHead className="text-right">Processes</TableHead>
          <TableHead className="text-right">Succeeded</TableHead>
          <TableHead className="text-right">Failed</TableHead>
          <TableHead className="text-right">Success rate</TableHead>
          <TableHead className="text-right">p50</TableHead>
          <TableHead className="text-right">p95</TableHead>
          <TableHead className="text-right">Tokens in / out</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {stats.map((s) => (
          <TableRow key={s.workflow}>
            <TableCell className="font-medium">{s.workflow}</TableCell>
            <TableCell className="text-right tabular-nums">
              {formatCount(s.processes)}
            </TableCell>
            <TableCell className="text-right tabular-nums">
              {formatCount(s.succeeded)}
            </TableCell>
            <TableCell className="text-right tabular-nums">
              {formatCount(s.failed)}
            </TableCell>
            <TableCell className="text-right tabular-nums">
              {formatPercent(s.success_rate)}
            </TableCell>
            <TableCell className="text-right tabular-nums">
              {formatDuration(s.duration_p50_ms)}
            </TableCell>
            <TableCell className="text-right tabular-nums">
              {formatDuration(s.duration_p95_ms)}
            </TableCell>
            <TableCell className="text-right tabular-nums">
              {formatTokenPair(s.usage)}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
