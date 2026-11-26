#!/usr/bin/env python3
"""Ingest a dfbench results/<run-id>/ directory into local SQLite and, when
configured, upsert it to Supabase.

dfbench itself is not involved: this script only reads the files it already
wrote. Remote upload requires both a project URL and SUPABASE_SERVICE_ROLE_KEY;
if either is missing the remote step is skipped and the process still exits 0.
Failures talking to Supabase are logged and become a non-zero exit from *this*
script only. compose-bench.sh must not propagate that status as the benchmark
exit code.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import sqlite3
import sys
import urllib.error
import urllib.request
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

SCHEMA_DIR = Path(__file__).resolve().parent.parent / "schemas" / "sql"
DEFAULT_BUCKET = "dfperf-artifacts"
HOST_SUMMARY_KEYS = (
    "hostname",
    "os",
    "kernel",
    "arch",
    "cpus",
    "memory_bytes",
    "docker_version",
    "compose_version",
    "classification",
    "baseline_eligible",
    "note",
)
LIST_TEXT_FILES = ("report.md",)
JSON_DOCS = ("run.json", "report.json", "preflight.json")


class PublishError(Exception):
    pass


def eprint(*args: Any) -> None:
    print(*args, file=sys.stderr)


def utc_now() -> str:
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%fZ")


def normalize_ts(value: Any) -> Any:
    """Postgres timestamptz accepts at most microsecond precision."""
    if not isinstance(value, str) or not value:
        return value
    raw = value.strip()
    try:
        dt = datetime.fromisoformat(raw.replace("Z", "+00:00"))
    except ValueError:
        return value
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=timezone.utc)
    return dt.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%fZ")



def load_json(path: Path) -> Any:
    with path.open("r", encoding="utf-8") as f:
        return json.load(f)


def dumps(value: Any) -> str:
    return json.dumps(value, ensure_ascii=False, separators=(",", ":"))


def variant_label(variant: dict[str, Any] | None) -> str | None:
    if not variant:
        return None
    parts = []
    for key in ("scheduler", "client"):
        img = variant.get(key) or {}
        ref = img.get("ref") or ""
        ver = img.get("version") or ""
        parts.append(f"{ref} [{ver}]" if ver else ref)
    label = " + ".join(parts).strip(" +")
    return label or None


def host_summary(host: Any) -> dict[str, Any] | None:
    if not isinstance(host, dict):
        return None
    out = {k: host[k] for k in HOST_SUMMARY_KEYS if k in host}
    return out or None


def bool_env(name: str, default: bool = True) -> bool:
    raw = os.environ.get(name)
    if raw is None or raw.strip() == "":
        return default
    return raw.strip().lower() in {"1", "true", "yes", "on"}


def resolve_run_dir(arg: str) -> Path:
    path = Path(arg).expanduser()
    if path.is_dir():
        return path.resolve()
    # Bare run id under ./results or repo results/
    repo = Path(__file__).resolve().parent.parent
    for candidate in (Path.cwd() / "results" / arg, repo / "results" / arg, path):
        if candidate.is_dir():
            return candidate.resolve()
    raise PublishError(f"run directory not found: {arg}")


def build_row(run_dir: Path, published: bool) -> dict[str, Any]:
    run_path = run_dir / "run.json"
    if not run_path.is_file():
        raise PublishError(f"missing {run_path}")
    run = load_json(run_path)
    run_id = run.get("id") or run_dir.name
    if not isinstance(run_id, str) or not run_id:
        raise PublishError(f"run.json has no id in {run_dir}")

    report = load_json(run_dir / "report.json") if (run_dir / "report.json").is_file() else None
    preflight = load_json(run_dir / "preflight.json") if (run_dir / "preflight.json").is_file() else None

    reasons = None
    if isinstance(report, dict) and report.get("reasons"):
        reasons = report["reasons"]
    elif isinstance(run.get("notes"), list) and run["notes"]:
        reasons = run["notes"]

    overlays = run.get("baseline", {}).get("overlays") if isinstance(run.get("baseline"), dict) else None
    cand = run.get("candidate")
    if isinstance(cand, dict) and cand.get("overlays"):
        if not overlays:
            overlays = cand["overlays"]
        elif len(cand["overlays"]) > len(overlays):
            overlays = cand["overlays"]
    if overlays == []:
        overlays = None

    blocks = run.get("block_order")
    n_blocks = len(blocks) if isinstance(blocks, list) else None

    host = run.get("host") if isinstance(run.get("host"), dict) else {}
    return {
        "id": run_id,
        "mode": run.get("mode"),
        "profile": run.get("profile"),
        "verdict": run.get("verdict"),
        "exit_code": run.get("exit_code"),
        "started_at": run.get("started_at"),
        "finished_at": run.get("finished_at"),
        "non_baseline": bool(run.get("non_baseline")),
        "classification": host.get("classification"),
        "baseline": variant_label(run.get("baseline") if isinstance(run.get("baseline"), dict) else None),
        "candidate": variant_label(cand if isinstance(cand, dict) else None),
        "overlays": overlays,
        "blocks": n_blocks,
        "reasons": reasons,
        "has_report": report is not None,
        "git_commit": run.get("git_commit"),
        "host": host_summary(host),
        "run_json": run,
        "report_json": report,
        "preflight_json": preflight,
        "published": published,
        "ingested_at": utc_now(),
        "run_dir": str(run_dir),
    }


def list_artifacts(run_dir: Path, run_id: str) -> list[dict[str, Any]]:
    out = []
    for name in LIST_TEXT_FILES:
        path = run_dir / name
        if not path.is_file():
            continue
        data = path.read_bytes()
        out.append(
            {
                "run_id": run_id,
                "name": name,
                "content_type": "text/markdown" if name.endswith(".md") else "text/plain",
                "storage_path": f"runs/{run_id}/{name}",
                "local_path": str(path),
                "size_bytes": len(data),
                "sha256": hashlib.sha256(data).hexdigest(),
                "bytes": data,
            }
        )
    return out


def open_sqlite(db_path: Path) -> sqlite3.Connection:
    db_path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(str(db_path))
    conn.row_factory = sqlite3.Row
    conn.execute("PRAGMA foreign_keys = ON")
    schema = (SCHEMA_DIR / "sqlite.sql").read_text(encoding="utf-8")
    conn.executescript(schema)
    return conn


def _sql_json(value: Any) -> str | None:
    if value is None:
        return None
    return dumps(value)


def _sql_bool(value: Any) -> int:
    return 1 if value else 0


def upsert_sqlite(conn: sqlite3.Connection, row: dict[str, Any], artifacts: list[dict[str, Any]]) -> None:
    cols = [
        "id",
        "mode",
        "profile",
        "verdict",
        "exit_code",
        "started_at",
        "finished_at",
        "non_baseline",
        "classification",
        "baseline",
        "candidate",
        "overlays",
        "blocks",
        "reasons",
        "has_report",
        "git_commit",
        "host",
        "run_json",
        "report_json",
        "preflight_json",
        "published",
        "ingested_at",
    ]
    values = {
        "id": row["id"],
        "mode": row["mode"],
        "profile": row["profile"],
        "verdict": row["verdict"],
        "exit_code": row["exit_code"],
        "started_at": row["started_at"],
        "finished_at": row["finished_at"],
        "non_baseline": _sql_bool(row["non_baseline"]),
        "classification": row["classification"],
        "baseline": row["baseline"],
        "candidate": row["candidate"],
        "overlays": _sql_json(row["overlays"]),
        "blocks": row["blocks"],
        "reasons": _sql_json(row["reasons"]),
        "has_report": _sql_bool(row["has_report"]),
        "git_commit": row["git_commit"],
        "host": _sql_json(row["host"]),
        "run_json": dumps(row["run_json"]),
        "report_json": _sql_json(row["report_json"]),
        "preflight_json": _sql_json(row["preflight_json"]),
        "published": _sql_bool(row["published"]),
        "ingested_at": row["ingested_at"],
    }
    placeholders = ", ".join(":" + c for c in cols)
    assignments = ", ".join(f"{c}=excluded.{c}" for c in cols if c != "id")
    conn.execute(
        f"INSERT INTO runs ({', '.join(cols)}) VALUES ({placeholders}) ON CONFLICT(id) DO UPDATE SET {assignments}",
        values,
    )
    conn.execute("DELETE FROM artifacts WHERE run_id = ?", (row["id"],))
    for art in artifacts:
        conn.execute(
            "INSERT INTO artifacts (run_id, name, content_type, storage_path, size_bytes, sha256) "
            "VALUES (:run_id, :name, :content_type, :storage_path, :size_bytes, :sha256)",
            {k: art[k] for k in ("run_id", "name", "content_type", "storage_path", "size_bytes", "sha256")},
        )
    conn.commit()


def remote_config() -> tuple[str, str] | None:
    url = (
        os.environ.get("SUPABASE_URL")
        or os.environ.get("SUPABASE_PROJECT_URL")
        or os.environ.get("PROJECT_URL")
        or ""
    ).strip().rstrip("/")
    key = (os.environ.get("SUPABASE_SERVICE_ROLE_KEY") or "").strip()
    if not url and not key:
        return None
    if not url or not key:
        missing = "SUPABASE_URL (or PROJECT_URL)" if not url else "SUPABASE_SERVICE_ROLE_KEY"
        eprint(f"publish-results: remote skipped; {missing} is not set")
        return None
    return url, key


def http_json(
    method: str,
    url: str,
    key: str,
    payload: Any | None = None,
    extra_headers: dict[str, str] | None = None,
    timeout: int = 30,
) -> tuple[int, bytes, str]:
    headers = {
        "apikey": key,
        "Authorization": f"Bearer {key}",
        "Accept": "application/json",
    }
    data = None
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
        headers["Content-Type"] = "application/json"
    if extra_headers:
        headers.update(extra_headers)
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            body = resp.read()
            return resp.getcode() or 200, body, resp.headers.get("Content-Type", "")
    except urllib.error.HTTPError as err:
        body = err.read() if err.fp else b""
        return err.code, body, err.headers.get("Content-Type", "") if err.headers else ""
    except urllib.error.URLError as err:
        raise PublishError(f"supabase request failed: {err.reason}") from err


def redact(text: str, key: str) -> str:
    if not key:
        return text
    return text.replace(key, "[redacted]")


def explain_http_error(path: str, status: int, body: bytes, key: str) -> str:
    snippet = redact(body.decode("utf-8", errors="replace")[:500], key)
    hint = ""
    if status in (404, 406) or "PGRST205" in snippet or "Could not find the table" in snippet:
        hint = " (schema may not be applied; see schemas/sql/postgres.sql)"
    elif status in (401, 403):
        hint = " (service role key rejected)"
    return f"supabase {path} HTTP {status}{hint}: {snippet}"


def ensure_remote_table(url: str, key: str) -> None:
    status, body, _ = http_json(
        "GET",
        f"{url}/rest/v1/runs?select=id&limit=1",
        key,
        extra_headers={"Prefer": "count=exact"},
    )
    if status == 200:
        return
    raise PublishError(explain_http_error("GET /rest/v1/runs", status, body, key))


def remote_payload(row: dict[str, Any]) -> dict[str, Any]:
    skip = {"run_dir"}
    payload = {k: v for k, v in row.items() if k not in skip}
    for key in ("started_at", "finished_at", "ingested_at"):
        if key in payload:
            payload[key] = normalize_ts(payload[key])
    return payload


def upsert_remote(url: str, key: str, row: dict[str, Any], artifacts: list[dict[str, Any]], dry_run: bool) -> None:
    ensure_remote_table(url, key)
    payload = remote_payload(row)
    if dry_run:
        eprint(f"publish-results: dry-run; would upsert run {row['id']} to {url}")
        return
    status, body, _ = http_json(
        "POST",
        f"{url}/rest/v1/runs",
        key,
        payload=payload,
        extra_headers={"Prefer": "resolution=merge-duplicates,return=minimal"},
    )
    if status not in (200, 201, 204):
        raise PublishError(explain_http_error("POST /rest/v1/runs", status, body, key))

    art_rows = [
        {k: art[k] for k in ("run_id", "name", "content_type", "storage_path", "size_bytes", "sha256")}
        for art in artifacts
    ]
    if art_rows:
        status, body, _ = http_json(
            "POST",
            f"{url}/rest/v1/artifacts",
            key,
            payload=art_rows,
            extra_headers={"Prefer": "resolution=merge-duplicates,return=minimal"},
        )
        if status not in (200, 201, 204):
            raise PublishError(explain_http_error("POST /rest/v1/artifacts", status, body, key))

    bucket = os.environ.get("DFPERF_STORAGE_BUCKET", DEFAULT_BUCKET)
    for art in artifacts:
        obj_url = f"{url}/storage/v1/object/{bucket}/{art['storage_path']}"
        headers = {
            "apikey": key,
            "Authorization": f"Bearer {key}",
            "Content-Type": art["content_type"],
            "x-upsert": "true",
        }
        req = urllib.request.Request(obj_url, data=art["bytes"], headers=headers, method="POST")
        try:
            with urllib.request.urlopen(req, timeout=30) as resp:
                resp.read()
        except urllib.error.HTTPError as err:
            body = err.read() if err.fp else b""
            # Storage is optional relative to the structured row; warn but keep going
            # only after the row itself succeeded. A missing bucket usually means
            # the migration was not applied fully.
            eprint("publish-results: warning: " + explain_http_error(f"POST storage {art['storage_path']}", err.code, body, key))
        except urllib.error.URLError as err:
            eprint(f"publish-results: warning: storage upload failed: {err.reason}")


def default_db_path(run_dir: Path) -> Path:
    env = os.environ.get("DFPERF_DB")
    if env:
        return Path(env).expanduser().resolve()
    # results/<run-id> -> results/dfperf.db
    parent = run_dir.parent
    if parent.name == "results" or (parent / "index.json").exists() or parent.name.endswith("results"):
        return parent / "dfperf.db"
    repo = Path(__file__).resolve().parent.parent
    return repo / "results" / "dfperf.db"


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    p = argparse.ArgumentParser(description="Ingest a dfbench run directory into SQLite and optionally Supabase.")
    p.add_argument("run_dir", help="results/<run-id> directory (or a bare run id)")
    p.add_argument("--db", default=None, help="SQLite path (default: results/dfperf.db or $DFPERF_DB)")
    p.add_argument("--skip-remote", action="store_true", help="do not contact Supabase even if secrets are set")
    p.add_argument("--skip-sqlite", action="store_true", help="do not write the local SQLite database")
    p.add_argument("--dry-run", action="store_true", help="validate and print actions; no writes")
    p.add_argument("--draft", action="store_true", help="store with published=false")
    return p.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    try:
        run_dir = resolve_run_dir(args.run_dir)
        published = False if args.draft else bool_env("DFPERF_PUBLISHED", True)
        row = build_row(run_dir, published)
        artifacts = list_artifacts(run_dir, row["id"])
    except PublishError as err:
        eprint(f"publish-results: {err}")
        return 1

    eprint(
        f"publish-results: run {row['id']} verdict={row['verdict']} exit={row['exit_code']} "
        f"has_report={row['has_report']}"
    )

    if not args.skip_sqlite:
        db_path = Path(args.db).expanduser().resolve() if args.db else default_db_path(run_dir)
        if args.dry_run:
            eprint(f"publish-results: dry-run; would upsert {row['id']} into {db_path}")
        else:
            conn = open_sqlite(db_path)
            try:
                upsert_sqlite(conn, row, artifacts)
            finally:
                conn.close()
            eprint(f"publish-results: wrote sqlite {db_path}")

    if args.skip_remote:
        eprint("publish-results: remote skipped (--skip-remote)")
        return 0

    cfg = remote_config()
    if cfg is None:
        eprint("publish-results: remote skipped (no Supabase URL and service role key)")
        return 0

    url, key = cfg
    try:
        upsert_remote(url, key, row, artifacts, dry_run=args.dry_run)
    except PublishError as err:
        eprint(f"publish-results: {err}")
        return 1
    if not args.dry_run:
        eprint(f"publish-results: upserted run {row['id']} to supabase")
    return 0


if __name__ == "__main__":
    sys.exit(main())
