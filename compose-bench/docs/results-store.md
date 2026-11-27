# Results store: SQLite locally, Supabase when configured

`dfbench` still only writes `results/<run-id>/`. A separate ingest step
indexes those files for the dashboard.

```sh
compose-bench.sh smoke|run|compare
        │
        │  (dfbench exit code 0/1/2/3/4 is kept)
        ▼
scripts/publish-results.sh results/<run-id>/
        │
        ├─ always: upsert results/dfperf.db   (gitignore'd, no Postgres schema)
        └─ if secrets are set: upsert Supabase schema dfperf + upload report.md
                │
                ▼
dashboard: VITE_SUPABASE_URL + VITE_SUPABASE_ANON_KEY
           → read published rows from schema dfperf
           otherwise → ./data JSON (build/preview) or dfperf.db (`npm run dev`)
```

## Environment

CI / the machine that runs `compose-bench.sh` (write path):

| variable | required | purpose |
|---|---|---|
| `SUPABASE_URL` or `PROJECT_URL` | with the key below | project URL, e.g. `https://YOUR_PROJECT.supabase.co` |
| `SUPABASE_SERVICE_ROLE_KEY` | with the URL above | server-side upsert; never put this in the dashboard or repo |
| `DFPERF_DB` | no | SQLite path (default `results/dfperf.db`) |
| `DFPERF_PUBLISHED` | no | `false` stores the row unpublished |

If the URL or the service role key is missing, remote upload is skipped and
ingest still exits 0. A remote error is logged and `publish-results.sh`
exits non-zero; `compose-bench.sh` prints a warning and **keeps the
benchmark exit code**.

Remote rows are `dfperf.runs` and `dfperf.artifacts`. The publish script
sends `Accept-Profile: dfperf` and `Content-Profile: dfperf` (the same
switch as supabase-js `db: { schema: "dfperf" }`). SQLite tables stay
unqualified in one local file.

Dashboard (read path, baked in at `vite` build time):

| variable | required | purpose |
|---|---|---|
| `VITE_SUPABASE_URL` | with the key below | same project URL |
| `VITE_SUPABASE_ANON_KEY` | with the URL | public read of `published = true` rows in schema `dfperf` |

These are not a user login. The frontend never uses `service_role`. When
either dashboard variable is unset, the UI falls back to local data and
does not go blank. The dashboard client sends the same `dfperf` profile
headers on `/rest/v1/` requests. Storage reads still use the private
bucket `dfperf-artifacts`.

See `dashboard/.env.example`. Do not commit `.env` files or keys.

## Schema `dfperf`

Postgres tables, indexes, RLS, and grants live in schema `dfperf`, not
`public`. SQLite is unchanged: one file, no schema.

| role | access |
|---|---|
| `anon`, `authenticated` | `USAGE` on `dfperf`, `SELECT` only. RLS keeps `published = true`. |
| `service_role` | `USAGE` plus `SELECT`, `INSERT`, `UPDATE`, `DELETE`. Writes stay on this role. |

The Data API must expose schema `dfperf`. In the Supabase project, add
`dfperf` under Data API → Exposed schemas (next to the schemas you already
expose, usually `public` and `storage`). This repository does not change
that setting. Until it is exposed, PostgREST returns an error such as
`PGRST106` and the publish script logs it.

Review
[`schemas/sql/postgres.sql`](../schemas/sql/postgres.sql)
for a fresh project. It refuses to run while `public.runs` or
`public.artifacts` still exist.

## Apply the migration

Fresh project: apply
[`schemas/sql/postgres.sql`](../schemas/sql/postgres.sql).
That creates schema `dfperf` directly.

Existing project that already applied
[`20261124120000_dfperf_results.sql`](../supabase/migrations/20261124120000_dfperf_results.sql):
apply the
[dfperf schema migration](../supabase/migrations/20261127120000_move_results_to_schema_dfperf.sql).
It creates `dfperf`, copies `public.runs` and `public.artifacts`, checks
that every public row is in `dfperf`, then drops the public tables and
points the storage read policy at `dfperf.runs`. Do not edit the earlier
migration files; projects that already applied them keep that history.

Next:

1. Put `SUPABASE_URL` and `SUPABASE_SERVICE_ROLE_KEY` in the CI (or
   local) secrets used by `compose-bench.sh`.
2. Expose schema `dfperf` on the Data API.
3. Put `VITE_SUPABASE_URL` and `VITE_SUPABASE_ANON_KEY` in the dashboard
   build environment (or the GitHub Actions secrets below). Rebuild the
   dashboard once; new runs then show up without rebuilding.

The private bucket `dfperf-artifacts` uses the same published-row rule.
Writes go through `service_role`.

Review the SQL before applying it to any existing Supabase project.

## GitHub Pages

[`.github/workflows/dashboard-pages.yml`](../../.github/workflows/dashboard-pages.yml)
builds `dashboard/` and deploys the static `dist/` to GitHub Pages. The
Pages source is GitHub Actions (`actions/upload-pages-artifact` and
`actions/deploy-pages`), not a `gh-pages` branch.

The workflow runs on pushes to `main` that touch `compose-bench/dashboard/**`
or the workflow file, and on `workflow_dispatch`. It does not run the Go
tests. Compose-bench CI (`.github/workflows/compose-bench.yml`) still builds
and tests the dashboard with no Supabase secrets.

One-time repository setting: Settings → Pages → Build and deployment →
Source: **GitHub Actions**.

Actions secrets, exact names:

| secret | purpose |
|---|---|
| `VITE_SUPABASE_URL` | project URL, inlined at build time |
| `VITE_SUPABASE_ANON_KEY` | anon / publishable key only |

Do not store a `service_role` key in either secret, in the workflow, or in
the built site. The Vite config rejects a JWT whose role is not `anon`.
If either secret is unset, the Pages build still succeeds and the site
uses the local `./data` fallback.

Vite `base` is `./`, so asset paths stay relative at
`https://<owner>.github.io/<repo>/`.

The workflow token is limited to `contents: read`, `pages: write`, and
`id-token: write`. The build job keeps only `contents: read`. The deploy
job keeps only `pages: write` and `id-token: write`.

## Local use

```sh
# ingest one existing run (SQLite only when secrets are unset)
scripts/publish-results.sh results/20261123t0912437e1f

# dashboard without Supabase: copies results/ into public/data/
cd dashboard && npm install && npm run build && npm run preview

# dev server: if results/dfperf.db exists it is served at /data
npm run dev
```

`results/dfperf.db` is local cache, not a substitute for the `results/<id>/`
directories, which remain the raw evidence.
