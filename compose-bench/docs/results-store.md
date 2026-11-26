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
        ├─ always: upsert results/dfperf.db   (gitignore'd)
        └─ if secrets are set: upsert Supabase + upload report.md
                │
                ▼
dashboard: VITE_SUPABASE_URL + VITE_SUPABASE_ANON_KEY
           → read published rows from the project
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

Dashboard (read path, baked in at `vite` build time):

| variable | required | purpose |
|---|---|---|
| `VITE_SUPABASE_URL` | with the key below | same project URL |
| `VITE_SUPABASE_ANON_KEY` | with the URL | public read of `published = true` rows only |

These are not a user login. The frontend never uses `service_role`. When
either dashboard variable is unset, the UI falls back to local data and
does not go blank.

See `dashboard/.env.example`. Do not commit `.env` files or keys.

## Apply the migration

Apply the schema to your own Supabase project if you want remote publish.
Review
[`schemas/sql/postgres.sql`](../schemas/sql/postgres.sql)
or
[`supabase/migrations/20261124120000_dfperf_results.sql`](../supabase/migrations/20261124120000_dfperf_results.sql)
if you need the exact SQL.

Next:

1. Put `SUPABASE_URL` and `SUPABASE_SERVICE_ROLE_KEY` in the CI (or
   local) secrets used by `compose-bench.sh`.
2. Put `VITE_SUPABASE_URL` and `VITE_SUPABASE_ANON_KEY` in the dashboard
   build environment. Rebuild the dashboard once; new runs then show up
   without rebuilding.

The schema enables RLS on `runs` and `artifacts`. `anon` /
`authenticated` may only `SELECT` rows with `published = true`; they
cannot insert or update. The private bucket `dfperf-artifacts` uses the
same rule. Writes go through `service_role`.

Review the SQL before applying it to any existing Supabase project.

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
