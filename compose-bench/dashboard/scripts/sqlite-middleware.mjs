import { existsSync, readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { getJsonColumn, listRunsFromDb, openResultsDb } from "./sqlite-data.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, "..", "..");

function sendJson(res, body, status = 200) {
  const data = JSON.stringify(body);
  res.statusCode = status;
  res.setHeader("Content-Type", "application/json; charset=utf-8");
  res.setHeader("Cache-Control", "no-cache");
  res.end(data);
}

// Vite dev helper: if results/dfperf.db exists, serve /data from it, otherwise
// fall through to public/data (filled by npm run sync from results/).
export function dfperfSqlitePlugin(dbPath = process.env.DFPERF_DB || join(repo, "results", "dfperf.db")) {
  return {
    name: "dfperf-sqlite-data",
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const url = (req.url ?? "").split("?")[0];
        if (!url.startsWith("/data/")) return next();
        const db = openResultsDb(dbPath);
        if (!db) return next();
        try {
          if (url === "/data/index.json") {
            sendJson(res, listRunsFromDb(db));
            return;
          }
          const json = /^\/data\/runs\/([0-9a-z]{8,})\/(run|report|preflight)\.json$/.exec(url);
          if (json) {
            const column = json[2] === "run" ? "run_json" : json[2] === "report" ? "report_json" : "preflight_json";
            const doc = getJsonColumn(db, json[1], column);
            if (doc == null) {
              sendJson(res, { error: "not found" }, 404);
              return;
            }
            sendJson(res, doc);
            return;
          }
          const md = /^\/data\/runs\/([0-9a-z]{8,})\/report\.md$/.exec(url);
          if (md) {
            const file = join(repo, "results", md[1], "report.md");
            if (existsSync(file)) {
              res.statusCode = 200;
              res.setHeader("Content-Type", "text/markdown; charset=utf-8");
              res.setHeader("Cache-Control", "no-cache");
              res.end(readFileSync(file));
              return;
            }
          }
        } catch (err) {
          console.warn("dfperf-sqlite-data:", err);
        } finally {
          db.close();
        }
        next();
      });
    },
  };
}
