# d7y-ci-infra smoke report — 20261121t024049aeba

**Verdict: INVALID** (exit code 3)

> **non-baseline**: orbstack engine on darwin (VM-backed, Docker-Desktop-class); results are non-baseline functional smoke data. Results are a functional smoke signal only; formal baselines require a fixed Linux host.

Reasons:

- aborted after block 01-baseline-b1 (invalid)
- block 01-baseline-b1 is invalid: seed-announced: after 16 attempts: client went back-to-source (metric 4194304 bytes, origin 4194304 bytes): seed peer not announced or not used
- no successful measured samples

## Versions

| variant | component | ref | pinned | version |
|---|---|---|---|---|
| baseline | scheduler | `dragonflyoss/scheduler:v2.5.4` | `dragonflyoss/scheduler@sha256:94cf26b861c4` | v2.5.0 (5ace19f) |
| baseline | client | `dragonflyoss/client:v1.5.14` | `dragonflyoss/client@sha256:d942d337b7c6` | dfget 1.5.14 (8887c145f, 2026-11-13) |
| baseline | driver | `dragonflyoss/client@sha256:d942d337b7c6605b5df451d445ade7d5e45b325db75ed62c8cb07998b3c93d04` | `dfperf-driver:d942d337b7c6-cbc502bfccb5` | dfget 1.5.14 (8887c145f, 2026-11-13) |
| baseline | overlays | broken-seed | | |

## Environment

- host: mini-24.local (darwin, arm64, 10 CPUs)
- docker: 29.4.0 on OrbStack (kernel 7.0.5-orbstack-00330-ge3df4e19b0a0-dirty), compose 5.1.2
- classification: **orbstack**, baseline eligible: false
- profile: `file-3peers`, fixtures volume `dfperf-fixtures-6a9efa261473012c`
- git: 49a61ecd4e5c

## Blocks (execution order)

| # | block | status | measured | failures | notes |
|---|---|---|---|---|---|
| 1 | 01-baseline-b1 | invalid | 0 | 0 | seed-announced: after 16 attempts: client went back-to-source (metric 4194304 bytes, origin 4194304 bytes): seed peer not announced or not used |

## Failure rates

| variant | valid blocks | measured | failures | rate |
|---|---|---|---|---|
| baseline | 0/1 | 0 | 0 | 0.0000 |

## Measurements

| state | file | n | p10 ms | median ms | p90 ms | median MiB/s |
|---|---|---|---|---|---|---|

## Validity checks

- 01-baseline-b1 `fixtures-manifest` ok: 2 files, spec 6a9efa261473012c
- 01-baseline-b1 `drivers-and-metrics-ready` ok: 4 drivers, 5 metrics endpoints, dfget 1.5.14 (8887c145f, 2026-11-13)
- 01-baseline-b1 `seed-announced` FAIL: after 16 attempts: client went back-to-source (metric 4194304 bytes, origin 4194304 bytes): seed peer not announced or not used

Block order: 01-baseline-b1
