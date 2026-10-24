import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { HashRouter, Route, Routes } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App } from "./App";
import { Analytics } from "./pages/Analytics";
import { ProcessDetailPage } from "./pages/ProcessDetail";
import { ProcessList } from "./pages/ProcessList";
import { WorkflowDetailPage } from "./pages/WorkflowDetail";
import { WorkflowList } from "./pages/WorkflowList";
import { ThemeProvider } from "./lib/ThemeProvider";
import "./styles.css";

const queryClient = new QueryClient();
const container = document.getElementById("root");
if (container === null) throw new Error("root element not found");

createRoot(container).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <HashRouter>
          <Routes>
            <Route path="/" element={<App />}>
              <Route index element={<ProcessList />} />
              <Route path="processes/:id" element={<ProcessDetailPage />} />
              <Route path="workflows" element={<WorkflowList />} />
              <Route path="workflows/:name" element={<WorkflowDetailPage />} />
              <Route path="analytics" element={<Analytics />} />
            </Route>
          </Routes>
        </HashRouter>
      </ThemeProvider>
    </QueryClientProvider>
  </StrictMode>,
);
