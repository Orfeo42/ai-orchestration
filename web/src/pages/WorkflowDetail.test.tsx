import { screen } from "@testing-library/react";
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
});
