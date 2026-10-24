import { useMemo, useState } from "react";
import { FilterSelect } from "@/components/FilterSelect";
import { StatCard } from "@/components/StatCard";
import { StepStatsTable } from "@/components/StepStatsTable";
import { TokensChart } from "@/components/TokensChart";
import { WorkflowStatsTable } from "@/components/WorkflowStatsTable";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { Grouping } from "@/process-types";
import {
  formatCount,
  formatDateTime,
  formatPercent,
  lastDaysRange,
} from "@/lib/format";
import {
  useFailures,
  useStepStats,
  useTokenPoints,
  useWorkflowStats,
} from "@/queries";

const groupOptions = [
  { value: "day", label: "Day" },
  { value: "project", label: "Project" },
  { value: "step", label: "Step" },
];

function isGrouping(value: string): value is Grouping {
  return value === "day" || value === "project" || value === "step";
}

function QueryError({ message }: { message: string }) {
  return (
    <Alert variant="destructive">
      <AlertDescription>{message}</AlertDescription>
    </Alert>
  );
}

export function Analytics() {
  const [initial] = useState(() => lastDaysRange(30));
  const [from, setFrom] = useState(initial.from);
  const [to, setTo] = useState(initial.to);
  const [workflow, setWorkflow] = useState("");
  const [group, setGroup] = useState<Grouping>("day");
  const range = useMemo(() => ({ from, to }), [from, to]);

  const workflowStats = useWorkflowStats(range);
  const selectedWorkflow =
    workflow || (workflowStats.data?.[0]?.workflow ?? "");
  const stepStats = useStepStats(selectedWorkflow, range);
  const tokens = useTokenPoints(range, group);
  const failures = useFailures(range);

  const workflowOptions = useMemo(
    () =>
      (workflowStats.data ?? []).map((s) => ({
        value: s.workflow,
        label: s.workflow,
      })),
    [workflowStats.data],
  );
  const totals = useMemo(() => {
    const stats = workflowStats.data ?? [];
    const processes = stats.reduce((sum, s) => sum + s.processes, 0);
    const succeeded = stats.reduce((sum, s) => sum + s.succeeded, 0);
    const tokensIn = stats.reduce((sum, s) => sum + s.usage.input, 0);
    const tokensOut = stats.reduce((sum, s) => sum + s.usage.output, 0);
    return { processes, succeeded, tokensIn, tokensOut };
  }, [workflowStats.data]);

  function handleGroup(next: string) {
    if (isGrouping(next)) setGroup(next);
  }

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex flex-col gap-1">
          <h1 className="text-2xl font-semibold tracking-tight">Analytics</h1>
          <p className="text-sm text-muted-foreground">
            Workflow outcomes, durations and token usage.
          </p>
        </div>
        <div className="flex items-end gap-2">
          <div className="grid gap-1">
            <Label htmlFor="range-from">From</Label>
            <Input
              id="range-from"
              type="date"
              value={from}
              onChange={(e) => {
                setFrom(e.target.value);
              }}
            />
          </div>
          <div className="grid gap-1">
            <Label htmlFor="range-to">To</Label>
            <Input
              id="range-to"
              type="date"
              value={to}
              onChange={(e) => {
                setTo(e.target.value);
              }}
            />
          </div>
        </div>
      </div>

      <section className="flex flex-col gap-3">
        <h2 className="text-lg font-semibold">Workflows</h2>
        {workflowStats.error && (
          <QueryError message={workflowStats.error.message} />
        )}
        {workflowStats.data && (
          <>
            <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
              <StatCard
                label="Processes"
                value={formatCount(totals.processes)}
              />
              <StatCard
                label="Success rate"
                value={
                  totals.processes === 0
                    ? "-"
                    : formatPercent(totals.succeeded / totals.processes)
                }
              />
              <StatCard
                label="Tokens in"
                value={formatCount(totals.tokensIn)}
              />
              <StatCard
                label="Tokens out"
                value={formatCount(totals.tokensOut)}
              />
            </div>
            <Card>
              <CardContent className="p-0">
                <WorkflowStatsTable stats={workflowStats.data} />
              </CardContent>
            </Card>
          </>
        )}
      </section>

      <section className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-lg font-semibold">Steps</h2>
          <FilterSelect
            label="Step workflow"
            value={selectedWorkflow}
            options={workflowOptions}
            onChange={setWorkflow}
            className="w-56"
          />
        </div>
        {stepStats.error && <QueryError message={stepStats.error.message} />}
        {stepStats.data && (
          <Card>
            <CardContent className="p-0">
              <StepStatsTable stats={stepStats.data} />
            </CardContent>
          </Card>
        )}
      </section>

      <section className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-lg font-semibold">Tokens</h2>
          <FilterSelect
            label="Group by"
            value={group}
            options={groupOptions}
            onChange={handleGroup}
            className="w-36"
          />
        </div>
        {tokens.error && <QueryError message={tokens.error.message} />}
        {tokens.data && (
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-base">Tokens by {group}</CardTitle>
              <CardDescription>Input and output tokens.</CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
              <TokensChart points={tokens.data} />
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{group}</TableHead>
                    <TableHead className="text-right">Input</TableHead>
                    <TableHead className="text-right">Output</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {tokens.data.map((p) => (
                    <TableRow key={p.bucket}>
                      <TableCell>{p.bucket}</TableCell>
                      <TableCell className="text-right tabular-nums">
                        {formatCount(p.usage.input)}
                      </TableCell>
                      <TableCell className="text-right tabular-nums">
                        {formatCount(p.usage.output)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        )}
      </section>

      <section className="flex flex-col gap-3">
        <h2 className="text-lg font-semibold">Failures</h2>
        {failures.error && <QueryError message={failures.error.message} />}
        {failures.data && (
          <Card>
            <CardContent className="p-0">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Kind</TableHead>
                    <TableHead>Error</TableHead>
                    <TableHead className="text-right">Count</TableHead>
                    <TableHead>Last seen</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {failures.data.map((f) => (
                    <TableRow key={`${f.error_kind}-${f.error}`}>
                      <TableCell>{f.error_kind || "-"}</TableCell>
                      <TableCell className="max-w-lg truncate">
                        {f.error}
                      </TableCell>
                      <TableCell className="text-right tabular-nums">
                        {formatCount(f.count)}
                      </TableCell>
                      <TableCell>{formatDateTime(f.last_at)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        )}
      </section>
    </div>
  );
}
