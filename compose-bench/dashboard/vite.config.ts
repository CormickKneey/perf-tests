import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";
import { dfperfSqlitePlugin } from "./scripts/sqlite-middleware.mjs";

// VITE_* values are inlined into the public bundle. Refuse anything that is
// not an anon / publishable key so a secret never ships to the browser.
function assertBrowserSafeKey(env: Record<string, string>) {
  for (const name of ["VITE_SUPABASE_ANON_KEY", "VITE_SUPABASE_PUBLISHABLE_KEY"]) {
    const key = (env[name] ?? "").trim();
    if (!key) continue;
    let role = "";
    const parts = key.split(".");
    if (parts.length === 3) {
      try {
        role = JSON.parse(atob(parts[1].replace(/-/g, "+").replace(/_/g, "/"))).role ?? "";
      } catch {
        role = "";
      }
    }
    if (key.startsWith("sb_secret_") || (role && role !== "anon")) {
      throw new Error(`${name} looks like a secret or service_role key; use the anon / publishable key (sb_publishable_...)`);
    }
  }
}

export default defineConfig(({ mode }) => {
  assertBrowserSafeKey(loadEnv(mode, process.cwd(), "VITE_"));
  return {
    base: "./",
    plugins: [react(), dfperfSqlitePlugin()],
    build: { outDir: "dist", sourcemap: false },
  };
});
