import { useEffect, useRef, useState } from "react";
import type { StepRun } from "./process-types";
import type { RunEvent } from "./types";

const FRAME_TYPES: readonly string[] = [
  "hello",
  "event",
  "run_update",
  "bye",
  "error",
];

interface WSFrame {
  type: "hello" | "event" | "run_update" | "bye" | "error";
  step_run?: StepRun;
  event?: RunEvent;
  last_seq?: number;
  error?: string;
}

export interface StepRunStream {
  stepRun: StepRun | null;
  events: RunEvent[];
  connected: boolean;
  error: string | null;
}

function isFrame(value: unknown): value is WSFrame {
  return (
    typeof value === "object" &&
    value !== null &&
    "type" in value &&
    typeof value.type === "string" &&
    FRAME_TYPES.includes(value.type)
  );
}

function wsUrl(stepRunId: string): string {
  const proto = window.location.protocol === "https:" ? "wss" : "ws";
  return `${proto}://${window.location.host}/ws/step-runs/${encodeURIComponent(stepRunId)}`;
}

function appendEvent(prev: RunEvent[], incoming: RunEvent): RunEvent[] {
  const last = prev.at(-1);
  if (last?.id !== undefined && last.id === incoming.id) return prev;
  return [...prev, incoming];
}

export function useStepRunStream(stepRunId: string | undefined): StepRunStream {
  const [stepRun, setStepRun] = useState<StepRun | null>(null);
  const [events, setEvents] = useState<RunEvent[]>([]);
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const reconnectAttempt = useRef(0);

  useEffect(() => {
    if (stepRunId === undefined) return;
    let ws: WebSocket | null = null;
    let cancelled = false;
    let finished = false;
    let reconnectTimer: number | null = null;

    function handleFrame(frame: WSFrame): void {
      switch (frame.type) {
        case "hello":
          if (frame.step_run) setStepRun(frame.step_run);
          setEvents([]);
          break;
        case "event": {
          const incoming = frame.event;
          if (incoming) setEvents((prev) => appendEvent(prev, incoming));
          break;
        }
        case "run_update":
          if (frame.step_run) setStepRun(frame.step_run);
          break;
        case "error":
          setError(frame.error ?? "unknown error");
          break;
        case "bye":
          finished = true;
          ws?.close();
          break;
      }
    }

    function connect(id: string): void {
      const socket = new WebSocket(wsUrl(id));
      ws = socket;

      socket.onopen = () => {
        setConnected(true);
        setError(null);
        reconnectAttempt.current = 0;
      };

      socket.onmessage = (evt: MessageEvent<unknown>) => {
        if (typeof evt.data !== "string") return;
        try {
          const parsed: unknown = JSON.parse(evt.data);
          if (isFrame(parsed)) handleFrame(parsed);
        } catch {
          setError("invalid frame");
        }
      };

      socket.onclose = () => {
        setConnected(false);
        if (cancelled || finished) return;
        reconnectAttempt.current += 1;
        const delay = Math.min(30000, 500 * 2 ** reconnectAttempt.current);
        reconnectTimer = window.setTimeout(() => {
          connect(id);
        }, delay);
      };

      socket.onerror = () => {
        setError("websocket error");
      };
    }

    connect(stepRunId);

    return () => {
      cancelled = true;
      if (reconnectTimer !== null) window.clearTimeout(reconnectTimer);
      ws?.close();
    };
  }, [stepRunId]);

  return { stepRun, events, connected, error };
}
