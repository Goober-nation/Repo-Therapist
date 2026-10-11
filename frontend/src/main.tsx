import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import "./index.css";
import { loadPublicConfig } from "./publicConfig";

// Runtime settings (GET /api/config): requested before the first render so they arrive early.
void loadPublicConfig();

const rootElement = document.getElementById("root");

if (!rootElement) {
  throw new Error("Не найден элемент #root для запуска приложения.");
}

createRoot(rootElement).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
