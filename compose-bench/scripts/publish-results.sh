#!/usr/bin/env bash
# Ingest results/<run-id>/ into local SQLite (results/dfperf.db) and, when
# SUPABASE_URL (or PROJECT_URL) and SUPABASE_SERVICE_ROLE_KEY are set, upsert
# the same row to Supabase schema dfperf (Accept-Profile / Content-Profile).
# Missing remote config skips the upload and is not a failure. This script's
# exit status must not replace the benchmark's 0/1/2/3/4 exit code —
# compose-bench.sh calls it best-effort.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
exec python3 "$ROOT/scripts/publish-results.py" "$@"
