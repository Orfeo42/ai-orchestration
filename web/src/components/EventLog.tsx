import type { RunEvent } from "@/types";
import { Badge } from "@/components/ui/badge";
import { Markdown } from "@/components/Markdown";
import { cn } from "@/lib/utils";

type Props = {
  events: RunEvent[];
};

function formatInput(input?: string): string {
  if (!input) return "";
  try {
    return JSON.stringify(JSON.parse(input), null, 2);
  } catch {
    return input;
  }
}

const kindColor: Record<string, string> = {
  tool_use: "text-sky-500",
  tool_result: "text-emerald-500",
  thinking: "text-violet-500",
  assistant_text: "text-foreground",
  result: "text-amber-500",
  phase: "text-primary",
  cancel: "text-destructive",
  inject: "text-fuchsia-500",
  error: "text-destructive",
  init: "text-muted-foreground",
  run_start: "text-muted-foreground",
  run_end: "text-muted-foreground",
};

export function EventLog({ events }: Props) {
  if (events.length === 0) {
    return (
      <div className="rounded-md border border-dashed py-10 text-center text-sm text-muted-foreground">
        Waiting for activity...
      </div>
    );
  }
  return (
    <ol className="flex flex-col divide-y rounded-md border">
      {events.map((evt) => (
        <li
          key={`${evt.id ?? ""}-${String(evt.seq ?? "")}-${evt.at}-${evt.kind}`}
          className="flex items-start gap-3 px-4 py-2.5 text-sm"
        >
          <span className="w-20 shrink-0 font-mono text-xs text-muted-foreground">
            {new Date(evt.at).toLocaleTimeString()}
          </span>
          <span
            className={cn(
              "w-28 shrink-0 font-mono text-xs uppercase tracking-wide",
              kindColor[evt.kind] ?? "text-muted-foreground",
            )}
          >
            {evt.kind}
          </span>
          <div className="min-w-0 flex-1">{renderBody(evt)}</div>
        </li>
      ))}
    </ol>
  );
}

function renderBody(evt: RunEvent) {
  switch (evt.kind) {
    case "tool_use":
      return (
        <details className="group">
          <summary className="cursor-pointer select-none font-medium hover:text-foreground/80">
            {evt.tool_name}
          </summary>
          <pre className="mt-2 max-h-[60vh] overflow-auto whitespace-pre-wrap break-words rounded-md bg-muted p-3 text-xs">
            {formatInput(evt.tool_input)}
          </pre>
        </details>
      );
    case "tool_result":
      return (
        <details className="group">
          <summary className="cursor-pointer select-none text-muted-foreground hover:text-foreground/80">
            result ({(evt.text ?? "").length} chars)
          </summary>
          <pre className="mt-2 max-h-[60vh] overflow-auto whitespace-pre-wrap break-words rounded-md bg-muted p-3 text-xs">
            {evt.text}
          </pre>
        </details>
      );
    case "phase":
      return <Badge variant="secondary">{evt.phase}</Badge>;
    case "result":
      if (evt.usage) {
        return (
          <span className="font-mono text-xs text-muted-foreground">
            in {String(evt.usage.input)} · out {String(evt.usage.output)}
            {evt.usage.cache_read_input
              ? ` · cache ${String(evt.usage.cache_read_input)}`
              : ""}
          </span>
        );
      }
      return <Markdown text={evt.result_text ?? ""} />;
    case "inject":
      return (
        <div className="text-muted-foreground">
          <em className="mr-1">injected:</em>
          <Markdown text={evt.text ?? ""} className="inline" />
        </div>
      );
    case "cancel":
      return <em className="text-destructive">cancelled by user</em>;
    case "init":
      return (
        <code className="rounded bg-muted px-1.5 py-0.5 text-xs">
          session {evt.session_id}
        </code>
      );
    case "assistant_text":
    case "thinking":
    default:
      return <Markdown text={evt.text ?? ""} />;
  }
}
