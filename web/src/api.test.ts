import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  cancelProcess,
  getProcess,
  listProcesses,
  retryProcess,
  signalProcess,
  startProcess,
} from "./api";

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("api", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  describe("listProcesses", () => {
    it("builds the query string and skips empty values", async () => {
      const fetchMock = vi
        .fn<typeof fetch>()
        .mockResolvedValue(jsonResponse({ processes: [] }));
      vi.stubGlobal("fetch", fetchMock);

      await listProcesses({
        status: "failed",
        workflow: "implement",
        project: "",
        limit: 50,
        cursor: "abc",
      });

      expect(fetchMock).toHaveBeenCalledWith(
        "/api/processes?status=failed&workflow=implement&limit=50&cursor=abc",
        undefined,
      );
    });

    it("returns the parsed page", async () => {
      vi.stubGlobal(
        "fetch",
        vi
          .fn<typeof fetch>()
          .mockResolvedValue(
            jsonResponse({ processes: [], next_cursor: "next" }),
          ),
      );

      const page = await listProcesses({});

      expect(page.next_cursor).toBe("next");
    });
  });

  describe("error handling", () => {
    it("throws ApiError with status and server message", async () => {
      vi.stubGlobal(
        "fetch",
        vi
          .fn<typeof fetch>()
          .mockResolvedValue(jsonResponse({ error: "no such process" }, 404)),
      );

      const err = await getProcess("x").catch((e: unknown) => e);

      expect(err).toBeInstanceOf(ApiError);
      expect(err).toMatchObject({ status: 404, message: "no such process" });
    });

    it("falls back to status text when the body is not JSON", async () => {
      vi.stubGlobal(
        "fetch",
        vi
          .fn<typeof fetch>()
          .mockResolvedValue(
            new Response("oops", { status: 500, statusText: "Server Error" }),
          ),
      );

      const err = await getProcess("x").catch((e: unknown) => e);

      expect(err).toMatchObject({ status: 500, message: "500 Server Error" });
    });

    it("surfaces 409 conflicts from actions", async () => {
      vi.stubGlobal(
        "fetch",
        vi
          .fn<typeof fetch>()
          .mockResolvedValue(jsonResponse({ error: "already terminal" }, 409)),
      );

      const err = await cancelProcess("p1").catch((e: unknown) => e);

      expect(err).toMatchObject({ status: 409, message: "already terminal" });
    });
  });

  describe("startProcess", () => {
    it("posts workflow and subject key and returns the process", async () => {
      const fetchMock = vi
        .fn<typeof fetch>()
        .mockResolvedValue(jsonResponse({ id: "p9" }, 201));
      vi.stubGlobal("fetch", fetchMock);

      const created = await startProcess("implement", "acme/repo#7");

      expect(created.id).toBe("p9");
      expect(fetchMock).toHaveBeenCalledWith("/api/processes", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          workflow: "implement",
          subject_key: "acme/repo#7",
        }),
      });
    });
  });

  describe("retryProcess", () => {
    it("sends the step when given", async () => {
      const fetchMock = vi
        .fn<typeof fetch>()
        .mockResolvedValue(new Response(null, { status: 204 }));
      vi.stubGlobal("fetch", fetchMock);

      await retryProcess("p1", "review");

      expect(fetchMock).toHaveBeenCalledWith(
        "/api/processes/p1/retry",
        expect.objectContaining({ body: JSON.stringify({ step: "review" }) }),
      );
    });

    it("sends an empty body without a step", async () => {
      const fetchMock = vi
        .fn<typeof fetch>()
        .mockResolvedValue(new Response(null, { status: 204 }));
      vi.stubGlobal("fetch", fetchMock);

      await retryProcess("p1", "");

      expect(fetchMock).toHaveBeenCalledWith(
        "/api/processes/p1/retry",
        expect.objectContaining({ body: "{}" }),
      );
    });
  });

  describe("signalProcess", () => {
    it("posts the outcome", async () => {
      const fetchMock = vi
        .fn<typeof fetch>()
        .mockResolvedValue(new Response(null, { status: 204 }));
      vi.stubGlobal("fetch", fetchMock);

      await signalProcess("p1", "approved");

      expect(fetchMock).toHaveBeenCalledWith(
        "/api/processes/p1/signal",
        expect.objectContaining({
          body: JSON.stringify({ outcome: "approved" }),
        }),
      );
    });
  });
});
