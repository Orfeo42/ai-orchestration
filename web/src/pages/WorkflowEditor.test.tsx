import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as api from "@/api";
import { makeCatalog, makeWorkflow } from "@/test/fixtures";
import { renderPage } from "@/test/render";
import { WorkflowEditorPage } from "./WorkflowEditor";

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    getCatalog: vi.fn(),
    listWorkflowVersions: vi.fn(),
    saveWorkflow: vi.fn(),
  };
});

function renderNew() {
  return renderPage(<WorkflowEditorPage />, {
    route: "/workflows/new",
    path: "/workflows/new",
  });
}

function renderEdit() {
  return renderPage(<WorkflowEditorPage />, {
    route: "/workflows/implement/edit",
    path: "/workflows/:name/edit",
  });
}

describe("WorkflowEditorPage", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    vi.mocked(api.getCatalog).mockResolvedValue(makeCatalog());
    vi.mocked(api.listWorkflowVersions).mockResolvedValue([makeWorkflow()]);
  });

  it("loads the existing workflow into the form", async () => {
    renderEdit();

    expect(await screen.findByLabelText("Name")).toHaveValue("implement");
    expect(screen.getByLabelText("Name")).toBeDisabled();
    expect(
      screen.getByLabelText("Description", { selector: "#wf-description" }),
    ).toHaveValue("Implement an issue");
    expect(screen.getByRole("group", { name: "Step code" })).toBeVisible();
    expect(screen.getByRole("group", { name: "Step approve" })).toBeVisible();
    const code = within(screen.getByRole("group", { name: "Step code" }));
    expect(code.getByRole("combobox", { name: "Uses" })).toHaveTextContent(
      "claude/implement",
    );
    expect(code.getByRole("combobox", { name: "On ok" })).toHaveTextContent(
      "approve",
    );
  });

  it("shows outcome selects after adding a step and choosing uses", async () => {
    const user = userEvent.setup();
    renderNew();
    await screen.findByLabelText("Name");

    await user.click(screen.getByRole("button", { name: "Add step" }));
    const step = within(screen.getByRole("group", { name: "Step step1" }));
    expect(step.queryByRole("combobox", { name: "On approved" })).toBeNull();

    await user.click(step.getByRole("combobox", { name: "Uses" }));
    await user.click(
      await screen.findByRole("option", { name: "human/approve" }),
    );

    expect(step.getByRole("combobox", { name: "On approved" })).toBeVisible();
    expect(step.getByRole("combobox", { name: "On rejected" })).toBeVisible();
    expect(step.getByText("Wait for approval")).toBeVisible();
  });

  it("saves the expected definition and navigates to the workflow", async () => {
    const user = userEvent.setup();
    vi.mocked(api.saveWorkflow).mockResolvedValue(makeWorkflow());
    renderNew();
    await screen.findByLabelText("Name");

    await user.type(screen.getByLabelText("Name"), "demo");
    await user.click(screen.getByRole("button", { name: "Add step" }));
    const step = within(screen.getByRole("group", { name: "Step step1" }));
    await user.click(step.getByRole("combobox", { name: "Uses" }));
    await user.click(
      await screen.findByRole("option", { name: "human/approve" }),
    );
    await user.click(step.getByRole("combobox", { name: "On approved" }));
    await user.click(await screen.findByRole("option", { name: "end" }));
    await user.click(step.getByRole("combobox", { name: "On rejected" }));
    await user.click(await screen.findByRole("option", { name: "fail" }));
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(api.saveWorkflow).toHaveBeenCalledWith({
      name: "demo",
      trigger: {},
      start: "step1",
      steps: {
        step1: {
          uses: "human/approve",
          on: { approved: "end", rejected: "fail" },
        },
      },
    });
    expect(await screen.findByText("workflow page")).toBeVisible();
  });

  it("builds a forgejo trigger", async () => {
    const user = userEvent.setup();
    vi.mocked(api.saveWorkflow).mockResolvedValue(makeWorkflow());
    renderNew();
    await screen.findByLabelText("Name");

    await user.type(screen.getByLabelText("Name"), "demo");
    await user.click(screen.getByRole("radio", { name: "forgejo" }));
    await user.type(
      screen.getByLabelText("Repos (comma separated)"),
      "a/b, c/d",
    );
    await user.type(screen.getByLabelText("Label"), "ai");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(api.saveWorkflow).toHaveBeenCalledWith(
      expect.objectContaining({
        trigger: { forgejo: { repos: ["a/b", "c/d"], label: "ai" } },
      }),
    );
  });

  it("renders the problems of a 422", async () => {
    const user = userEvent.setup();
    vi.mocked(api.saveWorkflow).mockRejectedValue(
      new api.ApiError(422, "invalid workflow", [
        "step code: unknown step",
        "step approve unreachable",
      ]),
    );
    renderEdit();
    await screen.findByLabelText("Name");

    await user.click(screen.getByRole("button", { name: "Save" }));

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("invalid workflow");
    expect(within(alert).getAllByRole("listitem")).toHaveLength(2);
    expect(within(alert).getByText("step approve unreachable")).toBeVisible();
  });

  it("disables save while a with field has invalid JSON", async () => {
    const user = userEvent.setup();
    renderEdit();
    await screen.findByLabelText("Name");
    const code = within(screen.getByRole("group", { name: "Step code" }));

    await user.click(code.getByLabelText("With (JSON)"));
    await user.paste("{");

    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });

  it("renames a step and keeps transitions pointing at it", async () => {
    const user = userEvent.setup();
    vi.mocked(api.saveWorkflow).mockResolvedValue(makeWorkflow());
    renderEdit();
    await screen.findByLabelText("Name");
    const approve = within(screen.getByRole("group", { name: "Step approve" }));

    await user.clear(approve.getByLabelText("Step id"));
    await user.type(approve.getByLabelText("Step id"), "review");
    await user.click(screen.getByRole("button", { name: "Save" }));

    const saved = vi.mocked(api.saveWorkflow).mock.calls[0]?.[0];
    expect(saved?.steps["code"]?.on["ok"]).toBe("review");
    expect(saved?.steps["review"]).toBeDefined();
  });

  it("shows an error when loading the catalog fails", async () => {
    vi.mocked(api.getCatalog).mockRejectedValue(new Error("catalog down"));

    renderNew();

    expect(await screen.findByRole("alert")).toHaveTextContent("catalog down");
  });
});
