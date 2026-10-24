import { Link } from "react-router";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Card, CardContent } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { formatDateTime } from "@/lib/format";
import { useWorkflows } from "@/queries";

export function WorkflowList() {
  const workflows = useWorkflows();

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold tracking-tight">Workflows</h1>
        <p className="text-sm text-muted-foreground">
          Latest version of every workflow.
        </p>
      </div>
      {workflows.error && (
        <Alert variant="destructive">
          <AlertDescription>{workflows.error.message}</AlertDescription>
        </Alert>
      )}
      {workflows.isPending && (
        <p className="text-sm text-muted-foreground">Loading workflows...</p>
      )}
      {workflows.data && (
        <Card>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Description</TableHead>
                  <TableHead className="text-right">Steps</TableHead>
                  <TableHead>Version</TableHead>
                  <TableHead>Created</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {workflows.data.map((w) => (
                  <TableRow key={w.id}>
                    <TableCell>
                      <Link
                        to={`/workflows/${encodeURIComponent(w.name)}`}
                        className="font-medium hover:underline"
                      >
                        {w.name}
                      </Link>
                    </TableCell>
                    <TableCell>{w.definition.description ?? "-"}</TableCell>
                    <TableCell className="text-right tabular-nums">
                      {Object.keys(w.definition.steps).length}
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {w.id} ({w.hash.slice(0, 8)})
                    </TableCell>
                    <TableCell>{formatDateTime(w.created_at)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
