-- Postgres schema for the dfperf results store (Supabase).
-- Apply to your Supabase project (see docs/results-store.md).
-- Do not apply this file to unrelated Supabase projects.
--
-- End state: runs and artifacts live in schema dfperf, not public.
-- A project that already applied the public.runs migration must run
-- supabase/migrations/20261127120000_move_results_to_schema_dfperf.sql
-- first. That migration copies public.runs / public.artifacts into dfperf
-- and drops the public tables. This file refuses to run while they exist
-- so a fresh apply cannot hide the old rows behind an empty dfperf schema.
--
-- Writes go through the service_role key (bypasses RLS). The dashboard uses
-- the anon / publishable key and can only SELECT published = true rows.
-- Expose schema dfperf on the Supabase Data API (Exposed schemas). This file
-- does not change that project setting.

do $$
begin
  if to_regclass('public.runs') is not null or to_regclass('public.artifacts') is not null then
    raise exception
      'public.runs or public.artifacts still exists; apply supabase/migrations/20261127120000_move_results_to_schema_dfperf.sql to copy rows into schema dfperf and drop the public tables';
  end if;
end
$$;

create schema if not exists dfperf;

revoke all on schema dfperf from public;
grant usage on schema dfperf to anon, authenticated, service_role;

create table if not exists dfperf.runs (
  id text primary key,
  mode text not null,
  profile text,
  verdict text,
  exit_code integer,
  started_at timestamptz,
  finished_at timestamptz,
  non_baseline boolean,
  classification text,
  baseline text,
  candidate text,
  overlays jsonb,
  blocks integer,
  reasons jsonb,
  has_report boolean,
  git_commit text,
  host jsonb,
  run_json jsonb not null,
  report_json jsonb,
  preflight_json jsonb,
  published boolean not null default true,
  ingested_at timestamptz not null default now()
);

create index if not exists runs_started_at_idx on dfperf.runs (started_at desc);
create index if not exists runs_published_idx on dfperf.runs (published) where published = true;

create table if not exists dfperf.artifacts (
  run_id text not null references dfperf.runs(id) on delete cascade,
  name text not null,
  content_type text,
  storage_path text,
  size_bytes integer,
  sha256 text,
  primary key (run_id, name)
);

alter table dfperf.runs enable row level security;
alter table dfperf.artifacts enable row level security;

-- Data API: anon/authenticated can read; service_role can write.
-- RLS still limits anon/authenticated to published = true. Do not GRANT ALL
-- to anon — writes stay on service_role.
revoke all on table dfperf.runs from public, anon, authenticated, service_role;
revoke all on table dfperf.artifacts from public, anon, authenticated, service_role;
grant select on table dfperf.runs to anon, authenticated;
grant select on table dfperf.artifacts to anon, authenticated;
grant select, insert, update, delete on table dfperf.runs to service_role;
grant select, insert, update, delete on table dfperf.artifacts to service_role;

drop policy if exists "published runs are readable" on dfperf.runs;
create policy "published runs are readable"
  on dfperf.runs
  for select
  to anon, authenticated
  using (published = true);

drop policy if exists "published artifacts are readable" on dfperf.artifacts;
create policy "published artifacts are readable"
  on dfperf.artifacts
  for select
  to anon, authenticated
  using (
    exists (
      select 1 from dfperf.runs r
      where r.id = artifacts.run_id and r.published = true
    )
  );

-- Storage for report.md and similar small text artifacts (not raw logs).
insert into storage.buckets (id, name, public, file_size_limit, allowed_mime_types)
values (
  'dfperf-artifacts',
  'dfperf-artifacts',
  false,
  10485760,
  array['text/markdown', 'text/plain', 'application/json']
)
on conflict (id) do nothing;

drop policy if exists "dfperf artifacts: read published" on storage.objects;
create policy "dfperf artifacts: read published"
  on storage.objects
  for select
  to anon, authenticated
  using (
    bucket_id = 'dfperf-artifacts'
    and exists (
      select 1 from dfperf.runs r
      where r.published = true
        and name like ('runs/' || r.id || '/%')
    )
  );
