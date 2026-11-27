#!/usr/bin/env python3
"""dfperf schema contract.

Static checks always run. When DFPERF_TEST_PG_DSN points at a disposable
Postgres superuser database, the public -> dfperf migration and the fresh
postgres.sql path are applied for real. That database is mutated (roles
anon, authenticated, and service_role; schemas public, storage, dfperf).
"""

from __future__ import annotations

import os
import shutil
import subprocess
import unittest
from pathlib import Path
from urllib.parse import urlparse

ROOT = Path(__file__).resolve().parent.parent
REPO = ROOT.parent
OLD_MIGRATION = ROOT / "supabase/migrations/20261124120000_dfperf_results.sql"
NEW_MIGRATION = ROOT / "supabase/migrations/20261127120000_move_results_to_schema_dfperf.sql"
POSTGRES_SQL = ROOT / "schemas/sql/postgres.sql"
SQLITE_SQL = ROOT / "schemas/sql/sqlite.sql"
DATA_TS = ROOT / "dashboard/src/data.ts"
PUBLISH_PY = ROOT / "scripts/publish-results.py"
PAGES_WORKFLOW = REPO / ".github/workflows/dashboard-pages.yml"

STORAGE_STUB = """
do $$
begin
  if not exists (select 1 from pg_roles where rolname = 'anon') then
    create role anon nologin;
  end if;
  if not exists (select 1 from pg_roles where rolname = 'authenticated') then
    create role authenticated nologin;
  end if;
  if not exists (select 1 from pg_roles where rolname = 'service_role') then
    create role service_role nologin bypassrls;
  end if;
end
$$;

alter role service_role bypassrls;

drop schema if exists dfperf cascade;
drop schema if exists storage cascade;
drop schema if exists public cascade;
create schema public;
grant all on schema public to public;

create schema storage;
create table storage.buckets (
  id text primary key,
  name text not null,
  public boolean not null default false,
  file_size_limit bigint,
  allowed_mime_types text[]
);
create table storage.objects (
  id bigint generated always as identity primary key,
  bucket_id text,
  name text
);
alter table storage.objects enable row level security;
grant usage on schema storage to anon, authenticated, service_role;
grant select, insert, update, delete on storage.objects to anon, authenticated, service_role;
grant select, insert, update, delete on storage.buckets to anon, authenticated, service_role;
"""

SEED_PUBLIC = """
insert into public.runs (id, mode, profile, published, run_json) values
  ('pub', 'smoke', 'file-3peers', true, '{"id":"pub"}'::jsonb),
  ('draft', 'smoke', 'file-3peers', false, '{"id":"draft"}'::jsonb);
insert into public.artifacts (run_id, name, content_type, storage_path, size_bytes, sha256) values
  ('pub', 'report.md', 'text/markdown', 'runs/pub/report.md', 12, 'abc'),
  ('draft', 'report.md', 'text/markdown', 'runs/draft/report.md', 34, 'def');
insert into storage.objects (bucket_id, name) values
  ('dfperf-artifacts', 'runs/pub/report.md'),
  ('dfperf-artifacts', 'runs/draft/report.md');
"""


def pg_env(dsn: str) -> dict[str, str]:
    parsed = urlparse(dsn)
    env = os.environ.copy()
    if parsed.hostname:
        env["PGHOST"] = parsed.hostname
    if parsed.port:
        env["PGPORT"] = str(parsed.port)
    if parsed.username:
        env["PGUSER"] = parsed.username
    if parsed.password:
        env["PGPASSWORD"] = parsed.password
    database = parsed.path.lstrip("/")
    if database:
        env["PGDATABASE"] = database
    return env


class StaticSchemaTests(unittest.TestCase):
    def test_postgres_sql_targets_dfperf(self):
        sql = POSTGRES_SQL.read_text(encoding="utf-8").lower()
        self.assertIn("create schema if not exists dfperf", sql)
        self.assertIn("create table if not exists dfperf.runs", sql)
        self.assertIn("create table if not exists dfperf.artifacts", sql)
        self.assertIn("grant usage on schema dfperf to anon, authenticated, service_role", sql)
        self.assertIn("grant select on table dfperf.runs to anon, authenticated", sql)
        self.assertIn("grant select, insert, update, delete on table dfperf.runs to service_role", sql)
        self.assertIn("from dfperf.runs r", sql)
        self.assertNotIn("create table if not exists public.runs", sql)
        self.assertNotIn("create table if not exists public.artifacts", sql)
        self.assertIn("exposed schemas", sql)

    def test_new_migration_copies_then_drops_public(self):
        sql = NEW_MIGRATION.read_text(encoding="utf-8").lower()
        self.assertIn("create schema if not exists dfperf", sql)
        self.assertIn("insert into dfperf.runs", sql)
        self.assertIn("from public.runs", sql)
        self.assertIn("insert into dfperf.artifacts", sql)
        self.assertIn("from public.artifacts", sql)
        self.assertIn("refusing to drop public.runs", sql)
        self.assertIn("refusing to drop public.artifacts", sql)
        self.assertLess(sql.index("refusing to drop public.runs"), sql.index("drop table if exists public.runs"))
        self.assertLess(sql.index("from public.runs"), sql.index("drop table if exists public.runs"))
        self.assertIn("drop table if exists public.artifacts", sql)
        self.assertIn("grant usage on schema dfperf to anon, authenticated, service_role", sql)
        self.assertIn("from dfperf.runs r", sql)
        # History stays applied as written: the original migration still creates public tables.
        old = OLD_MIGRATION.read_text(encoding="utf-8").lower()
        self.assertIn("create table if not exists public.runs", old)
        self.assertNotIn("schema dfperf", old)

    def test_sqlite_has_no_postgres_schema(self):
        sql = SQLITE_SQL.read_text(encoding="utf-8").lower()
        self.assertNotIn("create schema", sql)
        self.assertNotIn("dfperf.runs", sql)
        self.assertIn("create table if not exists runs", sql)

    def test_clients_send_dfperf_profile_headers(self):
        data = DATA_TS.read_text(encoding="utf-8")
        self.assertIn('export const SUPABASE_DB_SCHEMA = "dfperf"', data)
        self.assertIn('"Accept-Profile": SUPABASE_DB_SCHEMA', data)
        self.assertIn('"Content-Profile": SUPABASE_DB_SCHEMA', data)
        publish = PUBLISH_PY.read_text(encoding="utf-8")
        self.assertIn('DB_SCHEMA = "dfperf"', publish)
        self.assertIn('"Accept-Profile": DB_SCHEMA', publish)
        self.assertIn('"Content-Profile": DB_SCHEMA', publish)

    def test_pages_workflow_reads_anon_secrets_only(self):
        text = PAGES_WORKFLOW.read_text(encoding="utf-8")
        self.assertIn("secrets.VITE_SUPABASE_URL", text)
        self.assertIn("secrets.VITE_SUPABASE_ANON_KEY", text)
        self.assertIn("actions/upload-pages-artifact@", text)
        self.assertIn("actions/deploy-pages@", text)
        self.assertIn("contents: read", text)
        self.assertIn("pages: write", text)
        self.assertIn("id-token: write", text)
        self.assertIn("workflow_dispatch", text)
        self.assertIn("branches: [main]", text)
        self.assertNotIn("SERVICE_ROLE", text)
        self.assertNotIn("service_role", text)
        self.assertNotIn("supabase.co", text.lower())


class LiveSchemaTests(unittest.TestCase):
    dsn = ""
    env: dict[str, str] = {}

    @classmethod
    def setUpClass(cls):
        cls.dsn = os.environ.get("DFPERF_TEST_PG_DSN", "").strip()
        if not cls.dsn:
            return
        if shutil.which("psql") is None:
            raise AssertionError("DFPERF_TEST_PG_DSN is set but psql is not installed")
        cls.env = pg_env(cls.dsn)
        probe = cls._psql("select 1", check=False)
        if probe.returncode != 0:
            raise AssertionError(probe.stderr.strip() or "cannot connect to DFPERF_TEST_PG_DSN")

    def setUp(self):
        if not self.dsn:
            self.skipTest("DFPERF_TEST_PG_DSN not set; static checks still ran")
        self.psql(STORAGE_STUB)

    @classmethod
    def _psql(cls, sql: str, check: bool = True) -> subprocess.CompletedProcess[str]:
        proc = subprocess.run(
            ["psql", "-X", "-v", "ON_ERROR_STOP=1", "-q"],
            input=sql,
            capture_output=True,
            text=True,
            env=cls.env,
            check=False,
        )
        if check and proc.returncode != 0:
            raise AssertionError(proc.stderr.strip() or proc.stdout.strip() or "psql failed")
        return proc

    def psql(self, sql: str, check: bool = True) -> subprocess.CompletedProcess[str]:
        return self._psql(sql, check=check)

    def apply_file(self, path: Path, check: bool = True) -> subprocess.CompletedProcess[str]:
        body = path.read_text(encoding="utf-8")
        return self.psql(f"begin;\n{body}\ncommit;\n", check=check)

    def scalar(self, sql: str) -> str:
        proc = subprocess.run(
            ["psql", "-X", "-v", "ON_ERROR_STOP=1", "-q", "-t", "-A", "-c", sql],
            capture_output=True,
            text=True,
            env=self.env,
            check=False,
        )
        if proc.returncode != 0:
            raise AssertionError(proc.stderr.strip() or sql)
        return proc.stdout.strip()

    def test_migration_moves_rows_and_keeps_rls(self):
        self.apply_file(OLD_MIGRATION)
        self.psql(SEED_PUBLIC)
        self.apply_file(NEW_MIGRATION)

        self.assertEqual(self.scalar("select to_regclass('public.runs') is null"), "t")
        self.assertEqual(self.scalar("select to_regclass('public.artifacts') is null"), "t")
        self.assertEqual(self.scalar("select count(*) from dfperf.runs"), "2")
        self.assertEqual(self.scalar("select count(*) from dfperf.artifacts"), "2")
        self.assertEqual(self.scalar("select id from storage.buckets where id = 'dfperf-artifacts'"), "dfperf-artifacts")

        # Re-applying the upgrade is a no-op once public tables are gone.
        self.apply_file(NEW_MIGRATION)
        self.assertEqual(self.scalar("select count(*) from dfperf.runs"), "2")
        self.assertEqual(self.scalar("select string_agg(id, ',' order by id) from dfperf.runs"), "draft,pub")

        # postgres.sql matches the upgraded database and must not drop the copied rows.
        self.apply_file(POSTGRES_SQL)
        self.assertEqual(self.scalar("select count(*) from dfperf.runs"), "2")
        self._assert_reader_and_writer()

    def test_postgres_sql_refuses_to_hide_existing_public_rows(self):
        self.apply_file(OLD_MIGRATION)
        self.psql(SEED_PUBLIC)
        failed = self.apply_file(POSTGRES_SQL, check=False)
        self.assertNotEqual(failed.returncode, 0)
        self.assertIn("public.runs", failed.stderr)
        self.assertEqual(self.scalar("select count(*) from public.runs"), "2")
        self.assertEqual(self.scalar("select to_regclass('dfperf.runs') is null"), "t")

    def test_fresh_postgres_sql_creates_dfperf(self):
        self.apply_file(POSTGRES_SQL)
        self.assertEqual(self.scalar("select to_regclass('public.runs') is null"), "t")
        self.assertEqual(self.scalar("select to_regclass('dfperf.runs') is not null"), "t")
        self.psql(
            """
            insert into dfperf.runs (id, mode, published, run_json) values
              ('pub', 'smoke', true, '{}'::jsonb),
              ('draft', 'smoke', false, '{}'::jsonb);
            insert into storage.objects (bucket_id, name) values
              ('dfperf-artifacts', 'runs/pub/report.md'),
              ('dfperf-artifacts', 'runs/draft/report.md');
            """
        )
        self._assert_reader_and_writer()

    def _assert_reader_and_writer(self):
        self.assertEqual(self.scalar("select relrowsecurity from pg_class c join pg_namespace n on n.oid = c.relnamespace where n.nspname = 'dfperf' and c.relname = 'runs'"), "t")
        self.assertEqual(self.scalar("select relrowsecurity from pg_class c join pg_namespace n on n.oid = c.relnamespace where n.nspname = 'dfperf' and c.relname = 'artifacts'"), "t")
        for role in ("anon", "authenticated", "service_role"):
            self.assertEqual(self.scalar(f"select has_schema_privilege('{role}', 'dfperf', 'usage')"), "t", role)
        self.assertEqual(self.scalar("select has_table_privilege('anon', 'dfperf.runs', 'select')"), "t")
        self.assertEqual(self.scalar("select has_table_privilege('anon', 'dfperf.runs', 'insert')"), "f")
        self.assertEqual(self.scalar("select has_table_privilege('authenticated', 'dfperf.artifacts', 'select')"), "t")
        self.assertEqual(self.scalar("select has_table_privilege('authenticated', 'dfperf.artifacts', 'update')"), "f")
        self.assertEqual(self.scalar("select has_table_privilege('service_role', 'dfperf.runs', 'insert')"), "t")
        self.assertEqual(self.scalar("select has_table_privilege('service_role', 'dfperf.runs', 'update')"), "t")
        self.assertEqual(self.scalar("select has_table_privilege('service_role', 'dfperf.runs', 'delete')"), "t")
        self.assertEqual(self.scalar("select has_table_privilege('service_role', 'dfperf.artifacts', 'insert')"), "t")

        qual = self.scalar(
            "select qual from pg_policies where schemaname = 'storage' and policyname = 'dfperf artifacts: read published'"
        )
        self.assertIn("dfperf.runs", qual)
        self.assertNotIn("public.runs", qual)

        self.assertEqual(
            self.scalar("set role anon; select coalesce(string_agg(id, ',' order by id), '') from dfperf.runs;"),
            "pub",
        )
        self.assertEqual(
            self.scalar("set role authenticated; select coalesce(string_agg(id, ',' order by id), '') from dfperf.runs;"),
            "pub",
        )
        denied = self.psql(
            "set role anon; insert into dfperf.runs (id, mode, run_json) values ('nope', 'smoke', '{}'::jsonb);",
            check=False,
        )
        self.assertNotEqual(denied.returncode, 0)
        self.assertTrue(
            "permission denied" in denied.stderr.lower() or "row-level security" in denied.stderr.lower(),
            denied.stderr,
        )
        self.psql(
            "set role service_role; insert into dfperf.runs (id, mode, run_json, published) values ('svc', 'smoke', '{}'::jsonb, false);"
        )
        self.assertEqual(self.scalar("select count(*) from dfperf.runs where id = 'svc'"), "1")
        self.assertEqual(
            self.scalar("set role anon; select coalesce(string_agg(id, ',' order by id), '') from dfperf.runs;"),
            "pub",
        )
        self.assertEqual(
            self.scalar(
                "set role anon; select coalesce(string_agg(name, ',' order by name), '') from storage.objects;"
            ),
            "runs/pub/report.md",
        )


if __name__ == "__main__":
    unittest.main()
