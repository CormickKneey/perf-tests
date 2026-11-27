-- Local results store for d7y-ci-infra (results/dfperf.db).
-- Single SQLite file with unqualified table names. There is no Postgres schema
-- here; the remote store uses schema dfperf.
-- Structured columns power the dashboard list; full documents stay in * _json
-- text columns (JSON). Artifact bodies are not stored here.

CREATE TABLE IF NOT EXISTS runs (
  id TEXT PRIMARY KEY,
  mode TEXT NOT NULL,
  profile TEXT,
  verdict TEXT,
  exit_code INTEGER,
  started_at TEXT,
  finished_at TEXT,
  non_baseline INTEGER,
  classification TEXT,
  baseline TEXT,
  candidate TEXT,
  overlays TEXT,
  blocks INTEGER,
  reasons TEXT,
  has_report INTEGER,
  git_commit TEXT,
  host TEXT,
  run_json TEXT NOT NULL,
  report_json TEXT,
  preflight_json TEXT,
  published INTEGER NOT NULL DEFAULT 1,
  ingested_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS runs_started_at_idx ON runs (started_at DESC);
CREATE INDEX IF NOT EXISTS runs_published_idx ON runs (published);

CREATE TABLE IF NOT EXISTS artifacts (
  run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  content_type TEXT,
  storage_path TEXT,
  size_bytes INTEGER,
  sha256 TEXT,
  PRIMARY KEY (run_id, name)
);
