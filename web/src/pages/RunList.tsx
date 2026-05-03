import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { RotateCw } from "lucide-react";
import { listRuns, retryRun } from "@/api";
import type { Phase, Run, Service } from "@/types";
import { phaseLabel, phaseVariant } from "@/lib/phase";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription } from "@/components/ui/alert";

const ALL = "__all__";

export default function RunList() {
  const [runs, setRuns] = useState<Run[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [service, setService] = useState<Service | "">("");
  const [phase, setPhase] = useState<Phase | "">("");
  const [retrying, setRetrying] = useState<string | null>(null);

  async function onRetry(id: string) {
    setRetrying(id);
    try {
      await retryRun(id);
    } catch (e) {
      setError(String(e));
    } finally {
      setRetrying(null);
    }
  }

  useEffect(() => {
    let cancelled = false;
    async function load() {
      try {
        const data = await listRuns({ service, phase, limit: 100 });
        if (!cancelled) setRuns(data);
      } catch (e) {
        if (!cancelled) setError(String(e));
      }
    }
    load();
    const id = window.setInterval(load, 5000);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, [service, phase]);

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold tracking-tight">Runs</h1>
        <p className="text-sm text-muted-foreground">
          Live pipeline activity. Auto-refreshes every 5s.
        </p>
      </div>

      {error && (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      <Card>
        <CardHeader className="flex flex-row items-end justify-between gap-3 space-y-0">
          <div className="space-y-1">
            <CardTitle>All runs</CardTitle>
            <CardDescription>
              {runs.length} run{runs.length === 1 ? "" : "s"} matching filters
            </CardDescription>
          </div>
          <div className="flex flex-wrap gap-2">
            <Select
              value={service || ALL}
              onValueChange={(v) =>
                setService(v === ALL ? "" : (v as Service))
              }
            >
              <SelectTrigger className="w-40">
                <SelectValue placeholder="Service" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL}>All services</SelectItem>
                <SelectItem value="orchestrator">Orchestrator</SelectItem>
                <SelectItem value="reviewer">Reviewer</SelectItem>
              </SelectContent>
            </Select>
            <Select
              value={phase || ALL}
              onValueChange={(v) => setPhase(v === ALL ? "" : (v as Phase))}
            >
              <SelectTrigger className="w-40">
                <SelectValue placeholder="Phase" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL}>All phases</SelectItem>
                <SelectItem value="assessing">Assessing</SelectItem>
                <SelectItem value="implementing">Implementing</SelectItem>
                <SelectItem value="testing">Testing</SelectItem>
                <SelectItem value="pushing">Pushing</SelectItem>
                <SelectItem value="reviewing">Reviewing</SelectItem>
                <SelectItem value="done">Done</SelectItem>
                <SelectItem value="failed">Failed</SelectItem>
                <SelectItem value="cancelled">Cancelled</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </CardHeader>
        <CardContent className="px-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-6">Service</TableHead>
                <TableHead>Repo</TableHead>
                <TableHead>Issue/PR</TableHead>
                <TableHead>Title</TableHead>
                <TableHead>Phase</TableHead>
                <TableHead className="text-right">Tokens</TableHead>
                <TableHead>Started</TableHead>
                <TableHead className="pr-6 text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {runs.map((r) => {
                const total = r.usage.input + r.usage.output;
                const retryable =
                  r.phase === "failed" || r.phase === "cancelled";
                return (
                  <TableRow key={r.id}>
                    <TableCell className="pl-6 text-muted-foreground">
                      {r.service}
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {r.repo_owner}/{r.repo_name}
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {r.issue_number ? `#${r.issue_number}` : ""}
                      {r.pr_number ? ` PR#${r.pr_number}` : ""}
                    </TableCell>
                    <TableCell className="max-w-[28rem] truncate font-medium">
                      <Link
                        to={`/runs/${r.id}`}
                        className="hover:underline"
                        title={r.title || r.id}
                      >
                        {r.title || r.id}
                      </Link>
                    </TableCell>
                    <TableCell>
                      <Badge variant={phaseVariant[r.phase]}>
                        {phaseLabel[r.phase]}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {total.toLocaleString()}
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {new Date(r.started_at).toLocaleString()}
                    </TableCell>
                    <TableCell className="pr-6 text-right">
                      {retryable && (
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => onRetry(r.id)}
                          disabled={retrying === r.id}
                        >
                          <RotateCw className="h-3.5 w-3.5" />
                          {retrying === r.id ? "..." : "Retry"}
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                );
              })}
              {runs.length === 0 && (
                <TableRow>
                  <TableCell
                    colSpan={8}
                    className="h-24 text-center text-muted-foreground"
                  >
                    No runs yet.
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  );
}
