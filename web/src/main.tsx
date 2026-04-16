import React from "react";
import ReactDOM from "react-dom/client";
import { HashRouter, Route, Routes } from "react-router-dom";
import App from "./App";
import RunList from "./pages/RunList";
import RunDetail from "./pages/RunDetail";
import Tokens from "./pages/Tokens";
import "./styles.css";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <HashRouter>
      <Routes>
        <Route path="/" element={<App />}>
          <Route index element={<RunList />} />
          <Route path="runs/:id" element={<RunDetail />} />
          <Route path="tokens" element={<Tokens />} />
        </Route>
      </Routes>
    </HashRouter>
  </React.StrictMode>,
);
