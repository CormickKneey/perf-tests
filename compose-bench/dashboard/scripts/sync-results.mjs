// Copies the real benchmark results (results/index.json and per-run
// run.json / report.json / report.md / preflight.json) plus the docs into
// public/data/ so the static dashboard can fetch them. Nothing is invented:
// if results/ is empty the dashboard shows its empty state.
import { cpSync, existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, "..", "..");
const results = process.env.DFPERF_RESULTS ? resolve(process.env.DFPERF_RESULTS) : join(repo, "results");
const out = join(here, "..", "public", "data");

rmSync(out, { recursive: true, force: true });
mkdirSync(join(out, "runs"), { recursive: true });
mkdirSync(join(out, "docs"), { recursive: true });

let index = { schema: "dfperf/v1", generated_at: null, runs: [] };
const indexPath = join(results, "index.json");
if (existsSync(indexPath)) {
  index = JSON.parse(readFileSync(indexPath, "utf8"));
}
let copied = 0;
for (const run of index.runs ?? []) {
  if (!/^[0-9a-z]{8,}$/.test(run.id)) continue;
  const src = join(results, run.id);
  const dst = join(out, "runs", run.id);
  mkdirSync(dst, { recursive: true });
  for (const f of ["run.json", "report.json", "report.md", "preflight.json"]) {
    if (existsSync(join(src, f))) {
      cpSync(join(src, f), join(dst, f));
      copied++;
    }
  }
}
writeFileSync(join(out, "index.json"), JSON.stringify(index, null, 2));
for (const [src, name] of [
  [join(repo, "docs", "compose.md"), "compose.md"],
  [join(repo, "docs", "results-store.md"), "results-store.md"],
]) {
  if (existsSync(src)) cpSync(src, join(out, "docs", name));
}
console.log(`sync-results: ${index.runs?.length ?? 0} runs, ${copied} files from ${results}`);
