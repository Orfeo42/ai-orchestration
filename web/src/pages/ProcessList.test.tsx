import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as api from "@/api";
import { makeProcess, makeWorkflow } from "@/test/fixtures";
import { renderPage } from "@/test/render";
import { ProcessList } from "./ProcessList";

vi.mock("@/api");

describe("ProcessList", () => {
  beforeEach(() => {
    vi.mocked(api.listWorkflows).mockResolvedValue([
      makeWorkflow(),
      makeWorkflow({ id: 4, name: "review" }),
    ]);
    vi.mocked(api.listProcesses).mockResolvedValue({
      processes: [
        makeProcess({ id: "p1", title: "Fix the widget", status: "running" }),
        makeProcess({
          id: "p2",
          title: "Broken build",
          status: "failed",
          project: undefined,
          current_step: undefined,
        }),
      ],
    });
  });

  it("renders a row per process with a subject link", async () => {
    renderPage(<ProcessList />);

    expect(
      await screen.findByRole("link", { name: "Fix the widget" }),
    ).toHaveAttribute("href", "/processes/p1");
    expect(
      screen.getAllByRole("link", { name: "acme/repo#7" })[0],
    ).toHaveAttribute("href", "https://git.example/acme/repo/issues/7");
    expect(screen.getByRole("link", { name: "Broken build" })).toBeVisible();
    expect(screen.getAllByText("1,200 / 300", { selector: "td" })).toHaveLength(
      2,
    );
  });

  it("shows an error when loading fails", async () => {
    vi.mocked(api.listProcesses).mockRejectedValue(new Error("db down"));

    renderPage(<ProcessList />);

    expect(await screen.findByRole("alert")).toHaveTextContent("db down");
  });

  it("filters by status", async () => {
    const user = userEvent.setup();
    renderPage(<ProcessList />);
    await screen.findByRole("link", { name: "Fix the widget" });

    await user.click(screen.getByRole("combobox", { name: "Status" }));
    await user.click(await screen.findByRole("option", { name: "Failed" }));

    expect(api.listProcesses).toHaveBeenLastCalledWith(
      expect.objectContaining({ status: "failed" }),
    );
  });

  it("filters by workflow", async () => {
    const user = userEvent.setup();
    renderPage(<ProcessList />);
    await screen.findByRole("link", { name: "Fix the widget" });

    await user.click(screen.getByRole("combobox", { name: "Workflow" }));
    await user.click(await screen.findByRole("option", { name: "review" }));

    expect(api.listProcesses).toHaveBeenLastCalledWith(
      expect.objectContaining({ workflow: "review" }),
    );
  });

  it("filters by project", async () => {
    const user = userEvent.setup();
    renderPage(<ProcessList />);
    await screen.findByRole("link", { name: "Fix the widget" });

    await user.type(
      screen.getByRole("textbox", { name: "Project" }),
      "billing",
    );

    expect(api.listProcesses).toHaveBeenLastCalledWith(
      expect.objectContaining({ project: "billing" }),
    );
  });

  it("loads the next page with the cursor", async () => {
    const user = userEvent.setup();
    vi.mocked(api.listProcesses).mockImplementation((params) =>
      Promise.resolve(
        params.cursor === "c1"
          ? { processes: [makeProcess({ id: "p3", title: "Second page" })] }
          : {
              processes: [makeProcess({ id: "p1", title: "First page" })],
              next_cursor: "c1",
            },
      ),
    );
    renderPage(<ProcessList />);
    await screen.findByRole("link", { name: "First page" });

    await user.click(screen.getByRole("button", { name: "Load more" }));

    expect(
      await screen.findByRole("link", { name: "Second page" }),
    ).toBeVisible();
    expect(screen.getByRole("link", { name: "First page" })).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Load more" }),
    ).not.toBeInTheDocument();
  });

  it("hides load more when there is no next cursor", async () => {
    renderPage(<ProcessList />);
    await screen.findByRole("link", { name: "Fix the widget" });

    expect(
      screen.queryByRole("button", { name: "Load more" }),
    ).not.toBeInTheDocument();
  });

  it("switches to the board grouped by status", async () => {
    const user = userEvent.setup();
    renderPage(<ProcessList />);
    await screen.findByRole("link", { name: "Fix the widget" });

    await user.click(screen.getByRole("tab", { name: "Board" }));

    const failed = screen.getByRole("region", { name: "Failed column" });
    expect(
      within(failed).getByRole("link", { name: "Broken build" }),
    ).toBeVisible();
    expect(
      within(failed).queryByRole("link", { name: "Fix the widget" }),
    ).not.toBeInTheDocument();
    const running = screen.getByRole("region", { name: "Running column" });
    expect(
      within(running).getByRole("link", { name: "Fix the widget" }),
    ).toBeVisible();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  describe("start dialog", () => {
    it("submits the workflow and subject key then opens the process", async () => {
      const user = userEvent.setup();
      vi.mocked(api.startProcess).mockResolvedValue(makeProcess({ id: "p9" }));
      renderPage(<ProcessList />);
      await screen.findByRole("link", { name: "Fix the widget" });

      await user.click(screen.getByRole("button", { name: "Start process" }));
      const dialog = await screen.findByRole("dialog");
      await user.click(
        within(dialog).getByRole("combobox", { name: "Workflow to run" }),
      );
      await user.click(await screen.findByRole("option", { name: "review" }));
      await user.type(
        within(dialog).getByLabelText("Subject key"),
        "acme/repo#12",
      );
      await user.click(within(dialog).getByRole("button", { name: "Start" }));

      expect(api.startProcess).toHaveBeenCalledWith("review", "acme/repo#12");
      expect(await screen.findByText("process page")).toBeVisible();
    });

    it("disables submit until both fields are filled", async () => {
      const user = userEvent.setup();
      renderPage(<ProcessList />);

      await user.click(screen.getByRole("button", { name: "Start process" }));
      const dialog = await screen.findByRole("dialog");

      expect(
        within(dialog).getByRole("button", { name: "Start" }),
      ).toBeDisabled();
    });

    it("shows the server error and stays open", async () => {
      const user = userEvent.setup();
      vi.mocked(api.startProcess).mockRejectedValue(
        new Error("process already exists"),
      );
      renderPage(<ProcessList />);

      await user.click(screen.getByRole("button", { name: "Start process" }));
      const dialog = await screen.findByRole("dialog");
      await user.click(
        within(dialog).getByRole("combobox", { name: "Workflow to run" }),
      );
      await user.click(
        await screen.findByRole("option", { name: "implement" }),
      );
      await user.type(within(dialog).getByLabelText("Subject key"), "a/b#1");
      await user.click(within(dialog).getByRole("button", { name: "Start" }));

      expect(await within(dialog).findByRole("alert")).toHaveTextContent(
        "process already exists",
      );
    });
  });
});
