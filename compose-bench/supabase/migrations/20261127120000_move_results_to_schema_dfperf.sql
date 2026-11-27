-- Move the results store from public to schema dfperf.
-- Additive: earlier migrations are left unchanged.
--
-- Creates schema dfperf, recreates runs/artifacts there, copies any existing
-- public.runs / public.artifacts rows, checks the copy, then drops the public
-- tables and points the storage read policy at dfperf.runs.
--
-- End state matches schemas/sql/postgres.sql. Expose schema dfperf on the
-- Supabase Data API (Exposed schemas); this migration does not edit that
-- project setting. See docs/results-store.md.

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

-- Copy public rows only when those tables are still present. Upsert so a
-- re-run cannot leave a stale dfperf row in front of a newer public row.
do $move$
declare
  missing_runs bigint;
  missing_artifacts bigint;
  mismatched_runs bigint;
  mismatched_artifacts bigint;
begin
  if to_regclass('public.runs') is not null then
    insert into dfperf.runs (
      id, mode, profile, verdict, exit_code, started_at, finished_at,
      non_baseline, classification, baseline, candidate, overlays, blocks,
      reasons, has_report, git_commit, host, run_json, report_json,
      preflight_json, published, ingested_at
    )
    select
      id, mode, profile, verdict, exit_code, started_at, finished_at,
      non_baseline, classification, baseline, candidate, overlays, blocks,
      reasons, has_report, git_commit, host, run_json, report_json,
      preflight_json, published, ingested_at
    from public.runs
    on conflict (id) do update set
      mode = excluded.mode,
      profile = excluded.profile,
      verdict = excluded.verdict,
      exit_code = excluded.exit_code,
      started_at = excluded.started_at,
      finished_at = excluded.finished_at,
      non_baseline = excluded.non_baseline,
      classification = excluded.classification,
      baseline = excluded.baseline,
      candidate = excluded.candidate,
      overlays = excluded.overlays,
      blocks = excluded.blocks,
      reasons = excluded.reasons,
      has_report = excluded.has_report,
      git_commit = excluded.git_commit,
      host = excluded.host,
      run_json = excluded.run_json,
      report_json = excluded.report_json,
      preflight_json = excluded.preflight_json,
      published = excluded.published,
      ingested_at = excluded.ingested_at;

    select count(*) into missing_runs
    from public.runs p
    where not exists (select 1 from dfperf.runs d where d.id = p.id);

    select count(*) into mismatched_runs
    from public.runs p
    join dfperf.runs d on d.id = p.id
    where (d.mode, d.profile, d.verdict, d.exit_code, d.started_at, d.finished_at,
           d.non_baseline, d.classification, d.baseline, d.candidate, d.overlays,
           d.blocks, d.reasons, d.has_report, d.git_commit, d.host, d.run_json,
           d.report_json, d.preflight_json, d.published, d.ingested_at)
          is distinct from
          (p.mode, p.profile, p.verdict, p.exit_code, p.started_at, p.finished_at,
           p.non_baseline, p.classification, p.baseline, p.candidate, p.overlays,
           p.blocks, p.reasons, p.has_report, p.git_commit, p.host, p.run_json,
           p.report_json, p.preflight_json, p.published, p.ingested_at);

    if missing_runs <> 0 or mismatched_runs <> 0 then
      raise exception
        'refusing to drop public.runs; % row(s) missing and % row(s) mismatched in dfperf.runs',
        missing_runs, mismatched_runs;
    end if;
  end if;

  if to_regclass('public.artifacts') is not null then
    insert into dfperf.artifacts (run_id, name, content_type, storage_path, size_bytes, sha256)
    select run_id, name, content_type, storage_path, size_bytes, sha256
    from public.artifacts
    on conflict (run_id, name) do update set
      content_type = excluded.content_type,
      storage_path = excluded.storage_path,
      size_bytes = excluded.size_bytes,
      sha256 = excluded.sha256;

    select count(*) into missing_artifacts
    from public.artifacts p
    where not exists (
      select 1 from dfperf.artifacts d
      where d.run_id = p.run_id and d.name = p.name
    );

    select count(*) into mismatched_artifacts
    from public.artifacts p
    join dfperf.artifacts d on d.run_id = p.run_id and d.name = p.name
    where (d.content_type, d.storage_path, d.size_bytes, d.sha256)
          is distinct from
          (p.content_type, p.storage_path, p.size_bytes, p.sha256);

    if missing_artifacts <> 0 or mismatched_artifacts <> 0 then
      raise exception
        'refusing to drop public.artifacts; % row(s) missing and % row(s) mismatched in dfperf.artifacts',
        missing_artifacts, mismatched_artifacts;
    end if;
  end if;
end
$move$;

-- The previous storage policy references public.runs. Drop it before the
-- public tables so the dependency does not block the drop.
drop policy if exists "dfperf artifacts: read published" on storage.objects;

drop table if exists public.artifacts;
drop table if exists public.runs;

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

notify pgrst, 'reload schema';
