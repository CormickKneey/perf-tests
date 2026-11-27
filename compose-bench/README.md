# Compose benchmark

`compose-bench` is a standalone [Dragonfly](https://github.com/dragonflyoss/dragonfly)
benchmark that runs next to the existing cluster `dfbench` tool in this repository.
Give it scheduler and client versions (tags or digests); it starts the topology with
Docker Compose in interleaved ABBA blocks, measures dfget downloads in four cache
states, and writes raw data, a statistical report (JSON + Markdown), and an exit
code. The dashboard reads `results/` locally, or published rows from Supabase when
configured.

```sh
# single version smoke (1 block)
scripts/compose-bench.sh smoke --ref v2.5.4+v1.5.14

# baseline vs candidate (ABBA, 4 blocks each); A/A when both refs are equal
scripts/compose-bench.sh run --profile file-3peers \
  --baseline v2.5.4+v1.5.14 --candidate v2.5.4+v1.5.14

# dashboard (light theme by default; switch language and theme in the header)
cd dashboard && npm install && npm run build && npm run preview   # http://127.0.0.1:4173
```

Run the commands above from this `compose-bench/` directory. Exit codes: `0` pass,
`1` regression, `2` benchmark failed, `3` infra/invalid, `4` inconclusive
(priority 3 > 2 > 1 > 4 > 0).

| Path | What |
| --- | --- |
| `compose/compose.yaml` | One Compose block: scheduler, seed, three clients, drivers, origin |
| `compose/compare.yaml` | Comparison policy (alpha, thresholds, failure rate) |
| `compose/profiles/*.env` | Benchmark profiles (`file-3peers`) |
| `compose/config/` | dfdaemon / scheduler / dynconfig templates, `overlays/` |
| `scripts/compose-bench.sh` | Entry point |
| `cmd/dfbench`, `pkg/` | Go CLI and libraries; compose images install the binary as `dfbench` |
| `schemas/` | JSON schemas of the result documents |
| `docs/compose.md` | Design notes and verified upstream facts |
| `docs/results-store.md` | SQLite ingest and optional Supabase publish |
| `results/<run>/` | Local runs; this tree includes a few published sample documents only |
| `dashboard/` | Vite + React dashboard (light default, English/Chinese, theme toggle) |
| `schemas/sql/`, `supabase/migrations/` | SQLite file plus Postgres schema `dfperf` for the results store |
| `scripts/publish-results.sh` | Ingest a run into SQLite; upload to Supabase when secrets are set |

After `smoke`, `run` or `compare`, `compose-bench.sh` calls
`scripts/publish-results.sh` on the new `results/<run-id>/`. That writes
`results/dfperf.db` and, if `SUPABASE_URL` and `SUPABASE_SERVICE_ROLE_KEY`
are set, upserts the same row to Supabase. A publish failure is logged and
does not change the benchmark exit code. Remote tables live in Postgres
schema `dfperf` (not `public`). SQLite stays a single local file with no
schema. The dashboard uses `VITE_SUPABASE_URL` and `VITE_SUPABASE_ANON_KEY`
when both are set, and otherwise falls back to local `./data` (or
`dfperf.db` in `npm run dev`). GitHub Actions workflow
`dashboard-pages.yml` publishes `dashboard/dist` to GitHub Pages and reads
those two values from Actions secrets of the same names. Compose-bench CI
still builds the dashboard when the secrets are unset. Never put a
`service_role` key in the dashboard, the workflow, or the repository.
See [docs/results-store.md](docs/results-store.md).

Results from macOS (OrbStack / Docker Desktop) are marked **non-baseline**;
formal baselines need a fixed Linux host. The sample documents under
`results/` came from OrbStack and are for UI and schema demos only.

The Go module path is `github.com/dragonflyoss/perf-tests/compose-bench`.
It is separate from the root `github.com/dragonflyoss/perf-tests` module so
the existing `cmd/dfbench` cluster tool and `pkg/stats` stay unchanged.
