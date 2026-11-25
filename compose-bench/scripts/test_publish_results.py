#!/usr/bin/env python3
"""Tests for scripts/publish-results.py (local SQLite ingest and remote skip/fail)."""

from __future__ import annotations

import importlib.util
import io
import json
import os
import sqlite3
import subprocess
import sys
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
RUN_ID = "20261121t025924cae6"
RUN_DIR = ROOT / "results" / RUN_ID
MODULE_PATH = ROOT / "scripts" / "publish-results.py"


def load_module():
    spec = importlib.util.spec_from_file_location("publish_results", MODULE_PATH)
    mod = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(mod)
    return mod


pr = load_module()


class IngestTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.db = Path(self.tmp.name) / "dfperf.db"
        # Guarantee tests never inherit a real service role or project URL.
        self._env_backup = {}
        for key in list(os.environ):
            if "SUPABASE" in key or key in {"PROJECT_URL", "DFPERF_DB", "DFPERF_PUBLISHED"}:
                self._env_backup[key] = os.environ.pop(key)

    def tearDown(self):
        os.environ.update(self._env_backup)
        self.tmp.cleanup()

    def test_ingest_smoke_run_into_sqlite(self):
        self.assertTrue((RUN_DIR / "run.json").is_file(), "expected committed smoke run")
        rc = pr.main([str(RUN_DIR), "--db", str(self.db)])
        self.assertEqual(rc, 0)
        self.assertTrue(self.db.is_file())

        conn = sqlite3.connect(self.db)
        conn.row_factory = sqlite3.Row
        row = conn.execute("SELECT * FROM runs WHERE id = ?", (RUN_ID,)).fetchone()
        self.assertIsNotNone(row)
        self.assertEqual(row["verdict"], "PASS")
        self.assertEqual(row["exit_code"], 0)
        self.assertEqual(row["mode"], "smoke")
        self.assertEqual(row["profile"], "file-3peers")
        self.assertEqual(row["classification"], "orbstack")
        self.assertTrue(row["has_report"])
        self.assertTrue(row["published"])
        self.assertIn("scheduler:v2.5.4", row["baseline"])
        self.assertIn("client:v1.5.14", row["baseline"])

        report = json.loads(row["report_json"])
        measured = report["variants"]["baseline"]["measured"]
        self.assertEqual(measured, 24, "smoke run should have 24 successful downloads")
        self.assertEqual(report["variants"]["baseline"]["failures"], 0)

        art = conn.execute("SELECT * FROM artifacts WHERE run_id = ?", (RUN_ID,)).fetchone()
        self.assertIsNotNone(art)
        self.assertEqual(art["name"], "report.md")
        self.assertGreater(art["size_bytes"], 0)
        conn.close()

    def test_skip_remote_without_config_is_success(self):
        buf = io.StringIO()
        old = sys.stderr
        sys.stderr = buf
        try:
            rc = pr.main([str(RUN_DIR), "--db", str(self.db)])
        finally:
            sys.stderr = old
        self.assertEqual(rc, 0)
        self.assertIn("remote skipped", buf.getvalue())

    def test_partial_remote_config_skips(self):
        os.environ["SUPABASE_URL"] = "https://example.supabase.co"
        buf = io.StringIO()
        old = sys.stderr
        sys.stderr = buf
        try:
            rc = pr.main([str(RUN_DIR), "--db", str(self.db)])
        finally:
            sys.stderr = old
        self.assertEqual(rc, 0)
        self.assertIn("SUPABASE_SERVICE_ROLE_KEY is not set", buf.getvalue())
        self.assertIn("remote skipped", buf.getvalue())

    def test_remote_failure_does_not_write_and_exits_nonzero(self):
        # Point at a local server that rejects /rest/v1/runs (migration missing).
        hits = []

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_args, **_kwargs):
                return

            def do_GET(self):
                hits.append(("GET", self.path))
                body = b'{"code":"PGRST205","message":"Could not find the table public.runs"}'
                self.send_response(404)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(body)

            def do_POST(self):
                hits.append(("POST", self.path))
                length = int(self.headers.get("Content-Length") or 0)
                self.rfile.read(length)
                self.send_response(500)
                self.end_headers()
                self.wfile.write(b"should-not-be-called")

        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            host, port = server.server_address[:2]
            os.environ["SUPABASE_URL"] = f"http://127.0.0.1:{port}"
            os.environ["SUPABASE_SERVICE_ROLE_KEY"] = "test-service-role-not-real"
            rc = pr.main([str(RUN_DIR), "--db", str(self.db)])
            self.assertEqual(rc, 1)
            self.assertTrue(any(m == "GET" and "/rest/v1/runs" in path for m, path in hits))
            self.assertFalse(any(m == "POST" for m, _ in hits), "must not POST when the table is missing")
        finally:
            server.shutdown()
            server.server_close()

    def test_unreachable_remote_exits_nonzero_sqlite_still_written(self):
        os.environ["SUPABASE_URL"] = "http://127.0.0.1:1"
        os.environ["SUPABASE_SERVICE_ROLE_KEY"] = "test-service-role-not-real"
        rc = pr.main([str(RUN_DIR), "--db", str(self.db)])
        self.assertEqual(rc, 1)
        conn = sqlite3.connect(self.db)
        n = conn.execute("SELECT count(*) FROM runs WHERE id = ?", (RUN_ID,)).fetchone()[0]
        conn.close()
        self.assertEqual(n, 1, "local sqlite ingest should succeed even if remote fails")

    def test_dry_run_skips_writes(self):
        os.environ["SUPABASE_URL"] = "http://127.0.0.1:1"
        os.environ["SUPABASE_SERVICE_ROLE_KEY"] = "test-service-role-not-real"
        # dry-run should not create the db file (no sqlite write)
        rc = pr.main([str(RUN_DIR), "--db", str(self.db), "--dry-run", "--skip-remote"])
        self.assertEqual(rc, 0)
        self.assertFalse(self.db.exists())


class ComposeBenchHookTests(unittest.TestCase):
    def test_publish_failure_preserves_bench_exit_code(self):
        script = ROOT / "scripts" / "test_compose_bench_publish.sh"
        self.assertTrue(script.is_file())
        proc = subprocess.run(["bash", str(script)], cwd=str(ROOT), capture_output=True, text=True)
        if proc.returncode != 0:
            sys.stderr.write(proc.stdout + proc.stderr)
        self.assertEqual(proc.returncode, 0, proc.stderr)


if __name__ == "__main__":
    unittest.main()
