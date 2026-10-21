import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as api from "@/api";
import type { StepStats, WorkflowStats } from "@/process-types";
import { usage } from "@/test/fixtures";
import { renderPage } from "@/test/render";
import { Analytics } from "./Analytics";

vi.mock("@/api");

const workflowStats: WorkflowStats = {
  workflow: "implement",
  processes: 10,
  active: 1,
  succeeded: 8,
  failed: 2,
  cancelled: 0,
  success_rate: 0.8,
  duration_p50_ms: 60_000,
  duration_p95_ms: 300_000,
  usage,
};

const stepStats: StepStats = {
  workflow: "implement",
  step_id: "code",
  uses: "claude/implement",
  runs: 12,
  outcomes: { ok: 9, error: 3 },
  failure_rate: 0.25,
  retries: 2,
  duration_p50_ms: 30_000,
  duration_p95_ms: 90_000,
  usage,
};

describe("Analytics", () => {
  beforeEach(() => {
    vi.mocked(api.getWorkflowStats).mockResolvedValue([workflowStats]);
    vi.mocked(api.getStepStats).mockResolvedValue([stepStats]);
    vi.mocked(api.getTokenPoints).mockResolvedValue([
      { bucket: "2026-10-01", usage },
    ]);
    vi.mocked(api.getFailures).mockResolvedValue([
      {
        error_kind: "upstream",
        error: "gateway timeout",
        count: 4,
        last_at: "2026-10-02T10:00:00Z",
      },
    ]);
  });

  it("queries the last 30 days by default", async () => {
    renderPage(<Analytics />);
    await screen.findByText("gateway timeout");

    const range = vi.mocked(api.getWorkflowStats).mock.calls[0]?.[0];
    expect(range?.from).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(range?.to).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(screen.getByLabelText("From")).toHaveValue(range?.from);
    expect(screen.getByLabelText("To")).toHaveValue(range?.to);
  });

  it("renders workflow stats", async () => {
    renderPage(<Analytics />);

    const row = (await screen.findAllByRole("row")).find((r) =>
      within(r).queryByText("80.0%"),
    );
    if (row === undefined) throw new Error("workflow row not found");
    expect(within(row).getByText("1m 0s")).toBeVisible();
    expect(within(row).getByText("5m 0s")).toBeVisible();
    expect(within(row).getByText("1,200 / 300")).toBeVisible();
  });

  it("renders step stats for the first workflow", async () => {
    renderPage(<Analytics />);

    expect(await screen.findByText("ok: 9, error: 3")).toBeVisible();
    expect(screen.getByText("25.0%")).toBeVisible();
    expect(vi.mocked(api.getStepStats).mock.calls[0]?.[0]).toBe("implement");
  });

  it("renders the failures table", async () => {
    renderPage(<Analytics />);

    expect(await screen.findByText("gateway timeout")).toBeVisible();
    expect(screen.getByText("upstream")).toBeVisible();
  });

  it("regroups tokens when the group changes", async () => {
    const user = userEvent.setup();
    renderPage(<Analytics />);
    await screen.findByText("gateway timeout");

    await user.click(screen.getByRole("combobox", { name: "Group by" }));
    await user.click(await screen.findByRole("option", { name: "Project" }));

    expect(api.getTokenPoints).toHaveBeenLastCalledWith(
      expect.any(Object),
      "project",
    );
  });

  it("refetches when the date range changes", async () => {
    const user = userEvent.setup();
    renderPage(<Analytics />);
    await screen.findByText("gateway timeout");

    const from = screen.getByLabelText("From");
    await user.clear(from);
    await user.type(from, "2026-09-01");

    expect(api.getWorkflowStats).toHaveBeenLastCalledWith(
      expect.objectContaining({ from: "2026-09-01" }),
    );
  });

  it("shows the API error message when a request fails", async () => {
    const { ApiError } = await vi.importActual<typeof import("@/api")>("@/api");
    vi.mocked(api.getFailures).mockRejectedValue(
      new ApiError(500, "analytics unavailable"),
    );

    renderPage(<Analytics />);

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "analytics unavailable",
    );
    expect(await screen.findByText("ok: 9, error: 3")).toBeVisible();
  });
});
