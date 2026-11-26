import "@fontsource-variable/inter/opsz.css";
import "@fontsource/commit-mono/400.css";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import { PrefsProvider } from "./prefs";
import "./styles.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <PrefsProvider>
      <App />
    </PrefsProvider>
  </StrictMode>,
);
