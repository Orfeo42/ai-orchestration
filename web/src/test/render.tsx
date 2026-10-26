import type { ReactElement } from "react";
import { render, type RenderResult } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import { ThemeProvider } from "@/lib/ThemeProvider";

type Options = {
  route?: string;
  path?: string;
};

export function renderPage(
  ui: ReactElement,
  { route = "/", path = "/" }: Options = {},
): RenderResult {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, refetchInterval: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <ThemeProvider>
        <MemoryRouter initialEntries={[route]}>
          <Routes>
            <Route path={path} element={ui} />
            <Route path="/processes/:id" element={<p>process page</p>} />
            <Route path="/workflows" element={<p>workflow list page</p>} />
            <Route path="/workflows/:name" element={<p>workflow page</p>} />
          </Routes>
        </MemoryRouter>
      </ThemeProvider>
    </QueryClientProvider>,
  );
}
