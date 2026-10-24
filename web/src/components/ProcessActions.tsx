import { useState } from "react";
import { RotateCw, Square } from "lucide-react";
import { FilterSelect } from "@/components/FilterSelect";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import type { ProcessDetail } from "@/process-types";
import { isProcessRetryable, isProcessTerminal } from "@/lib/status";
import { useCancelProcess, useRetryProcess, useSignalProcess } from "@/queries";

type Props = {
  detail: ProcessDetail;
};

export function ProcessActions({ detail }: Props) {
  const { process, workflow } = detail;
  const cancel = useCancelProcess(process.id);
  const retry = useRetryProcess(process.id);
  const signal = useSignalProcess(process.id);
  const [retryStep, setRetryStep] = useState("");

  const stepOptions = Object.keys(workflow.definition.steps).map((id) => ({
    value: id,
    label: id,
  }));
  const waiting = process.waiting_for;
  const signalOutcomes =
    waiting?.kind === "signal"
      ? Object.keys(workflow.definition.steps[waiting.step]?.on ?? {})
      : [];
  const busy = cancel.isPending || retry.isPending || signal.isPending;
  const failure = cancel.error ?? retry.error ?? signal.error;

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        {!isProcessTerminal(process.status) && (
          <Button
            variant="outline"
            size="sm"
            disabled={busy}
            onClick={() => {
              cancel.mutate();
            }}
          >
            <Square className="h-3.5 w-3.5" />
            Cancel process
          </Button>
        )}
        {isProcessRetryable(process.status) && (
          <>
            <FilterSelect
              label="Retry from step"
              allLabel="Default step"
              value={retryStep}
              options={stepOptions}
              onChange={setRetryStep}
              className="w-48"
            />
            <Button
              variant="outline"
              size="sm"
              disabled={busy}
              onClick={() => {
                retry.mutate(retryStep);
              }}
            >
              <RotateCw className="h-3.5 w-3.5" />
              Retry
            </Button>
          </>
        )}
        {signalOutcomes.map((outcome) => (
          <Button
            key={outcome}
            size="sm"
            disabled={busy}
            onClick={() => {
              signal.mutate(outcome);
            }}
          >
            Signal: {outcome}
          </Button>
        ))}
      </div>
      {waiting?.kind === "comment" && (
        <p className="text-sm text-muted-foreground">
          Waiting for a comment on the issue to resume step {waiting.step}.
        </p>
      )}
      {failure && (
        <Alert variant="destructive">
          <AlertDescription>{failure.message}</AlertDescription>
        </Alert>
      )}
    </div>
  );
}
