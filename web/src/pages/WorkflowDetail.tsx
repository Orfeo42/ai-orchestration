import { useMemo } from "react";
import { useParams } from "react-router";
import { WorkflowGraph } from "@/components/WorkflowGraph";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { formatDateTime } from "@/lib/format";
import { workflowGraphSource } from "@/lib/workflowGraph";
import { useWorkflowVersions } from "@/queries";

type ViewProps = {
  name: string;
};

function WorkflowDetailView({ name }: ViewProps) {
  const versions = useWorkflowVersions(name);
  const sorted = useMemo(
    () => [...(versions.data ?? [])].sort((a, b) => b.id - a.id),
    [versions.data],
  );
  const latest = sorted[0];
  const source = useMemo(
    () => (latest ? workflowGraphSource(latest.definition) : ""),
    [latest],
  );

  if (versions.isPending) {
    return <p className="text-sm text-muted-foreground">Loading workflow...</p>;
  }
  if (versions.error) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{versions.error.message}</AlertDescription>
      </Alert>
    );
  }
  if (!latest) {
    return <p className="text-sm text-muted-foreground">No versions found.</p>;
  }

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold tracking-tight">{name}</h1>
        {latest.definition.description !== undefined && (
          <p className="text-sm text-muted-foreground">
            {latest.definition.description}
          </p>
        )}
      </div>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">Graph</CardTitle>
          <CardDescription>
            Latest version, starting at {latest.definition.start}.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <WorkflowGraph source={source} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">YAML source</CardTitle>
        </CardHeader>
        <CardContent>
          <pre className="max-h-[60vh] overflow-auto rounded-md bg-muted p-3 text-xs">
            {latest.source_yaml}
          </pre>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">Versions</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Version</TableHead>
                <TableHead>Hash</TableHead>
                <TableHead>Created</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {sorted.map((v) => (
                <TableRow key={v.id}>
                  <TableCell className="tabular-nums">{v.id}</TableCell>
                  <TableCell className="font-mono text-xs">
                    {v.hash.slice(0, 12)}
                  </TableCell>
                  <TableCell>{formatDateTime(v.created_at)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  );
}

export function WorkflowDetailPage() {
  const { name } = useParams();
  if (name === undefined) return <p>Missing workflow name.</p>;
  return <WorkflowDetailView name={name} />;
}
