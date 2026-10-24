import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  deleteWorkflow,
  getCatalog,
  listPlugins,
  refreshPlugin,
  registerPlugin,
  removePlugin,
  saveWorkflow,
} from "./api";
import { makeWorkflow } from "./test/fixtures";

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("catalog and plugin api", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  describe("getCatalog", () => {
    it("defaults missing lists to empty", async () => {
      vi.stubGlobal(
        "fetch",
        vi
          .fn<typeof fetch>()
          .mockResolvedValue(jsonResponse({ steps: null, triggers: null })),
      );

      const catalog = await getCatalog();

      expect(catalog).toEqual({ steps: [], triggers: [] });
    });
  });

  describe("listPlugins", () => {
    it("returns an empty list for null", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn<typeof fetch>().mockResolvedValue(jsonResponse(null)),
      );

      expect(await listPlugins()).toEqual([]);
    });
  });

  describe("registerPlugin", () => {
    it("posts name and base_url", async () => {
      const fetchMock = vi
        .fn<typeof fetch>()
        .mockResolvedValue(jsonResponse({ name: "x" }, 201));
      vi.stubGlobal("fetch", fetchMock);

      await registerPlugin("x", "http://x");

      expect(fetchMock).toHaveBeenCalledWith(
        "/api/plugins",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({ name: "x", base_url: "http://x" }),
        }),
      );
    });

    it("throws ApiError with the server message", async () => {
      vi.stubGlobal(
        "fetch",
        vi
          .fn<typeof fetch>()
          .mockResolvedValue(jsonResponse({ error: "unreachable" }, 502)),
      );

      const err = await registerPlugin("x", "http://x").catch(
        (e: unknown) => e,
      );

      expect(err).toBeInstanceOf(ApiError);
      expect(err).toMatchObject({ status: 502, message: "unreachable" });
    });
  });

  describe("refreshPlugin", () => {
    it("posts to the refresh endpoint", async () => {
      const fetchMock = vi
        .fn<typeof fetch>()
        .mockResolvedValue(jsonResponse({ name: "a b" }));
      vi.stubGlobal("fetch", fetchMock);

      await refreshPlugin("a b");

      expect(fetchMock).toHaveBeenCalledWith("/api/plugins/a%20b/refresh", {
        method: "POST",
      });
    });
  });

  describe("removePlugin", () => {
    it("sends DELETE and resolves on 204", async () => {
      const fetchMock = vi
        .fn<typeof fetch>()
        .mockResolvedValue(new Response(null, { status: 204 }));
      vi.stubGlobal("fetch", fetchMock);

      await removePlugin("x");

      expect(fetchMock).toHaveBeenCalledWith("/api/plugins/x", {
        method: "DELETE",
      });
    });

    it("surfaces the 409 message", async () => {
      vi.stubGlobal(
        "fetch",
        vi
          .fn<typeof fetch>()
          .mockResolvedValue(jsonResponse({ error: "used by wf" }, 409)),
      );

      await expect(removePlugin("x")).rejects.toMatchObject({
        status: 409,
        message: "used by wf",
      });
    });
  });

  describe("saveWorkflow", () => {
    it("posts the definition", async () => {
      const fetchMock = vi
        .fn<typeof fetch>()
        .mockResolvedValue(jsonResponse(makeWorkflow()));
      vi.stubGlobal("fetch", fetchMock);
      const definition = makeWorkflow().definition;

      await saveWorkflow(definition);

      expect(fetchMock).toHaveBeenCalledWith(
        "/api/workflows",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify(definition),
        }),
      );
    });

    it("carries problems from a 422", async () => {
      vi.stubGlobal(
        "fetch",
        vi
          .fn<typeof fetch>()
          .mockResolvedValue(
            jsonResponse({ error: "invalid", problems: ["a", "b", 3] }, 422),
          ),
      );

      const err = await saveWorkflow(makeWorkflow().definition).catch(
        (e: unknown) => e,
      );

      expect(err).toBeInstanceOf(ApiError);
      expect(err).toMatchObject({ status: 422, problems: ["a", "b"] });
    });

    it("leaves problems unset for a 400", async () => {
      vi.stubGlobal(
        "fetch",
        vi
          .fn<typeof fetch>()
          .mockResolvedValue(jsonResponse({ error: "bad" }, 400)),
      );

      const err = await saveWorkflow(makeWorkflow().definition).catch(
        (e: unknown) => e,
      );

      expect(err).toMatchObject({ status: 400, message: "bad" });
      expect(err).toHaveProperty("problems", undefined);
    });
  });

  describe("deleteWorkflow", () => {
    it("sends DELETE to the encoded name", async () => {
      const fetchMock = vi
        .fn<typeof fetch>()
        .mockResolvedValue(new Response(null, { status: 204 }));
      vi.stubGlobal("fetch", fetchMock);

      await deleteWorkflow("my wf");

      expect(fetchMock).toHaveBeenCalledWith("/api/workflows/my%20wf", {
        method: "DELETE",
      });
    });
  });
});
