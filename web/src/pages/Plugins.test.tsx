import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as api from "@/api";
import { makePlugin } from "@/test/fixtures";
import { renderPage } from "@/test/render";
import { Plugins } from "./Plugins";

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    listPlugins: vi.fn(),
    registerPlugin: vi.fn(),
    refreshPlugin: vi.fn(),
    removePlugin: vi.fn(),
  };
});

describe("Plugins", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    vi.mocked(api.listPlugins).mockResolvedValue([
      makePlugin(),
      makePlugin({
        name: "memory",
        base_url: "http://memory:8080",
        status: "down",
        error: "connection refused",
      }),
    ]);
  });

  it("renders a row per plugin with status, counts and error", async () => {
    renderPage(<Plugins />);

    const row = (await screen.findByText("http://memory:8080")).closest("tr");
    expect(row).not.toBeNull();
    const cells = within(row as HTMLElement);
    expect(cells.getByText("down")).toBeVisible();
    expect(cells.getByText("connection refused")).toBeVisible();
    expect(screen.getByText("http://forgejo:8080")).toBeVisible();
    expect(screen.getByText("ok")).toBeVisible();
  });

  it("shows actions and triggers when a row is expanded", async () => {
    const user = userEvent.setup();
    renderPage(<Plugins />);

    await user.click(
      await screen.findByRole("button", { name: "Details for forgejo" }),
    );

    expect(screen.getByText("Post a comment")).toBeVisible();
    expect(screen.getByText("Issue labeled")).toBeVisible();
  });

  it("registers a plugin and clears the form", async () => {
    const user = userEvent.setup();
    vi.mocked(api.registerPlugin).mockResolvedValue(makePlugin());
    renderPage(<Plugins />);
    await screen.findByText("http://memory:8080");

    await user.type(screen.getByLabelText("Name"), "notify");
    await user.type(screen.getByLabelText("Base URL"), "http://notify:8080");
    await user.click(screen.getByRole("button", { name: "Register" }));

    expect(api.registerPlugin).toHaveBeenCalledWith(
      "notify",
      "http://notify:8080",
    );
    expect(await screen.findByLabelText("Name")).toHaveValue("");
  });

  it("shows the API error when registering fails", async () => {
    const user = userEvent.setup();
    vi.mocked(api.registerPlugin).mockRejectedValue(
      new api.ApiError(502, "plugin unreachable"),
    );
    renderPage(<Plugins />);
    await screen.findByText("http://memory:8080");

    await user.type(screen.getByLabelText("Name"), "notify");
    await user.type(screen.getByLabelText("Base URL"), "http://bad");
    await user.click(screen.getByRole("button", { name: "Register" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "plugin unreachable",
    );
  });

  it("refreshes a plugin", async () => {
    const user = userEvent.setup();
    vi.mocked(api.refreshPlugin).mockResolvedValue(makePlugin());
    renderPage(<Plugins />);

    await user.click(
      await screen.findByRole("button", { name: "Refresh forgejo" }),
    );

    expect(api.refreshPlugin).toHaveBeenCalledWith("forgejo");
  });

  it("removes a plugin after confirmation", async () => {
    const user = userEvent.setup();
    vi.mocked(api.removePlugin).mockResolvedValue();
    renderPage(<Plugins />);

    await user.click(
      await screen.findByRole("button", { name: "Remove forgejo" }),
    );
    await user.click(screen.getByRole("button", { name: "Remove plugin" }));

    expect(api.removePlugin).toHaveBeenCalledWith("forgejo");
  });

  it("shows the 409 message when the plugin is in use", async () => {
    const user = userEvent.setup();
    vi.mocked(api.removePlugin).mockRejectedValue(
      new api.ApiError(409, "plugin used by workflow implement"),
    );
    renderPage(<Plugins />);

    await user.click(
      await screen.findByRole("button", { name: "Remove forgejo" }),
    );
    await user.click(screen.getByRole("button", { name: "Remove plugin" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "plugin used by workflow implement",
    );
  });
});
