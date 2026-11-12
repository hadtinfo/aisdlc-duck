import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { App } from "./App";
import { initialLocale, setLocale } from "./i18n";
import "./styles.css";

// Ngôn ngữ trước khi biết người dùng: localStorage → navigator.language; lựa chọn trên server áp dụng khi GET /api/me về.
setLocale(initialLocale());

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <BrowserRouter>
      <App />
    </BrowserRouter>
  </StrictMode>,
);
