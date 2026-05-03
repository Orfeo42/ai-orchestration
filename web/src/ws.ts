import { useEffect, useRef, useState } from "react";
import type { Run, RunEvent } from "./types";

interface WSFrame {
  type: "hello" | "event" | "run_update" | "bye" | "error";
  run?: Run;
  event?: RunEvent;
  last_seq?: number;
  error?: string;
}

export interface RunStream {
  run: Run | null;
  events: RunEvent[];
  connected: boolean;
  error: string | null;
}

function wsUrl(runId: string): string {
  const proto = window.location.protocol === "https:" ? "wss" : "ws";
  return `${proto}://${window.location.host}/ws/runs/${runId}`;
}

export function useRunStream(runId: string | undefined): RunStream {
  const [run, setRun] = useState<Run | null>(null);
  const [events, setEvents] = useState<RunEvent[]>([]);
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const reconnectAttempt = useRef(0);

  useEffect(() => {
    if (!runId) return;
    let ws: WebSocket | null = null;
    let cancelled = false;
    let reconnectTimer: number | null = null;

    function connect() {
      ws = new WebSocket(wsUrl(runId!));

      ws.onopen = () => {
        setConnected(true);
        setError(null);
        reconnectAttempt.current = 0;
      };

      ws.onmessage = (evt) => {
        const frame = JSON.parse(evt.data) as WSFrame;
        switch (frame.type) {
          case "hello":
            if (frame.run) setRun(frame.run);
            setEvents([]);
            break;
          case "event":
            if (frame.event) {
              setEvents((prev) => {
                const incoming = frame.event!;
                if (
                  prev.length > 0 &&
                  incoming.id &&
                  prev[prev.length - 1].id === incoming.id
                ) {
                  return prev;
                }
                return [...prev, incoming];
              });
            }
            break;
          case "run_update":
            if (frame.run) {
              const next = frame.run;
              setRun((cur) => (runEqual(cur, next) ? cur : next));
            }
            break;
          case "error":
            setError(frame.error ?? "unknown error");
            break;
          case "bye":
            ws?.close();
            break;
        }
      };

      ws.onclose = () => {
        setConnected(false);
        if (cancelled) return;
        reconnectAttempt.current += 1;
        const delay = Math.min(30000, 500 * 2 ** reconnectAttempt.current);
        reconnectTimer = window.setTimeout(connect, delay);
      };

      ws.onerror = () => {
        setError("websocket error");
      };
    }

    connect();

    return () => {
      cancelled = true;
      if (reconnectTimer !== null) window.clearTimeout(reconnectTimer);
      ws?.close();
    };
  }, [runId]);

  return { run, events, connected, error };
}

function runEqual(a: Run | null, b: Run): boolean {
  if (!a) return false;
  return (
    a.id === b.id &&
    a.phase === b.phase &&
    a.attempt === b.attempt &&
    a.title === b.title &&
    a.pr_number === b.pr_number &&
    a.issue_number === b.issue_number &&
    a.usage.input === b.usage.input &&
    a.usage.output === b.usage.output &&
    a.usage.cache_read_input === b.usage.cache_read_input &&
    a.usage.cache_creation_input === b.usage.cache_creation_input
  );
}
