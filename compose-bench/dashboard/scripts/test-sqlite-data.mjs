// Verify the dashboard can list a published run from results/dfperf.db (or $DFPERF_DB).
// When no db is present, ingest a committed sample run into a temporary SQLite file.
import { join, resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { mkdtempSync, existsSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { spawnSync } from "node:child_process";
import { getJsonColumn, listRunsFromDb, openResultsDb } from "./sqlite-data.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, "..", "..");
const want = process.env.DFPERF_TEST_RUN_ID || "20261121t025924cae6";
const givenDb = process.env.DFPERF_DB || join(repo, "results", "dfperf.db");

let dbPath = givenDb;
let cleanup = null;
if (!existsSync(dbPath)) {
  const dir = mkdtempSync(join(tmpdir(), "dfperf-test-"));
  dbPath = join(dir, "dfperf.db");
  cleanup = dir;
  const runDir = join(repo, "results", want);
  const pub = spawnSync(
    "python3",
    [join(repo, "scripts", "publish-results.py"), runDir, "--db", dbPath, "--skip-remote"],
    { encoding: "utf8" },
  );
  if (pub.status !== 0) {
    if (cleanup) rmSync(cleanup, { recursive: true, force: true });
    console.error(pub.stdout || "");
    console.error(pub.stderr || "");
    console.error(`failed to ingest sample run ${want} into ${dbPath}`);
    process.exit(1);
  }
}

const db = openResultsDb(dbPath);
if (!db) {
  if (cleanup) rmSync(cleanup, { recursive: true, force: true });
  console.error(`missing sqlite db at ${dbPath}`);
  process.exit(1);
}
try {
  const index = listRunsFromDb(db);
  const entry = index.runs.find((r) => r.id === want);
  if (!entry) {
    console.error(`run ${want} not in sqlite index; have ${index.runs.map((r) => r.id).join(",")}`);
    process.exit(1);
  }
  if (entry.verdict !== "PASS" || entry.exit_code !== 0) {
    console.error(`unexpected list entry`, entry);
    process.exit(1);
  }
  const report = getJsonColumn(db, want, "report_json");
  const measured = report?.variants?.baseline?.measured;
  if (measured !== 24) {
    console.error(`expected 24 measured downloads, got ${measured}`);
    process.exit(1);
  }
  const run = getJsonColumn(db, want, "run_json");
  if (run?.id !== want || run?.verdict !== "PASS") {
    console.error(`bad run_json`, run?.id, run?.verdict);
    process.exit(1);
  }
  console.log(`OK: sqlite ${dbPath} lists ${want} PASS with ${measured} measured downloads (${index.runs.length} runs)`);
} finally {
  db.close();
  if (cleanup) rmSync(cleanup, { recursive: true, force: true });
}
