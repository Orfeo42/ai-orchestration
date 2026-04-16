import type { RunEvent } from "../types";

interface Props {
  events: RunEvent[];
}

function formatInput(input?: string): string {
  if (!input) return "";
  try {
    return JSON.stringify(JSON.parse(input), null, 2);
  } catch {
    return input;
  }
}

export default function EventLog({ events }: Props) {
  if (events.length === 0) {
    return <div className="empty">Waiting for activity...</div>;
  }
  return (
    <ol className="event-log">
      {events.map((evt, i) => (
        <li key={`${evt.id ?? i}-${evt.seq ?? i}`} className={`event event-${evt.kind}`}>
          <span className="event-time">{new Date(evt.at).toLocaleTimeString()}</span>
          <span className="event-kind">{evt.kind}</span>
          {renderBody(evt)}
        </li>
      ))}
    </ol>
  );
}

function renderBody(evt: RunEvent) {
  switch (evt.kind) {
    case "tool_use":
      return (
        <details>
          <summary>{evt.tool_name}</summary>
          <pre>{formatInput(evt.tool_input)}</pre>
        </details>
      );
    case "tool_result":
      return (
        <details>
          <summary>result</summary>
          <pre>{evt.text}</pre>
        </details>
      );
    case "phase":
      return <span className="phase-badge">{evt.phase}</span>;
    case "result":
      if (evt.usage) {
        return (
          <span className="usage">
            in {evt.usage.input} · out {evt.usage.output}
            {evt.usage.cache_read_input
              ? ` · cache ${evt.usage.cache_read_input}`
              : ""}
          </span>
        );
      }
      return <span>{evt.result_text}</span>;
    case "inject":
      return <em>injected: {evt.text}</em>;
    case "cancel":
      return <em>cancelled by user</em>;
    case "init":
      return <code>session {evt.session_id}</code>;
    default:
      return <span>{evt.text}</span>;
  }
}
