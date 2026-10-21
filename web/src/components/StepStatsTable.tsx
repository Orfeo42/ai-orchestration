import type { StepStats } from "@/process-types";
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
  stats: StepStats[];
};

export function StepStatsTable({ stats }: Props) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Step</TableHead>
          <TableHead>Uses</TableHead>
          <TableHead className="text-right">Runs</TableHead>
          <TableHead>Outcomes</TableHead>
          <TableHead className="text-right">Failure rate</TableHead>
          <TableHead className="text-right">Retries</TableHead>
          <TableHead className="text-right">p50</TableHead>
          <TableHead className="text-right">p95</TableHead>
          <TableHead className="text-right">Tokens in / out</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {stats.map((s) => (
          <TableRow key={s.step_id}>
            <TableCell className="font-medium">{s.step_id}</TableCell>
            <TableCell className="font-mono text-xs">{s.uses}</TableCell>
            <TableCell className="text-right tabular-nums">
              {formatCount(s.runs)}
            </TableCell>
            <TableCell>
              {Object.entries(s.outcomes)
                .map(([outcome, count]) => `${outcome}: ${formatCount(count)}`)
                .join(", ")}
            </TableCell>
            <TableCell className="text-right tabular-nums">
              {formatPercent(s.failure_rate)}
            </TableCell>
            <TableCell className="text-right tabular-nums">
              {formatCount(s.retries)}
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
