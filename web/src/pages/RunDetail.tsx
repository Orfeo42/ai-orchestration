import { useState } from "react";
import { useParams } from "react-router-dom";
import { Send, Square, RotateCw, Wifi, WifiOff } from "lucide-react";
import EventLog from "@/components/EventLog";
import { cancelRun, injectNote, retryRun } from "@/api";
import { useRunStream } from "@/ws";
import { phaseLabel, phaseVariant, isTerminal } from "@/lib/phase";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { cn } from "@/lib/utils";

function StatCard({
  label,
  value,
}: {
  label: string;
  value: string | number;
}) {
  return (
    <Card>
      <CardContent className="px-4 py-3">
        <div className="text-xs uppercase tracking-wide text-muted-foreground">
          {label}
        </div>
        <div className="mt-1 text-xl font-semibold tabular-nums">
          {typeof value === "number" ? value.toLocaleString() : value}
        </div>
      </CardContent>
    </Card>
  );
}

export default function RunDetail() {
  const { id } = useParams();
  const { run, events, connected, error } = useRunStream(id);
  const [note, setNote] = useState("");
  const [actionMsg, setActionMsg] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  if (!id) return <div>Missing run id.</div>;

  const totalIn = run?.usage.input ?? 0;
  const totalOut = run?.usage.output ?? 0;
  const totalCacheRead = run?.usage.cache_read_input ?? 0;
  const totalCacheCreate = run?.usage.cache_creation_input ?? 0;
  const terminal = isTerminal(run?.phase);
  const retryable = run?.phase === "failed" || run?.phase === "cancelled";

  async function onCancel() {
    if (!id) return;
    setBusy(true);
    try {
      await cancelRun(id);
      setActionMsg("cancel sent");
    } catch (e) {
      setActionMsg(String(e));
    } finally {
      setBusy(false);
    }
  }

  async function onRetry() {
    if (!id) return;
    setBusy(true);
    try {
      await retryRun(id);
      setActionMsg("retry enqueued");
    } catch (e) {
      setActionMsg(String(e));
    } finally {
      setBusy(false);
    }
  }

  async function onInject() {
    if (!id || !note.trim()) return;
    setBusy(true);
    try {
      await injectNote(id, note.trim());
      setActionMsg("note sent");
      setNote("");
    } catch (e) {
      setActionMsg(String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 space-y-2">
          <h1 className="truncate text-2xl font-semibold tracking-tight">
            {run?.title ?? id}
          </h1>
          {run && (
            <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
              <span className="capitalize">{run.service}</span>
              <span>·</span>
              <span className="font-mono text-xs">
                {run.repo_owner}/{run.repo_name}
              </span>
              {run.issue_number ? (
                <>
                  <span>·</span>
                  <span>#{run.issue_number}</span>
                </>
              ) : null}
              {run.pr_number ? (
                <>
                  <span>·</span>
                  <span>PR#{run.pr_number}</span>
                </>
              ) : null}
              <span>·</span>
              <Badge variant={phaseVariant[run.phase]}>
                {phaseLabel[run.phase]}
              </Badge>
              <span>·</span>
              <span>attempt {run.attempt}</span>
            </div>
          )}
        </div>
        <div className="flex items-center gap-2">
          <span
            className={cn(
              "inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1 text-xs font-medium",
              connected
                ? "border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
                : "border-muted-foreground/30 bg-muted text-muted-foreground",
            )}
          >
            {connected ? (
              <Wifi className="h-3.5 w-3.5" />
            ) : (
              <WifiOff className="h-3.5 w-3.5" />
            )}
            {connected ? "live" : "offline"}
          </span>
          <Button
            variant="outline"
            size="sm"
            onClick={onCancel}
            disabled={busy || terminal}
          >
            <Square className="h-3.5 w-3.5" />
            Stop
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={onRetry}
            disabled={busy || !retryable}
          >
            <RotateCw className="h-3.5 w-3.5" />
            Retry
          </Button>
        </div>
      </div>

      {error && (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {run && (
        <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
          <StatCard label="Input" value={totalIn} />
          <StatCard label="Output" value={totalOut} />
          <StatCard label="Cache read" value={totalCacheRead} />
          <StatCard label="Cache create" value={totalCacheCreate} />
        </div>
      )}

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">Inject instructions</CardTitle>
          <CardDescription>
            Pause Claude and resume with this note on the next loop iteration.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-2">
          <Textarea
            rows={2}
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder="Add instructions..."
            disabled={terminal}
          />
          <div className="flex items-center justify-between gap-3">
            <span className="text-xs text-muted-foreground">
              {actionMsg ?? " "}
            </span>
            <Button
              size="sm"
              onClick={onInject}
              disabled={busy || terminal || !note.trim()}
            >
              <Send className="h-3.5 w-3.5" />
              Inject
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">Activity</CardTitle>
          <CardDescription>
            Live stream of Claude/Gemini events for this run.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <EventLog events={events} />
        </CardContent>
      </Card>
    </div>
  );
}
