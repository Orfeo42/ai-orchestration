import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as api from "@/api";
import { makeDetail, makeStepRun } from "@/test/fixtures";
import { renderPage } from "@/test/render";
import type { StepRunStream } from "@/ws";
import { ProcessDetailPage } from "./ProcessDetail";

vi.mock("@/api");
vi.mock("@/ws");

import { useStepRunStream } from "@/ws";

const idleStream: StepRunStream = {
  stepRun: null,
  events: [],
  connected: false,
  error: null,
};

function renderDetail() {
  return renderPage(<ProcessDetailPage />, {
    route: "/processes/p1",
    path: "/processes/:id",
  });
}

describe("ProcessDetailPage", () => {
  beforeEach(() => {
    vi.mocked(useStepRunStream).mockReturnValue(idleStream);
    vi.mocked(api.listStepRunEvents).mockResolvedValue([]);
  });

  describe("header", () => {
    it("shows title, subject, workflow version and status", async () => {
      vi.mocked(api.getProcess).mockResolvedValue(
        makeDetail({ status: "succeeded" }),
      );

      renderDetail();

      expect(
        await screen.findByRole("heading", { name: "Fix the widget" }),
      ).toBeVisible();
      expect(screen.getByRole("link", { name: "acme/repo#7" })).toBeVisible();
      expect(
        screen.getByText(/implement \(version 3, abcdef12\)/),
      ).toBeVisible();
      expect(screen.getAllByText("Succeeded")).toHaveLength(2);
    });

    it("shows error and error kind", async () => {
      vi.mocked(api.getProcess).mockResolvedValue(
        makeDetail({
          status: "failed",
          error: "tests failed",
          error_kind: "business",
        }),
      );

      renderDetail();

      expect(await screen.findByRole("alert")).toHaveTextContent(
        "[business] tests failed",
      );
    });

    it("shows waiting info", async () => {
      vi.mocked(api.getProcess).mockResolvedValue(
        makeDetail({
          status: "waiting",
          waiting_for: {
            kind: "comment",
            step: "approve",
            since: "2026-10-01T10:00:00Z",
            resume_outcome: "commented",
          },
        }),
      );

      renderDetail();

      expect(
        await screen.findByText(/Waiting for comment at step approve/),
      ).toBeVisible();
    });

    it("shows the load error", async () => {
      vi.mocked(api.getProcess).mockRejectedValue(new Error("not found"));

      renderDetail();

      expect(await screen.findByRole("alert")).toHaveTextContent("not found");
    });
  });

  describe("actions", () => {
    it("offers cancel but not retry for a running process and cancels", async () => {
      const user = userEvent.setup();
      vi.mocked(api.getProcess).mockResolvedValue(
        makeDetail({ status: "running" }),
      );
      vi.mocked(api.cancelProcess).mockResolvedValue();
      renderDetail();

      await user.click(
        await screen.findByRole("button", { name: "Cancel process" }),
      );

      expect(api.cancelProcess).toHaveBeenCalledWith("p1");
      expect(
        screen.queryByRole("button", { name: "Retry" }),
      ).not.toBeInTheDocument();
    });

    it("offers retry but not cancel for a failed process and retries", async () => {
      const user = userEvent.setup();
      vi.mocked(api.getProcess).mockResolvedValue(
        makeDetail({ status: "failed" }, [makeStepRun({ status: "failed" })]),
      );
      vi.mocked(api.retryProcess).mockResolvedValue();
      renderDetail();

      await user.click(await screen.findByRole("button", { name: "Retry" }));

      expect(api.retryProcess).toHaveBeenCalledWith("p1", "");
      expect(
        screen.queryByRole("button", { name: "Cancel process" }),
      ).not.toBeInTheDocument();
    });

    it("retries from the chosen step", async () => {
      const user = userEvent.setup();
      vi.mocked(api.getProcess).mockResolvedValue(
        makeDetail({ status: "cancelled" }),
      );
      vi.mocked(api.retryProcess).mockResolvedValue();
      renderDetail();

      await user.click(
        await screen.findByRole("combobox", { name: "Retry from step" }),
      );
      await user.click(await screen.findByRole("option", { name: "approve" }));
      await user.click(screen.getByRole("button", { name: "Retry" }));

      expect(api.retryProcess).toHaveBeenCalledWith("p1", "approve");
    });

    it("shows no actions for a succeeded process", async () => {
      vi.mocked(api.getProcess).mockResolvedValue(
        makeDetail({ status: "succeeded" }),
      );
      renderDetail();
      await screen.findByRole("heading", { name: "Fix the widget" });

      expect(
        screen.queryByRole("button", { name: "Cancel process" }),
      ).not.toBeInTheDocument();
      expect(
        screen.queryByRole("button", { name: "Retry" }),
      ).not.toBeInTheDocument();
    });

    it("shows one signal button per outcome of the waiting step", async () => {
      const user = userEvent.setup();
      vi.mocked(api.getProcess).mockResolvedValue(
        makeDetail({
          status: "waiting",
          waiting_for: {
            kind: "signal",
            step: "approve",
            since: "2026-10-01T10:00:00Z",
            resume_outcome: "",
          },
        }),
      );
      vi.mocked(api.signalProcess).mockResolvedValue();
      renderDetail();

      await user.click(
        await screen.findByRole("button", { name: "Signal: approved" }),
      );

      expect(api.signalProcess).toHaveBeenCalledWith("p1", "approved");
      expect(
        screen.getByRole("button", { name: "Signal: rejected" }),
      ).toBeVisible();
    });

    it("shows a hint and no signal buttons when waiting for a comment", async () => {
      vi.mocked(api.getProcess).mockResolvedValue(
        makeDetail({
          status: "waiting",
          waiting_for: {
            kind: "comment",
            step: "approve",
            since: "2026-10-01T10:00:00Z",
            resume_outcome: "commented",
          },
        }),
      );
      renderDetail();

      expect(
        await screen.findByText(/Waiting for a comment on the issue/),
      ).toBeVisible();
      expect(
        screen.queryByRole("button", { name: /Signal:/ }),
      ).not.toBeInTheDocument();
    });

    it("shows the server error when an action fails", async () => {
      const user = userEvent.setup();
      vi.mocked(api.getProcess).mockResolvedValue(
        makeDetail({ status: "running" }),
      );
      vi.mocked(api.cancelProcess).mockRejectedValue(
        new Error("already terminal"),
      );
      renderDetail();

      await user.click(
        await screen.findByRole("button", { name: "Cancel process" }),
      );

      expect(await screen.findByRole("alert")).toHaveTextContent(
        "already terminal",
      );
    });
  });

  describe("step timeline", () => {
    const stepRuns = [
      makeStepRun({ id: "s1", step_id: "code", outcome: "ok" }),
      makeStepRun({
        id: "s2",
        step_id: "approve",
        uses: "human/approve",
        outcome: "approved",
      }),
    ];

    beforeEach(() => {
      vi.mocked(api.getProcess).mockResolvedValue(
        makeDetail({ status: "succeeded" }, stepRuns),
      );
      vi.mocked(api.listStepRunEvents).mockImplementation((id) =>
        Promise.resolve([
          {
            id: `e-${id}`,
            run_id: id,
            kind: "assistant_text",
            at: "2026-10-01T10:00:00Z",
            text: `events of ${id}`,
          },
        ]),
      );
    });

    it("lists step runs with their details", async () => {
      renderDetail();

      const table = await screen.findByRole("table");
      expect(within(table).getByRole("button", { name: "code" })).toBeVisible();
      expect(
        within(table).getByRole("button", { name: "approve" }),
      ).toBeVisible();
      expect(within(table).getByText("human/approve")).toBeVisible();
      expect(within(table).getByText("approved")).toBeVisible();
    });

    it("shows the events of the latest step run by default", async () => {
      renderDetail();

      expect(await screen.findByText("events of s2")).toBeVisible();
    });

    it("shows the events of the selected step run", async () => {
      const user = userEvent.setup();
      renderDetail();
      await screen.findByText("events of s2");

      await user.click(screen.getByRole("button", { name: "code" }));

      expect(await screen.findByText("events of s1")).toBeVisible();
      expect(screen.queryByText("events of s2")).not.toBeInTheDocument();
    });

    it("shows the step diff in the diff tab", async () => {
      const user = userEvent.setup();
      vi.mocked(api.getStepRunDiff).mockResolvedValue({
        pr_number: 4,
        files: [
          {
            path: "main.go",
            status: "modified",
            patch: "@@ -1 +1 @@\n-a\n+b",
            additions: 1,
            deletions: 1,
          },
        ],
      });
      renderDetail();
      await screen.findByText("events of s2");

      await user.click(screen.getByRole("tab", { name: "Diff" }));

      expect(await screen.findByText("main.go")).toBeVisible();
      expect(api.getStepRunDiff).toHaveBeenCalledWith("s2");
    });
  });

  describe("running step", () => {
    beforeEach(() => {
      vi.mocked(api.getProcess).mockResolvedValue(
        makeDetail({ status: "running" }, [
          makeStepRun({ id: "s1", status: "running", ended_at: undefined }),
        ]),
      );
      vi.mocked(useStepRunStream).mockReturnValue({
        ...idleStream,
        connected: true,
        events: [
          {
            id: "live1",
            run_id: "s1",
            kind: "assistant_text",
            at: "2026-10-01T10:00:00Z",
            text: "live event",
          },
        ],
      });
    });

    it("shows live events from the stream instead of fetching", async () => {
      renderDetail();

      expect(await screen.findByText("live event")).toBeVisible();
      expect(screen.getByText("live")).toBeVisible();
      expect(api.listStepRunEvents).not.toHaveBeenCalled();
      expect(useStepRunStream).toHaveBeenCalledWith("s1");
    });

    it("injects a note", async () => {
      const user = userEvent.setup();
      vi.mocked(api.injectNote).mockResolvedValue();
      renderDetail();

      await user.type(await screen.findByLabelText("Note"), "use table tests");
      await user.click(screen.getByRole("button", { name: "Inject" }));

      expect(api.injectNote).toHaveBeenCalledWith("s1", "use table tests");
    });

    it("stops the running step", async () => {
      const user = userEvent.setup();
      vi.mocked(api.cancelStepRun).mockResolvedValue();
      renderDetail();

      await user.click(
        await screen.findByRole("button", { name: "Stop step" }),
      );

      expect(api.cancelStepRun).toHaveBeenCalledWith("s1");
    });

    it("disables inject while the note is empty", async () => {
      renderDetail();

      expect(
        await screen.findByRole("button", { name: "Inject" }),
      ).toBeDisabled();
    });
  });

  describe("finished step", () => {
    it("has no inject or stop controls and does not open a stream", async () => {
      vi.mocked(api.getProcess).mockResolvedValue(
        makeDetail({ status: "succeeded" }),
      );
      renderDetail();
      await screen.findByRole("heading", { name: "Fix the widget" });

      expect(
        screen.queryByRole("button", { name: "Inject" }),
      ).not.toBeInTheDocument();
      expect(
        screen.queryByRole("button", { name: "Stop step" }),
      ).not.toBeInTheDocument();
      expect(useStepRunStream).toHaveBeenCalledWith(undefined);
    });
  });
});
