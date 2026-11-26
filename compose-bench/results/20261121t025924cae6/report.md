# d7y-ci-infra smoke report — 20261121t025924cae6

**Verdict: PASS** (exit code 0)

> **non-baseline**: orbstack engine on darwin (VM-backed, Docker-Desktop-class); results are non-baseline functional smoke data. Results are a functional smoke signal only; formal baselines require a fixed Linux host.

## Versions

| variant | component | ref | pinned | version |
|---|---|---|---|---|
| baseline | scheduler | `dragonflyoss/scheduler:v2.5.4` | `dragonflyoss/scheduler@sha256:94cf26b861c4` | v2.5.0 (5ace19f) |
| baseline | client | `dragonflyoss/client:v1.5.14` | `dragonflyoss/client@sha256:d942d337b7c6` | dfget 1.5.14 (8887c145f, 2026-11-13) |
| baseline | driver | `dragonflyoss/client@sha256:d942d337b7c6605b5df451d445ade7d5e45b325db75ed62c8cb07998b3c93d04` | `dfperf-driver:d942d337b7c6-35fbaf237836` | dfget 1.5.14 (8887c145f, 2026-11-13) |

## Environment

- host: mini-24.local (darwin, arm64, 10 CPUs)
- docker: 29.4.0 on OrbStack (kernel 7.0.5-orbstack-00330-ge3df4e19b0a0-dirty), compose 5.1.2
- classification: **orbstack**, baseline eligible: false
- profile: `file-3peers`, fixtures volume `dfperf-fixtures-6a9efa261473012c`
- git: df6a69338ed5 (dirty)

## Blocks (execution order)

| # | block | status | measured | failures | notes |
|---|---|---|---|---|---|
| 1 | 01-baseline-b1 | ok | 24 | 0 |  |

## Failure rates

| variant | valid blocks | measured | failures | rate |
|---|---|---|---|---|
| baseline | 1/1 | 24 | 0 | 0.0000 |

## Measurements

| state | file | n | p10 ms | median ms | p90 ms | median MiB/s |
|---|---|---|---|---|---|---|
| cold | f-4mib.bin | 3 | 1237.7 | 1303.2 | 1315.0 | 3.1 |
| seed-warm | f-4mib.bin | 3 | 90.1 | 95.7 | 96.3 | 41.8 |
| peer-warm | f-4mib.bin | 3 | 173.6 | 173.9 | 197.4 | 23.0 |
| local-hit | f-4mib.bin | 3 | 3.3 | 3.7 | 6.1 | 1094.2 |
| cold | f-64mib.bin | 3 | 1047.5 | 1057.6 | 1235.1 | 60.5 |
| seed-warm | f-64mib.bin | 3 | 67.6 | 88.6 | 101.5 | 722.1 |
| peer-warm | f-64mib.bin | 3 | 177.8 | 179.9 | 196.5 | 355.7 |
| local-hit | f-64mib.bin | 3 | 2.1 | 2.3 | 2.4 | 27854.5 |

## Validity checks

- 01-baseline-b1 `fixtures-manifest` ok: 2 files, spec 6a9efa261473012c
- 01-baseline-b1 `drivers-and-metrics-ready` ok: 4 drivers, 5 metrics endpoints, dfget 1.5.14 (8887c145f, 2026-11-13)
- 01-baseline-b1 `seed-announced` ok: attempt 1: seed back-to-source 4194304 bytes, client-1 remote-peer 4194304 bytes, client back-to-source 0
- 01-baseline-b1 `cold-client-back-to-source-zero` ok: 6 cold samples, 0 with client back-to-source bytes 
- 01-baseline-b1 `local-hit-served-locally` ok (informational): 6 local-hit samples, 0 not fully local

Block order: 01-baseline-b1
