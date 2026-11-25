<!-- markdownlint-disable MD013 -->

# compose-bench on Docker Compose — design notes

compose-bench is a standalone Dragonfly benchmark. It takes one or two
*variants* (a scheduler image and a client image, each by tag or digest),
starts the whole topology with Docker Compose once per *block*, measures
dfget downloads in four cache states, and turns the raw data into
`report.json`, `report.md` and an exit code. Nothing here depends on
Kubernetes or modifies the Dragonfly repositories.

```sh
scripts/compose-bench.sh smoke --profile file-3peers --ref v2.5.4+v1.5.14
scripts/compose-bench.sh run   --profile file-3peers --baseline <ref> --candidate <ref>
scripts/compose-bench.sh compare --run-dir results/<run>     # re-run the offline statistics
scripts/compose-bench.sh preflight | index | clean [--run-id ID] [--fixtures] [--dry-run]
```

A ref is `<scheduler-tag>+<client-tag>` (expands to `dragonflyoss/scheduler`
and `dragonflyoss/client`), `scheduler=<image>,client=<image>` (tags or
`@sha256:` digests), or a single `<tag>` used for both.

## Upstream facts, verified against real images and source

Everything below was checked on 2026-11-21 against
`dragonflyoss/scheduler:v2.5.4` (index `sha256:b9d27b9d…`, linux/arm64
manifest `sha256:94cf26b8…`) and `dragonflyoss/client:v1.5.14` (index
`sha256:db6490a4…`, linux/arm64 manifest `sha256:d942d337…`), plus the
matching source trees on GitHub.

| SPEC claim | Result | Evidence |
|---|---|---|
| Official compose (dragonfly `d41be19`, `deploy/docker-compose/docker-compose.yaml`) only has scheduler, seed-client, client, uses `latest`, `restart: always`, fixed `container_name` and host ports | **Confirmed.** Configs come from `template/*.template.yaml`, rendered by `run.sh` with one host IP (`__IP__`) and distinct ports per daemon | fetched file; we instead give every peer its own fixed IP and identical ports |
| Since `5523218` (#4895, 2026-07-27) the scheduler runs without manager and reads a local `dynconfig.yaml` | **Confirmed.** `manager.addr` unset → `scheduler/config/dynconfig_local.go` loads `LocalDynconfigData` (`applications`, `seedPeerClusterConfig`, `schedulerClusterConfig`, `schedulerClusterClientConfig`) from `--dynconfig` (default `/etc/dragonfly/dynconfig.yaml`) | `scheduler --help` in the image; source at v2.5.4 |
| `dfget --endpoint` is the local unix socket | **Confirmed** (`-e, --endpoint`, default `/var/run/dragonfly/dfdaemon.sock`) | `dfget --help` in the image |
| `host.ip` is an `IpAddr`, no hostnames → fixed IP per peer | **Confirmed** (`pub ip: Option<IpAddr>` in `dragonfly-client-config/src/dfdaemon.rs`) | source at v1.5.14 |
| seed peer announces as `type: super` | **Confirmed**: `seedPeer.type` is `HostType` = `normal` \| `super`; the scheduler's `dragonfly_scheduler_traffic{host_type="super"}` and `download_peer_back_to_source_started_total{host_type="super"}` show the seed doing the back-to-source | metrics captured in every block (`metrics/after/scheduler.prom`) |
| dfdaemon without manager | `dfdaemon --dynconfig` ("used when the manager address is not configured"); local dynconfig `scheduler.addrs` (static list, takes precedence over `addr`) | `dfdaemon --help`; `dragonfly-client/src/dynconfig/local.rs` |

### Corrections to the review draft (and why)

1. **`--transfer-from-dfdaemon=false` is invalid.** In dfget 1.5.14 it is a
   bare boolean flag; clap rejects a value:
   `error: unexpected value 'false' for '--transfer-from-dfdaemon' found`.
   We simply omit it. Its default (false) means *dfdaemon* hard-links or
   copies the finished file to `--output`, so the output path must exist on
   a volume the daemon can see. Each driver therefore mounts the daemon's
   storage volume at the same path (`/var/lib/dragonfly`) and writes to
   `/var/lib/dragonfly/dfperf-out/`; the file is hashed after the timed
   region and deleted.
2. **`--scheduling-policy always` is required.** dfget 1.5.14 defaults to
   `auto`, which "downloads small files from the source directly". With the
   small fixtures we use on a Mac this would bypass the seed and break the
   "cold has 0 client back-to-source bytes" rule, so the profile pins
   `DFPERF_SCHEDULING_POLICY=always`.
3. **Clients probe the origin even on cold-from-seed downloads.** Before
   scheduling, dfdaemon sends one `Range: bytes=0-0` request (1 body byte)
   to learn the content length. The nginx access log shows it for every
   peer. "Client back-to-source bytes" is therefore defined as
   `dragonfly_client_download_traffic{type="BACK_TO_SOURCE"}` of the client
   **and** origin piece bytes (non-probe `body_bytes_sent`) attributed to
   the client's IP. Probes are counted separately (`origin_probes`).
4. **Scheduler image version label.** `dragonflyoss/scheduler:v2.5.4`
   reports `GitVersion: v2.5.0`, `GitCommit: 5ace19f` from `scheduler
   version`. Reports record the requested ref, the pinned digest and this
   self-reported version side by side.
5. **Wrong scheduler address on the seed is caught before measuring.**
   With `seed-wrong-scheduler` dfdaemon exits with `Error: available
   schedulers not found`, so `up --wait` fails (block `infra-error`, run
   exit 3). With `broken-seed` (`seedPeer.enable: false`) everything starts
   but the warmup sees the client go back-to-source (4 MiB via metrics and
   origin log), so the block is `invalid` (exit 3).

### Environment-specific workarounds on this Mac

* **Registry access.** The OrbStack engine is configured with the
  `docker.m.daocloud.io` mirror, which answered `Bad Gateway` for every
  manifest (docker.io and ghcr.io alike), while the host itself can reach the
  registries. `Images.Resolve` therefore tries the local image, then
  `docker pull`, then — if `.tools/bin/crane` exists — `crane pull
  --format=oci --platform linux/{arch}` + `docker load`. crane runs with an
  empty project-local `DOCKER_CONFIG`, so no registry credentials are read.
  With the containerd image store the loaded image keeps its manifest digest,
  so `repo@sha256:…` resolves locally and is what compose uses.
* **BuildKit cannot `FROM` a local-only digest** (it insists on resolving
  metadata remotely). Sidecar images are built with `docker create` +
  `docker cp` + `docker commit` instead: the base layers stay byte-identical
  to the client digest under test and the extra layer only adds
  `/opt/dfperf/dfbench`. The derived tag encodes both the base image id and
  the dfbench sha256.
* **nginx:stable-alpine has no openssl**, so fixtures are generated by
  `dfbench fixtures` (ChaCha8 stream keyed by sha256(generator|seed|size)).

## Topology (one block = one compose project)

| service | image | IP | role |
|---|---|---|---|
| origin | nginx (pinned digest) | `.5` | serves `/files/*` from the fixtures volume (read-only); JSON access log on a volume |
| scheduler | scheduler under test | `.10` | no manager, local dynconfig, metrics `:8000` |
| seed-client | client under test | `.20` | dfdaemon, `seedPeer: {enable: true, type: super}` |
| client-1..3 | client under test | `.21–.23` | dfdaemon, normal peers |
| driver-seed, driver-1..3 | client digest + dfbench | `.30–.33` | share the daemon's socket and storage volumes; HTTP API runs dfget |
| fixtures | tools | none | one-shot: generates files + `manifest.json` + `SHA256SUMS` (idempotent) |
| runner (`tools` profile) | tools | `.40` | runs the measurement plan; no docker.sock |
| compare (`tools` profile) | tools | none | offline comparison of a run directory |

The subnet is `DFPERF_NET_PREFIX.0/24` (default `10.213.0.0/24`). No
service publishes host ports, every service has `restart: "no"`, nothing
mounts `docker.sock`, and there are no fixed container names.

**Why HTTP drivers?** The runner must not have `docker.sock`, so it cannot
`docker exec` into containers. Each driver is a sidecar built from the exact
client digest (so dfget always matches dfdaemon), shares the daemon's unix
socket, and exposes `POST /v1/download`. The runner calls drivers over the
block network, scrapes `/metrics` of every daemon and the scheduler before
and after each download, and tails the origin's access log.

## Block lifecycle

1. `docker compose config` → `blocks/<id>/compose.rendered.yaml` (archived).
2. `up -d --wait` (the fixtures job must complete, every daemon must pass
   `grpc_health_probe` on its socket, drivers must answer `/v1/healthz`).
3. `run --rm runner`:
   * fixtures manifest present, drivers and metrics endpoints reachable;
   * **seed-announced**: a fresh warmup task on client-1 must make the super
     seed go back-to-source while client-1 fetches 0 piece bytes from origin
     (retried for `DFPERF_WARMUP_TIMEOUT` seconds);
   * per file and sample `i` (peers rotate `a = client[i%3]`, `b = client[(i+1)%3]`):
     * `cold` — new tag on `a`;
     * `seed-warm` — driver-seed downloads the tag first, then `b`;
     * `peer-warm` — `a` downloads (prep), then `b`;
     * `local-hit` — `b` downloads the same tag again;
   * **cold-client-back-to-source-zero** over every cold sample (required);
   * local-hit served from `LOCAL_PEER` (informational).
4. collect `ps`, `images` (resolved image IDs per container), `logs`.
5. `down -v --remove-orphans`, then verify no container, volume or network
   with that project label remains.

A different tag per sample gives a new task id for the same URL, so every
sample starts from a defined cache state without restarting anything.

Blocks run in ABBA order (`B C C B B C C B` for four blocks per variant).
The run stops at the first block that is not `ok`; the result is then
invalid (exit 3) anyway.

## Fixed settings

* dfdaemon GC: `interval 3600s`, `taskTTL 86400s`, disk thresholds 95/90 —
  no eviction inside a block; `storage.keep: false` on fresh volumes.
* scheduler GC: `peerTTL 24h`, `taskGCInterval 30m`, `hostTTL 1h`; `GOGC=100`.
* dfdaemon/scheduler log to stdout, captured in `logs/compose.log`.

## Safety rules

* Project names must match `^dfperf-[0-9a-z]{8,}-(baseline|candidate)-b[0-9]+$`;
  `down` and `clean` validate the name first and refuse anything else.
* The fixtures volume `dfperf-fixtures-<16 hex>` is `external`, so `down -v`
  cannot remove it; `clean --fixtures` validates
  `^dfperf-fixtures-[0-9a-f]{12,64}$` before `docker volume rm`.
* `clean` removes leftovers by compose project label only (containers,
  `<project>_*` volumes, networks). `docker system prune` is never used.
* Preflight (exit 3 on failure): docker reachable, compose ≥ 2.24, free disk
  on the results filesystem and inside the engine, no network or host
  interface overlapping the subnet, no stale dfperf containers. It also
  classifies the platform: only `linux-native` engines are baseline-eligible;
  Docker Desktop, OrbStack and other VM-backed engines produce
  **non-baseline** results (`run.json: non_baseline: true`, banner in
  `report.md` and in the dashboard).

## Statistics and exit codes

`compose/compare.yaml` is the policy. One group = (cache state, file).
Measured downloads of all valid blocks of a variant are pooled.

* two-sided Mann–Whitney U (exact without ties up to 50 observations,
  otherwise tie-corrected normal approximation), Holm-corrected over groups;
* **regression**: `p_adj < 0.05` and candidate median slower by > 10 % and
  > 5 ms;
* **inconclusive**: slower by more than the threshold but not significant,
  or fewer than 8 samples per side;
* bootstrap 95 % CI of `median(c)/median(b)` is reported (informational);
* failure rate (failed measured + prep downloads, including sha256
  mismatches) must stay below 0.01 per variant (perf-tests rule).

| exit | verdict | when |
|---|---|---|
| 0 | PASS | all groups pass |
| 1 | REGRESSION | at least one group regressed |
| 2 | FAILED | failure rate ≥ 0.01 |
| 3 | INVALID | preflight / image / compose failure, invalid block, too few valid blocks (< 4 per variant) |
| 4 | INCONCLUSIVE | at least one group inconclusive |

With several outcomes the priority is 3 > 2 > 1 > 4 > 0.

## Results layout

```text
results/index.json                      # all runs, read by the dashboard
results/<run>/run.json                  # refs, digests, host, block order, verdict
results/<run>/preflight.json
results/<run>/report.json | report.md | exit_code | console.log
results/<run>/compose.yaml compare.yaml profile.env
results/<run>/config/<variant>/*.yaml   # rendered dfdaemon/scheduler configs
results/<run>/blocks/<NN-variant-bK>/
    block.json plan.json runner.json runner.log samples.jsonl
    compose.env compose.rendered.yaml images.json ps.jsonl
    metrics/{before,after}/{scheduler,seed-client,client-N}.prom
    logs/{up,compose,runner.stdout,down}.log
```

JSON schemas live in `schemas/`; `dfbench validate results` checks a tree.

## Observations from the Mac runs (non-baseline)

* `cold` takes ≈1.0–1.3 s for both 4 MiB and 64 MiB, while `seed-warm`
  takes ≈60–110 ms. The constant ≈1 s looks like a scheduling retry round
  (the scheduler's `retryInterval` is 1 s) while the seed is triggered —
  this is an unverified hypothesis worth checking on Linux.
* The first A/A run ended **INCONCLUSIVE** (exit 4): `seed-warm` 64 MiB
  candidate median was 20.7 % slower with `p_adj = 1.0`. That is exactly the
  case the inconclusive rule exists for: noise on a VM-backed engine, no
  evidence of a regression.

## Not in the MVP

manager, MySQL, Redis; profiles `proxy` (k6), `obs` (Prometheus/Grafana),
`image` (Linux only, privileged), `manager`, and a configurable number of
peers. The topology code (`pkg/compose/render.go: Topology`) and compose file
are where N peers would be generated.
