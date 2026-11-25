import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { dfperfSqlitePlugin } from "./scripts/sqlite-middleware.mjs";

export default defineConfig({
  base: "./",
  plugins: [react(), dfperfSqlitePlugin()],
  build: { outDir: "dist", sourcemap: false },
});
