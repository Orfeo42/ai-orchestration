import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { makeStepRun } from "@/test/fixtures";
import { MockWebSocket } from "@/test/mockWebSocket";
import { useStepRunStream } from "./ws";

function lastSocket(): MockWebSocket {
  const socket = MockWebSocket.instances.at(-1);
  if (!socket) throw new Error("no socket created");
  return socket;
}

describe("useStepRunStream", () => {
  beforeEach(() => {
    MockWebSocket.reset();
    vi.stubGlobal("WebSocket", MockWebSocket);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("does not connect without a step run id", () => {
    renderHook(() => useStepRunStream(undefined));

    expect(MockWebSocket.instances).toHaveLength(0);
  });

  it("connects to the step run websocket and reports connected", () => {
    const { result } = renderHook(() => useStepRunStream("s1"));

    expect(lastSocket().url).toMatch(/\/ws\/step-runs\/s1$/);
    expect(result.current.connected).toBe(false);

    act(() => {
      lastSocket().open();
    });

    expect(result.current.connected).toBe(true);
  });

  it("stores the step run from the hello frame", () => {
    const { result } = renderHook(() => useStepRunStream("s1"));

    act(() => {
      lastSocket().emit({ type: "hello", step_run: makeStepRun({ id: "s1" }) });
    });

    expect(result.current.stepRun?.id).toBe("s1");
    expect(result.current.events).toEqual([]);
  });

  it("appends events and drops a repeat of the last event id", () => {
    const { result } = renderHook(() => useStepRunStream("s1"));
    const event = {
      id: "e1",
      run_id: "s1",
      kind: "assistant_text",
      at: "2026-10-01T10:00:00Z",
      text: "hi",
    };

    act(() => {
      lastSocket().emit({ type: "event", event });
      lastSocket().emit({ type: "event", event });
      lastSocket().emit({ type: "event", event: { ...event, id: "e2" } });
    });

    expect(result.current.events.map((e) => e.id)).toEqual(["e1", "e2"]);
  });

  it("replaces the step run on run_update frames", () => {
    const { result } = renderHook(() => useStepRunStream("s1"));

    act(() => {
      lastSocket().emit({
        type: "hello",
        step_run: makeStepRun({ id: "s1", status: "running" }),
      });
      lastSocket().emit({
        type: "run_update",
        step_run: makeStepRun({ id: "s1", status: "succeeded" }),
      });
    });

    expect(result.current.stepRun?.status).toBe("succeeded");
  });

  it("exposes error frames", () => {
    const { result } = renderHook(() => useStepRunStream("s1"));

    act(() => {
      lastSocket().emit({ type: "error", error: "boom" });
    });

    expect(result.current.error).toBe("boom");
  });

  it("reports invalid frames without crashing", () => {
    const { result } = renderHook(() => useStepRunStream("s1"));

    act(() => {
      lastSocket().emitRaw("not json");
    });

    expect(result.current.error).toBe("invalid frame");
  });

  it("reconnects with backoff after an unexpected close", () => {
    vi.useFakeTimers();
    renderHook(() => useStepRunStream("s1"));

    act(() => {
      lastSocket().onclose?.();
    });
    expect(MockWebSocket.instances).toHaveLength(1);

    act(() => {
      vi.advanceTimersByTime(1000);
    });

    expect(MockWebSocket.instances).toHaveLength(2);
  });

  it("does not reconnect after a bye frame", () => {
    vi.useFakeTimers();
    renderHook(() => useStepRunStream("s1"));

    act(() => {
      lastSocket().emit({ type: "bye" });
    });
    act(() => {
      vi.advanceTimersByTime(60000);
    });

    expect(MockWebSocket.instances).toHaveLength(1);
  });

  it("closes the socket on unmount", () => {
    const { unmount } = renderHook(() => useStepRunStream("s1"));
    const socket = lastSocket();

    unmount();

    expect(socket.closed).toBe(true);
  });
});
