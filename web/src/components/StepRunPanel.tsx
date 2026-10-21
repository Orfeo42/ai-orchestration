import { useState } from "react";
import { Send, Square, Wifi, WifiOff } from "lucide-react";
import { DiffView } from "@/components/DiffView";
import { EventLog } from "@/components/EventLog";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import type { StepRun } from "@/process-types";
import { useCancelStepRun, useInjectNote, useStepRunEvents } from "@/queries";
import { cn } from "@/lib/utils";
import { useStepRunStream } from "@/ws";

type Props = {
  processId: string;
  stepRun: StepRun;
};

export function StepRunPanel({ processId, stepRun }: Props) {
  const running = stepRun.status === "running";
  const stream = useStepRunStream(running ? stepRun.id : undefined);
  const fetched = useStepRunEvents(stepRun.id, !running);
  const cancel = useCancelStepRun(processId, stepRun.id);
  const inject = useInjectNote(stepRun.id);
  const [note, setNote] = useState("");

  const events = running ? stream.events : (fetched.data ?? []);
  const failure = stream.error ?? fetched.error?.message;
  const actionError = cancel.error ?? inject.error;
  const trimmed = note.trim();

  function handleInject() {
    inject.mutate(trimmed, {
      onSuccess: () => {
        setNote("");
      },
    });
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-lg font-semibold">
          {stepRun.step_id}{" "}
          <span className="text-sm font-normal text-muted-foreground">
            attempt {stepRun.attempt}, iteration {stepRun.iteration}
          </span>
        </h2>
        {running && (
          <div className="flex items-center gap-2">
            <span
              className={cn(
                "inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1 text-xs font-medium",
                stream.connected
                  ? "border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
                  : "border-muted-foreground/30 bg-muted text-muted-foreground",
              )}
            >
              {stream.connected ? (
                <Wifi className="h-3.5 w-3.5" />
              ) : (
                <WifiOff className="h-3.5 w-3.5" />
              )}
              {stream.connected ? "live" : "offline"}
            </span>
            <Button
              variant="outline"
              size="sm"
              disabled={cancel.isPending}
              onClick={() => {
                cancel.mutate();
              }}
            >
              <Square className="h-3.5 w-3.5" />
              Stop step
            </Button>
          </div>
        )}
      </div>

      {stepRun.error && (
        <Alert variant="destructive">
          <AlertDescription>{stepRun.error}</AlertDescription>
        </Alert>
      )}
      {failure && (
        <Alert variant="destructive">
          <AlertDescription>{failure}</AlertDescription>
        </Alert>
      )}
      {actionError && (
        <Alert variant="destructive">
          <AlertDescription>{actionError.message}</AlertDescription>
        </Alert>
      )}

      {running && (
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">Inject instructions</CardTitle>
            <CardDescription>
              Pause Claude and resume with this note on the next loop iteration.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-2">
            <Label htmlFor="inject-note">Note</Label>
            <Textarea
              id="inject-note"
              rows={2}
              value={note}
              onChange={(e) => {
                setNote(e.target.value);
              }}
              placeholder="Add instructions..."
            />
            <div className="flex justify-end">
              <Button
                size="sm"
                onClick={handleInject}
                disabled={inject.isPending || trimmed === ""}
              >
                <Send className="h-3.5 w-3.5" />
                Inject
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      <Tabs defaultValue="activity" className="w-full">
        <TabsList>
          <TabsTrigger value="activity">Activity</TabsTrigger>
          <TabsTrigger value="diff">Diff</TabsTrigger>
        </TabsList>
        <TabsContent value="activity">
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-base">Activity</CardTitle>
              <CardDescription>
                Events emitted by this step run.
              </CardDescription>
            </CardHeader>
            <CardContent>
              <EventLog events={events} />
            </CardContent>
          </Card>
        </TabsContent>
        <TabsContent value="diff">
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-base">Modified files</CardTitle>
              <CardDescription>
                File changes from the PR opened by this step.
              </CardDescription>
            </CardHeader>
            <CardContent>
              <DiffView stepRunId={stepRun.id} />
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}
