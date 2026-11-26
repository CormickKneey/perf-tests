#!/usr/bin/env bash
# compose-bench entry point.
#
#   scripts/compose-bench.sh smoke --profile file-3peers --ref v2.5.4+v1.5.14
#   scripts/compose-bench.sh run   --profile file-3peers --baseline <ref> --candidate <ref>
#   scripts/compose-bench.sh compare --run-dir results/<run>
#   scripts/compose-bench.sh preflight|index|clean [--run-id <id>] [--fixtures] [--dry-run]
#
# A ref is `<scheduler-tag>+<client-tag>` (dragonflyoss/* images),
# `scheduler=<image>,client=<image>` (tags or @sha256 digests) or `<tag>`.
#
# After smoke, run, or compare (any verdict, including INVALID) the wrapper
# ingests results/<run-id>/ into local SQLite and, if Supabase secrets are
# set, uploads that run. Upload problems are logged and never replace the
# benchmark exit code.
#
# Exit codes: 0 pass, 1 regression, 2 benchmark failed, 3 infra/invalid,
# 4 inconclusive (priority 3 > 2 > 1 > 4 > 0).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD="$ROOT/.build"

# Read --flag VALUE or --flag=VALUE from a remaining argument list.
parse_flag() {
  local name="$1"
  shift
  while [[ $# -gt 0 ]]; do
    if [[ "$1" == "$name" && $# -ge 2 ]]; then
      printf '%s\n' "$2"
      return 0
    fi
    if [[ "$1" == "$name"=* ]]; then
      printf '%s\n' "${1#"$name"=}"
      return 0
    fi
    shift
  done
  return 1
}

abs_dir() {
  local p="$1"
  if [[ "$p" == /* ]]; then
    printf '%s\n' "$p"
    return 0
  fi
  if [[ -d "$p" ]]; then
    (cd "$p" && pwd)
    return 0
  fi
  if [[ -d "$ROOT/$p" ]]; then
    (cd "$ROOT/$p" && pwd)
    return 0
  fi
  printf '%s\n' "$ROOT/$p"
}

results_root_from_args() {
  local v
  if v="$(parse_flag --results "$@")"; then
    abs_dir "$v"
  else
    printf '%s\n' "$ROOT/results"
  fi
}

run_dir_from_args() {
  local d id results
  if d="$(parse_flag --run-dir "$@")"; then
    printf '%s\n' "$d"
    return 0
  fi
  if id="$(parse_flag --run-id "$@")"; then
    results="$(results_root_from_args "$@")"
    printf '%s\n' "$results/$id"
    return 0
  fi
  return 1
}

snapshot_run_ids() {
  local root="$1" f
  for f in "$root"/*/run.json; do
    [[ -f "$f" ]] || continue
    basename "$(dirname "$f")"
  done
}

discover_new_run_dir() {
  local results_root="$1" snap="$2" dir id
  for dir in "$results_root"/*/; do
    [[ -f "${dir}run.json" ]] || continue
    id="$(basename "$dir")"
    if ! grep -qxF "$id" "$snap" 2>/dev/null; then
      printf '%s\n' "${dir%/}"
    fi
  done | sort | tail -n 1
}

# Always returns 0. A failed ingest/upload must not become the process status.
publish_after_bench() {
  local code="$1"
  local run_dir="${2:-}"
  if [[ -z "$run_dir" ]]; then
    return 0
  fi
  if [[ ! -d "$run_dir" && -d "$ROOT/$run_dir" ]]; then
    run_dir="$ROOT/$run_dir"
  fi
  if [[ ! -d "$run_dir" ]]; then
    return 0
  fi
  if [[ ! -f "$run_dir/run.json" ]]; then
    echo "publish-results: skip (no run.json in $run_dir)" >&2
    return 0
  fi
  if ! "$ROOT/scripts/publish-results.sh" "$run_dir"; then
    echo "publish-results: warning: ingest or upload failed; benchmark exit code ${code} is unchanged" >&2
  fi
  return 0
}

compose_bench_main() {
  local cmd="$1"
  shift

  if [[ -z "$cmd" ]]; then
    sed -n '2,18p' "$ROOT/scripts/compose-bench.sh" | sed 's/^# \{0,1\}//'
    return 3
  fi

  command -v go >/dev/null || { echo "go toolchain is required to build dfbench" >&2; return 3; }
  command -v docker >/dev/null || { echo "docker is required" >&2; return 3; }

  mkdir -p "$BUILD/host"
  local version ldflags linux_bin goarch arch crane crane_args results_root snap run_dir code
  version="$(git -C "$ROOT" describe --always --dirty 2>/dev/null || echo dev)"
  ldflags="-X main.version=$version"
  (cd "$ROOT" && go build -trimpath -ldflags "$ldflags" -o "$BUILD/host/dfbench" ./cmd/dfbench) || return 3
  DFBENCH="$BUILD/host/dfbench"

  case "$cmd" in
    run|smoke)
      arch="$(docker info --format '{{.Architecture}}' 2>/dev/null || true)"
      case "$arch" in
        aarch64|arm64) goarch=arm64 ;;
        x86_64|amd64) goarch=amd64 ;;
        *) echo "cannot determine docker engine architecture (${arch:-unreachable})" >&2; return 3 ;;
      esac
      linux_bin="$BUILD/linux-$goarch/dfbench"
      mkdir -p "$(dirname "$linux_bin")"
      (cd "$ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" go build -trimpath -ldflags "$ldflags" -o "$linux_bin" ./cmd/dfbench) || return 3
      crane_args=()
      crane="${DFPERF_CRANE:-$ROOT/.tools/bin/crane}"
      if [[ -x "$crane" ]]; then
        crane_args=(--crane "$crane")
      fi
      results_root="$(results_root_from_args "$@")"
      snap="$(mktemp)"
      snapshot_run_ids "$results_root" >"$snap"
      set +e
      "$DFBENCH" compose "$cmd" --root "$ROOT" --linux-binary "$linux_bin" ${crane_args[@]+"${crane_args[@]}"} "$@"
      code=$?
      set -e
      run_dir=""
      if run_dir="$(run_dir_from_args "$@")"; then
        :
      else
        run_dir="$(discover_new_run_dir "$results_root" "$snap")"
      fi
      rm -f "$snap"
      publish_after_bench "$code" "$run_dir"
      return "$code"
      ;;
    clean|index|preflight)
      exec "$DFBENCH" compose "$cmd" --root "$ROOT" "$@"
      ;;
    compare)
      set +e
      "$DFBENCH" compare "$@"
      code=$?
      set -e
      run_dir=""
      run_dir="$(run_dir_from_args "$@" || true)"
      publish_after_bench "$code" "$run_dir"
      return "$code"
      ;;
    *)
      echo "unknown command: $cmd" >&2
      return 3
      ;;
  esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  if [[ $# -lt 1 ]]; then
    compose_bench_main ""
    exit 3
  fi
  compose_bench_main "$@"
  exit $?
fi
