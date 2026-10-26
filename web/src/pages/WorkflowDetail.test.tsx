import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import * as api from "@/api";
import { makeWorkflow } from "@/test/fixtures";
import { renderPage } from "@/test/render";
import { WorkflowDetailPage } from "./WorkflowDetail";

vi.mock("@/api");

const mermaidMock = vi.hoisted(() => ({
  initialize: vi.fn<(config: unknown) => void>(),
  render: vi.fn<(id: string, source: string) => Promise<{ svg: string }>>(),
}));

vi.mock("mermaid", () => ({ default: mermaidMock }));

function renderDetail() {
  return renderPage(<WorkflowDetailPage />, {
    route: "/workflows/implement",
    path: "/workflows/:name",
  });
}

describe("WorkflowDetailPage", () => {
  it("renders the graph from the latest definition and the yaml source", async () => {
    mermaidMock.render.mockResolvedValue({
      svg: "<svg><title>graph</title></svg>",
    });
    vi.mocked(api.listWorkflowVersions).mockResolvedValue([
      makeWorkflow({ id: 2, source_yaml: "old: yaml" }),
      makeWorkflow({ id: 3, source_yaml: "name: implement\nstart: code\n" }),
    ]);

    renderDetail();

    expect(
      await screen.findByRole("img", { name: "Workflow graph" }),
    ).toBeVisible();
    expect(mermaidMock.render).toHaveBeenCalledWith(
      expect.any(String),
      expect.stringContaining("flowchart TD"),
    );
    expect(screen.getByText(/name: implement/)).toBeVisible();
    expect(screen.queryByText("old: yaml")).not.toBeInTheDocument();
  });

  it("lists every version", async () => {
    mermaidMock.render.mockResolvedValue({ svg: "<svg></svg>" });
    vi.mocked(api.listWorkflowVersions).mockResolvedValue([
      makeWorkflow({ id: 2, hash: "aaaaaaaaaaaa1111" }),
      makeWorkflow({ id: 3, hash: "bbbbbbbbbbbb2222" }),
    ]);

    renderDetail();

    expect(await screen.findByText("aaaaaaaaaaaa")).toBeVisible();
    expect(screen.getByText("bbbbbbbbbbbb")).toBeVisible();
  });

  it("shows a message when the graph cannot be rendered", async () => {
    mermaidMock.render.mockRejectedValue(new Error("parse error"));
    vi.mocked(api.listWorkflowVersions).mockResolvedValue([makeWorkflow()]);

    renderDetail();

    expect(await screen.findByRole("alert")).toHaveTextContent("parse error");
  });

  it("shows an error when loading fails", async () => {
    vi.mocked(api.listWorkflowVersions).mockRejectedValue(
      new Error("not found"),
    );

    renderDetail();

    expect(await screen.findByRole("alert")).toHaveTextContent("not found");
  });

  it("links to the editor", async () => {
    vi.mocked(api.listWorkflowVersions).mockResolvedValue([makeWorkflow()]);
    mermaidMock.render.mockResolvedValue({ svg: "<svg></svg>" });

    renderDetail();

    expect(await screen.findByRole("link", { name: "Edit" })).toHaveAttribute(
      "href",
      "/workflows/implement/edit",
    );
  });

  it("deletes the workflow after confirmation and returns to the list", async () => {
    const user = userEvent.setup();
    mermaidMock.render.mockResolvedValue({ svg: "<svg></svg>" });
    vi.mocked(api.listWorkflowVersions).mockResolvedValue([makeWorkflow()]);
    vi.mocked(api.deleteWorkflow).mockResolvedValue();

    renderDetail();
    await user.click(await screen.findByRole("button", { name: "Delete" }));
    expect(api.deleteWorkflow).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Delete workflow" }));

    expect(api.deleteWorkflow).toHaveBeenCalledWith("implement");
    expect(await screen.findByText("workflow list page")).toBeVisible();
  });

  it("shows the error when deleting fails", async () => {
    const user = userEvent.setup();
    mermaidMock.render.mockResolvedValue({ svg: "<svg></svg>" });
    vi.mocked(api.listWorkflowVersions).mockResolvedValue([makeWorkflow()]);
    vi.mocked(api.deleteWorkflow).mockRejectedValue(new Error("in use"));

    renderDetail();
    await user.click(await screen.findByRole("button", { name: "Delete" }));
    await user.click(screen.getByRole("button", { name: "Delete workflow" }));

    expect(await screen.findByText("in use")).toBeVisible();
  });
});
