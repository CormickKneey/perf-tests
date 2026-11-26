-- Postgres schema for the dfperf results store (Supabase).
-- Apply to your Supabase project (see docs/results-store.md).
-- Do not apply this file to unrelated Supabase projects.
--
-- Writes go through the service_role key (bypasses RLS). The dashboard uses
-- the anon / publishable key and can only SELECT published = true rows.

create table if not exists public.runs (
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

create index if not exists runs_started_at_idx on public.runs (started_at desc);
create index if not exists runs_published_idx on public.runs (published) where published = true;

create table if not exists public.artifacts (
  run_id text not null references public.runs(id) on delete cascade,
  name text not null,
  content_type text,
  storage_path text,
  size_bytes integer,
  sha256 text,
  primary key (run_id, name)
);

alter table public.runs enable row level security;
alter table public.artifacts enable row level security;

revoke all on table public.runs from anon, authenticated, public;
revoke all on table public.artifacts from anon, authenticated, public;
grant select on table public.runs to anon, authenticated;
grant select on table public.artifacts to anon, authenticated;

drop policy if exists "published runs are readable" on public.runs;
create policy "published runs are readable"
  on public.runs
  for select
  to anon, authenticated
  using (published = true);

drop policy if exists "published artifacts are readable" on public.artifacts;
create policy "published artifacts are readable"
  on public.artifacts
  for select
  to anon, authenticated
  using (
    exists (
      select 1 from public.runs r
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
      select 1 from public.runs r
      where r.published = true
        and name like ('runs/' || r.id || '/%')
    )
  );
