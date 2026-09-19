import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import "./index.css";

const rootElement = document.getElementById("root");

if (!rootElement) {
  throw new Error("Не найден элемент #root для запуска приложения.");
}

createRoot(rootElement).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
