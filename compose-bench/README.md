# Compose benchmark

`compose-bench` is a standalone [Dragonfly](https://github.com/dragonflyoss/dragonfly)
benchmark that runs next to the existing cluster `dfbench` tool in this repository.
Give it scheduler and client versions (tags or digests); it starts the topology with
Docker Compose in interleaved ABBA blocks, measures dfget downloads in four cache
states, and writes raw data, a statistical report (JSON + Markdown), and an exit
code.

```sh
# single version smoke (1 block)
scripts/compose-bench.sh smoke --ref v2.5.4+v1.5.14

# baseline vs candidate (ABBA, 4 blocks each); A/A when both refs are equal
scripts/compose-bench.sh run --profile file-3peers \
  --baseline v2.5.4+v1.5.14 --candidate v2.5.4+v1.5.14
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

Results from macOS (OrbStack / Docker Desktop) are marked **non-baseline**;
formal baselines need a fixed Linux host.

The Go module path is `github.com/dragonflyoss/perf-tests/compose-bench`.
It is separate from the root `github.com/dragonflyoss/perf-tests` module so
the existing `cmd/dfbench` cluster tool and `pkg/stats` stay unchanged.
