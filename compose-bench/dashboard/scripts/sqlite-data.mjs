// Read published runs from a local results/dfperf.db for the Vite dev server
// and for offline checks. The browser never opens the file itself.
import { DatabaseSync } from "node:sqlite";
import { existsSync } from "node:fs";

const LIST_COLS =
  "id, mode, profile, verdict, exit_code, started_at, finished_at, non_baseline, classification, baseline, candidate, overlays, blocks, reasons, has_report";

export function parseMaybeJson(value) {
  if (value == null || value === "") return null;
  if (typeof value !== "string") return value;
  try {
    return JSON.parse(value);
  } catch {
    return value;
  }
}

export function rowToIndex(row) {
  return {
    id: row.id,
    mode: row.mode,
    profile: row.profile,
    verdict: row.verdict,
    exit_code: row.exit_code,
    started_at: row.started_at,
    finished_at: row.finished_at,
    non_baseline: Boolean(row.non_baseline),
    classification: row.classification,
    baseline: row.baseline,
    candidate: row.candidate ?? undefined,
    overlays: parseMaybeJson(row.overlays),
    blocks: row.blocks,
    reasons: parseMaybeJson(row.reasons),
    has_report: Boolean(row.has_report),
  };
}

export function openResultsDb(path, { readOnly = true } = {}) {
  if (!existsSync(path)) return null;
  return new DatabaseSync(path, { readOnly });
}

export function listRunsFromDb(db) {
  const rows = db.prepare(`SELECT ${LIST_COLS} FROM runs WHERE published = 1 ORDER BY id DESC`).all();
  return {
    schema: "dfperf/v1",
    generated_at: new Date().toISOString(),
    runs: rows.map(rowToIndex),
  };
}

export function getJsonColumn(db, id, column) {
  if (!["run_json", "report_json", "preflight_json"].includes(column)) {
    throw new Error(`unsupported column ${column}`);
  }
  const row = db.prepare(`SELECT ${column} AS doc FROM runs WHERE id = ? AND published = 1`).get(id);
  if (!row || row.doc == null) return null;
  return typeof row.doc === "string" ? JSON.parse(row.doc) : row.doc;
}
