# d7y-ci-infra compare report — 20261121t02532701c3

**Verdict: INCONCLUSIVE** (exit code 4)

> **non-baseline**: orbstack engine on darwin (VM-backed, Docker-Desktop-class); results are non-baseline functional smoke data. Results are a functional smoke signal only; formal baselines require a fixed Linux host.

## Versions

| variant | component | ref | pinned | version |
|---|---|---|---|---|
| baseline | scheduler | `dragonflyoss/scheduler:v2.5.4` | `dragonflyoss/scheduler@sha256:94cf26b861c4` | v2.5.0 (5ace19f) |
| baseline | client | `dragonflyoss/client:v1.5.14` | `dragonflyoss/client@sha256:d942d337b7c6` | dfget 1.5.14 (8887c145f, 2026-11-13) |
| baseline | driver | `dragonflyoss/client@sha256:d942d337b7c6605b5df451d445ade7d5e45b325db75ed62c8cb07998b3c93d04` | `dfperf-driver:d942d337b7c6-030dec9133ad` | dfget 1.5.14 (8887c145f, 2026-11-13) |
| candidate | scheduler | `dragonflyoss/scheduler:v2.5.4` | `dragonflyoss/scheduler@sha256:94cf26b861c4` | v2.5.0 (5ace19f) |
| candidate | client | `dragonflyoss/client:v1.5.14` | `dragonflyoss/client@sha256:d942d337b7c6` | dfget 1.5.14 (8887c145f, 2026-11-13) |
| candidate | driver | `dragonflyoss/client@sha256:d942d337b7c6605b5df451d445ade7d5e45b325db75ed62c8cb07998b3c93d04` | `dfperf-driver:d942d337b7c6-030dec9133ad` | dfget 1.5.14 (8887c145f, 2026-11-13) |

## Environment

- host: mini-24.local (darwin, arm64, 10 CPUs)
- docker: 29.4.0 on OrbStack (kernel 7.0.5-orbstack-00330-ge3df4e19b0a0-dirty), compose 5.1.2
- classification: **orbstack**, baseline eligible: false
- profile: `file-3peers`, fixtures volume `dfperf-fixtures-6a9efa261473012c`
- git: df6a69338ed5

## Blocks (execution order)

| # | block | status | measured | failures | notes |
|---|---|---|---|---|---|
| 1 | 01-baseline-b1 | ok | 24 | 0 |  |
| 2 | 02-candidate-b1 | ok | 24 | 0 |  |
| 3 | 03-candidate-b2 | ok | 24 | 0 |  |
| 4 | 04-baseline-b2 | ok | 24 | 0 |  |
| 5 | 05-baseline-b3 | ok | 24 | 0 |  |
| 6 | 06-candidate-b3 | ok | 24 | 0 |  |
| 7 | 07-candidate-b4 | ok | 24 | 0 |  |
| 8 | 08-baseline-b4 | ok | 24 | 0 |  |

## Failure rates

| variant | valid blocks | measured | failures | rate |
|---|---|---|---|---|
| baseline | 4/4 | 96 | 0 | 0.0000 |
| candidate | 4/4 | 96 | 0 | 0.0000 |

## Comparison (duration ms, lower is better)

Mann–Whitney U two-sided, Holm-corrected; regression needs p_adj < 0.05 and Δ > 10% and > 5 ms.

| state | file | n (b/c) | median b | median c | Δ | ratio 95% CI | p | p_adj | verdict |
|---|---|---|---|---|---|---|---|---|---|
| cold | f-4mib.bin | 12/12 | 1057.3 | 1122.9 | 6.2% | 0.94–1.21 | 0.266 | 1.000 | **pass** |
| seed-warm | f-4mib.bin | 12/12 | 62.0 | 69.2 | 11.7% | 0.71–1.77 | 0.630 | 1.000 | **inconclusive** |
| peer-warm | f-4mib.bin | 12/12 | 176.3 | 183.5 | 4.1% | 0.93–1.15 | 0.478 | 1.000 | **pass** |
| local-hit | f-4mib.bin | 12/12 | 3.9 | 3.3 | -15.9% | 0.70–1.27 | 0.799 | 1.000 | **pass** |
| cold | f-64mib.bin | 12/12 | 1097.2 | 1085.7 | -1.0% | 0.82–1.16 | 0.887 | 1.000 | **pass** |
| seed-warm | f-64mib.bin | 12/12 | 102.8 | 97.8 | -4.8% | 0.72–1.19 | 0.755 | 1.000 | **pass** |
| peer-warm | f-64mib.bin | 12/12 | 178.6 | 183.6 | 2.8% | 0.93–1.20 | 0.478 | 1.000 | **pass** |
| local-hit | f-64mib.bin | 12/12 | 2.2 | 2.2 | -0.6% | 0.94–1.07 | 0.977 | 1.000 | **pass** |

## Validity checks

- 01-baseline-b1 `fixtures-manifest` ok: 2 files, spec 6a9efa261473012c
- 01-baseline-b1 `drivers-and-metrics-ready` ok: 4 drivers, 5 metrics endpoints, dfget 1.5.14 (8887c145f, 2026-11-13)
- 01-baseline-b1 `seed-announced` ok: attempt 1: seed back-to-source 4194304 bytes, client-1 remote-peer 4194304 bytes, client back-to-source 0
- 01-baseline-b1 `cold-client-back-to-source-zero` ok: 6 cold samples, 0 with client back-to-source bytes 
- 01-baseline-b1 `local-hit-served-locally` ok (informational): 6 local-hit samples, 0 not fully local
- 02-candidate-b1 `fixtures-manifest` ok: 2 files, spec 6a9efa261473012c
- 02-candidate-b1 `drivers-and-metrics-ready` ok: 4 drivers, 5 metrics endpoints, dfget 1.5.14 (8887c145f, 2026-11-13)
- 02-candidate-b1 `seed-announced` ok: attempt 1: seed back-to-source 4194304 bytes, client-1 remote-peer 4194304 bytes, client back-to-source 0
- 02-candidate-b1 `cold-client-back-to-source-zero` ok: 6 cold samples, 0 with client back-to-source bytes 
- 02-candidate-b1 `local-hit-served-locally` ok (informational): 6 local-hit samples, 0 not fully local
- 03-candidate-b2 `fixtures-manifest` ok: 2 files, spec 6a9efa261473012c
- 03-candidate-b2 `drivers-and-metrics-ready` ok: 4 drivers, 5 metrics endpoints, dfget 1.5.14 (8887c145f, 2026-11-13)
- 03-candidate-b2 `seed-announced` ok: attempt 1: seed back-to-source 4194304 bytes, client-1 remote-peer 4194304 bytes, client back-to-source 0
- 03-candidate-b2 `cold-client-back-to-source-zero` ok: 6 cold samples, 0 with client back-to-source bytes 
- 03-candidate-b2 `local-hit-served-locally` ok (informational): 6 local-hit samples, 0 not fully local
- 04-baseline-b2 `fixtures-manifest` ok: 2 files, spec 6a9efa261473012c
- 04-baseline-b2 `drivers-and-metrics-ready` ok: 4 drivers, 5 metrics endpoints, dfget 1.5.14 (8887c145f, 2026-11-13)
- 04-baseline-b2 `seed-announced` ok: attempt 1: seed back-to-source 4194304 bytes, client-1 remote-peer 4194304 bytes, client back-to-source 0
- 04-baseline-b2 `cold-client-back-to-source-zero` ok: 6 cold samples, 0 with client back-to-source bytes 
- 04-baseline-b2 `local-hit-served-locally` ok (informational): 6 local-hit samples, 0 not fully local
- 05-baseline-b3 `fixtures-manifest` ok: 2 files, spec 6a9efa261473012c
- 05-baseline-b3 `drivers-and-metrics-ready` ok: 4 drivers, 5 metrics endpoints, dfget 1.5.14 (8887c145f, 2026-11-13)
- 05-baseline-b3 `seed-announced` ok: attempt 1: seed back-to-source 4194304 bytes, client-1 remote-peer 4194304 bytes, client back-to-source 0
- 05-baseline-b3 `cold-client-back-to-source-zero` ok: 6 cold samples, 0 with client back-to-source bytes 
- 05-baseline-b3 `local-hit-served-locally` ok (informational): 6 local-hit samples, 0 not fully local
- 06-candidate-b3 `fixtures-manifest` ok: 2 files, spec 6a9efa261473012c
- 06-candidate-b3 `drivers-and-metrics-ready` ok: 4 drivers, 5 metrics endpoints, dfget 1.5.14 (8887c145f, 2026-11-13)
- 06-candidate-b3 `seed-announced` ok: attempt 1: seed back-to-source 4194304 bytes, client-1 remote-peer 4194304 bytes, client back-to-source 0
- 06-candidate-b3 `cold-client-back-to-source-zero` ok: 6 cold samples, 0 with client back-to-source bytes 
- 06-candidate-b3 `local-hit-served-locally` ok (informational): 6 local-hit samples, 0 not fully local
- 07-candidate-b4 `fixtures-manifest` ok: 2 files, spec 6a9efa261473012c
- 07-candidate-b4 `drivers-and-metrics-ready` ok: 4 drivers, 5 metrics endpoints, dfget 1.5.14 (8887c145f, 2026-11-13)
- 07-candidate-b4 `seed-announced` ok: attempt 1: seed back-to-source 4194304 bytes, client-1 remote-peer 4194304 bytes, client back-to-source 0
- 07-candidate-b4 `cold-client-back-to-source-zero` ok: 6 cold samples, 0 with client back-to-source bytes 
- 07-candidate-b4 `local-hit-served-locally` ok (informational): 6 local-hit samples, 0 not fully local
- 08-baseline-b4 `fixtures-manifest` ok: 2 files, spec 6a9efa261473012c
- 08-baseline-b4 `drivers-and-metrics-ready` ok: 4 drivers, 5 metrics endpoints, dfget 1.5.14 (8887c145f, 2026-11-13)
- 08-baseline-b4 `seed-announced` ok: attempt 1: seed back-to-source 4194304 bytes, client-1 remote-peer 4194304 bytes, client back-to-source 0
- 08-baseline-b4 `cold-client-back-to-source-zero` ok: 6 cold samples, 0 with client back-to-source bytes 
- 08-baseline-b4 `local-hit-served-locally` ok (informational): 6 local-hit samples, 0 not fully local

Block order: 01-baseline-b1 → 02-candidate-b1 → 03-candidate-b2 → 04-baseline-b2 → 05-baseline-b3 → 06-candidate-b3 → 07-candidate-b4 → 08-baseline-b4
