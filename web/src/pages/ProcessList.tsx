import { useDeferredValue, useMemo, useState } from "react";
import { FilterSelect } from "@/components/FilterSelect";
import { ProcessBoard } from "@/components/ProcessBoard";
import { ProcessTable } from "@/components/ProcessTable";
import { StartProcessDialog } from "@/components/StartProcessDialog";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { ProcessStatus } from "@/process-types";
import { processStatuses, processStatusLabel } from "@/lib/status";
import { useProcesses, useWorkflows } from "@/queries";

type ViewMode = "table" | "board";

const statusOptions = processStatuses.map((s) => ({
  value: s,
  label: processStatusLabel[s],
}));

function isProcessStatus(value: string): value is ProcessStatus {
  return processStatuses.some((s) => s === value);
}

function isViewMode(value: string): value is ViewMode {
  return value === "table" || value === "board";
}

export function ProcessList() {
  const [status, setStatus] = useState("");
  const [workflow, setWorkflow] = useState("");
  const [project, setProject] = useState("");
  const [view, setView] = useState<ViewMode>("table");
  const deferredProject = useDeferredValue(project.trim());

  const filters = useMemo(
    () => ({
      status: isProcessStatus(status) ? status : undefined,
      workflow: workflow === "" ? undefined : workflow,
      project: deferredProject === "" ? undefined : deferredProject,
    }),
    [status, workflow, deferredProject],
  );

  const processes = useProcesses(filters);
  const workflows = useWorkflows();

  const workflowOptions = useMemo(
    () => (workflows.data ?? []).map((w) => ({ value: w.name, label: w.name })),
    [workflows.data],
  );
  const items = useMemo(
    () => processes.data?.pages.flatMap((p) => p.processes) ?? [],
    [processes.data],
  );

  function handleView(next: string) {
    if (isViewMode(next)) setView(next);
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex flex-col gap-1">
          <h1 className="text-2xl font-semibold tracking-tight">Processes</h1>
          <p className="text-sm text-muted-foreground">
            One process per issue and workflow. Auto-refreshes every 5s.
          </p>
        </div>
        <StartProcessDialog />
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <FilterSelect
          label="Status"
          allLabel="All statuses"
          value={status}
          options={statusOptions}
          onChange={setStatus}
          className="w-44"
        />
        <FilterSelect
          label="Workflow"
          allLabel="All workflows"
          value={workflow}
          options={workflowOptions}
          onChange={setWorkflow}
          className="w-52"
        />
        <Input
          aria-label="Project"
          placeholder="Project"
          value={project}
          onChange={(e) => {
            setProject(e.target.value);
          }}
          className="w-44"
        />
        <Tabs value={view} onValueChange={handleView} className="ml-auto">
          <TabsList>
            <TabsTrigger value="table">Table</TabsTrigger>
            <TabsTrigger value="board">Board</TabsTrigger>
          </TabsList>
        </Tabs>
      </div>

      {processes.error && (
        <Alert variant="destructive">
          <AlertDescription>{processes.error.message}</AlertDescription>
        </Alert>
      )}

      {processes.isPending && (
        <p className="text-sm text-muted-foreground">Loading processes...</p>
      )}

      {processes.isSuccess && items.length === 0 && (
        <p className="py-10 text-center text-sm text-muted-foreground">
          No processes found.
        </p>
      )}

      {items.length > 0 && view === "table" && (
        <Card>
          <CardContent className="p-0">
            <ProcessTable processes={items} />
          </CardContent>
        </Card>
      )}

      {items.length > 0 && view === "board" && (
        <ProcessBoard processes={items} />
      )}

      {processes.hasNextPage && (
        <div className="flex justify-center">
          <Button
            variant="outline"
            onClick={() => {
              void processes.fetchNextPage();
            }}
            disabled={processes.isFetchingNextPage}
          >
            Load more
          </Button>
        </div>
      )}
    </div>
  );
}
