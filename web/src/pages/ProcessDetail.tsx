import { useState } from "react";
import { useParams } from "react-router";
import { ProcessActions } from "@/components/ProcessActions";
import { StatCard } from "@/components/StatCard";
import { ProcessStatusBadge } from "@/components/StatusBadge";
import { StepRunPanel } from "@/components/StepRunPanel";
import { StepTimeline } from "@/components/StepTimeline";
import { SubjectLink } from "@/components/SubjectLink";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import type { ProcessDetail as ProcessDetailData } from "@/process-types";
import { formatCount, formatDateTime } from "@/lib/format";
import { useProcessDetail } from "@/queries";

type ViewProps = {
  id: string;
};

type HeaderProps = {
  detail: ProcessDetailData;
};

function ProcessHeader({ detail }: HeaderProps) {
  const { process, workflow } = detail;
  const waiting = process.waiting_for;
  return (
    <div className="flex flex-col gap-2">
      <h1 className="text-2xl font-semibold tracking-tight">{process.title}</h1>
      <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
        <SubjectLink subject={process.subject} />
        <span>·</span>
        <span>
          {process.workflow_name} (version {process.workflow_version_id},{" "}
          {workflow.hash.slice(0, 8)})
        </span>
        <span>·</span>
        <ProcessStatusBadge status={process.status} />
        {process.project !== undefined && (
          <>
            <span>·</span>
            <span>{process.project}</span>
          </>
        )}
      </div>
      {waiting && (
        <p className="text-sm text-muted-foreground">
          Waiting for {waiting.kind} at step {waiting.step} since{" "}
          {formatDateTime(waiting.since)}.
        </p>
      )}
      {process.error !== undefined && process.error !== "" && (
        <Alert variant="destructive">
          <AlertDescription>
            {process.error_kind !== undefined && process.error_kind !== ""
              ? `[${process.error_kind}] `
              : ""}
            {process.error}
          </AlertDescription>
        </Alert>
      )}
    </div>
  );
}

function ProcessDetailView({ id }: ViewProps) {
  const query = useProcessDetail(id);
  const [selectedId, setSelectedId] = useState<string | undefined>(undefined);

  if (query.isPending) {
    return <p className="text-sm text-muted-foreground">Loading process...</p>;
  }
  if (query.error) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{query.error.message}</AlertDescription>
      </Alert>
    );
  }

  const detail = query.data;
  const { process, step_runs: stepRuns } = detail;
  const selected =
    stepRuns.find((run) => run.id === selectedId) ?? stepRuns.at(-1);

  return (
    <div className="flex flex-col gap-5">
      <ProcessHeader detail={detail} />
      <ProcessActions detail={detail} />

      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <StatCard label="Input" value={formatCount(process.usage.input)} />
        <StatCard label="Output" value={formatCount(process.usage.output)} />
        <StatCard
          label="Cache read"
          value={formatCount(process.usage.cache_read_input)}
        />
        <StatCard
          label="Cache create"
          value={formatCount(process.usage.cache_creation_input)}
        />
      </div>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">Step timeline</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          <StepTimeline
            stepRuns={stepRuns}
            selectedId={selected?.id}
            onSelect={setSelectedId}
          />
        </CardContent>
      </Card>

      {selected && (
        <StepRunPanel key={selected.id} processId={id} stepRun={selected} />
      )}
    </div>
  );
}

export function ProcessDetailPage() {
  const { id } = useParams();
  if (id === undefined) return <p>Missing process id.</p>;
  return <ProcessDetailView id={id} />;
}
