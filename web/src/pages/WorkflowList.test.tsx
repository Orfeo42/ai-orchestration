import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import * as api from "@/api";
import { makeWorkflow } from "@/test/fixtures";
import { renderPage } from "@/test/render";
import { WorkflowList } from "./WorkflowList";

vi.mock("@/api");

describe("WorkflowList", () => {
  it("lists the latest version of each workflow with a detail link", async () => {
    vi.mocked(api.listWorkflows).mockResolvedValue([makeWorkflow()]);

    renderPage(<WorkflowList />);

    expect(
      await screen.findByRole("link", { name: "implement" }),
    ).toHaveAttribute("href", "/workflows/implement");
    expect(screen.getByText("Implement an issue")).toBeVisible();
  });

  it("shows an error when loading fails", async () => {
    vi.mocked(api.listWorkflows).mockRejectedValue(new Error("boom"));

    renderPage(<WorkflowList />);

    expect(await screen.findByRole("alert")).toHaveTextContent("boom");
  });
});
