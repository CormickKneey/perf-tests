#!/usr/bin/env bash
# Prove publish_after_bench never replaces the benchmark exit code (0/1/2/3/4).
set -euo pipefail
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=scripts/compose-bench.sh
source "$REPO/scripts/compose-bench.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fake_publish="$tmp/publish-results.sh"
cat >"$fake_publish" <<'SH'
#!/usr/bin/env bash
echo "fake publish failing" >&2
exit 7
SH
chmod +x "$fake_publish"

mini="$tmp/root"
mkdir -p "$mini/scripts" "$mini/results/fakerun"
cp "$fake_publish" "$mini/scripts/publish-results.sh"
echo '{"id":"fakerun","verdict":"INVALID"}' >"$mini/results/fakerun/run.json"

# Point the sourced hook at the stub tree.
ROOT="$mini"
set +e
out="$(publish_after_bench 3 "$mini/results/fakerun" 2>&1)"
hook_rc=$?
set -e

echo "$out"
if [[ "$hook_rc" -ne 0 ]]; then
  echo "FAIL: publish_after_bench returned $hook_rc, expected 0" >&2
  exit 1
fi
if ! grep -q "benchmark exit code 3 is unchanged" <<<"$out"; then
  echo "FAIL: expected unchanged-exit warning" >&2
  exit 1
fi

end="$tmp/e2e.sh"
cat >"$end" <<SH
#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=/dev/null
source "$REPO/scripts/compose-bench.sh"
ROOT="$mini"
code=4
publish_after_bench "\$code" "$mini/results/fakerun"
exit "\$code"
SH
set +e
bash "$end" >/dev/null
e2e_rc=$?
set -e
if [[ "$e2e_rc" -ne 4 ]]; then
  echo "FAIL: e2e exit was $e2e_rc, expected 4 (bench inconclusive), publish must not replace it" >&2
  exit 1
fi

echo "OK: publish failure left bench exit codes 3 and 4 unchanged"
