import { Link } from "react-router";
import type { Process } from "@/process-types";
import { ProcessStatusBadge } from "@/components/StatusBadge";
import { SubjectLink } from "@/components/SubjectLink";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  formatDateTime,
  formatDurationBetween,
  formatTokenPair,
} from "@/lib/format";

type Props = {
  processes: Process[];
};

export function ProcessTable({ processes }: Props) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Workflow</TableHead>
          <TableHead>Subject</TableHead>
          <TableHead>Title</TableHead>
          <TableHead>Project</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>Current step</TableHead>
          <TableHead className="text-right">Tokens in / out</TableHead>
          <TableHead>Created</TableHead>
          <TableHead className="text-right">Duration</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {processes.map((p) => (
          <TableRow key={p.id}>
            <TableCell>{p.workflow_name}</TableCell>
            <TableCell>
              <SubjectLink subject={p.subject} />
            </TableCell>
            <TableCell className="max-w-xs truncate">
              <Link
                to={`/processes/${p.id}`}
                className="font-medium hover:underline"
              >
                {p.title}
              </Link>
            </TableCell>
            <TableCell>{p.project ?? "-"}</TableCell>
            <TableCell>
              <ProcessStatusBadge status={p.status} />
            </TableCell>
            <TableCell>{p.current_step ?? "-"}</TableCell>
            <TableCell className="text-right tabular-nums">
              {formatTokenPair(p.usage)}
            </TableCell>
            <TableCell>{formatDateTime(p.created_at)}</TableCell>
            <TableCell className="text-right tabular-nums">
              {formatDurationBetween(
                p.started_at ?? p.created_at,
                p.ended_at ?? p.updated_at,
              )}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
