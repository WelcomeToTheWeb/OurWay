#!/usr/bin/env bash
#
# 2.2-0 — coverage ratchet.
#
# Measures total statement coverage for the two Go modules and refuses a
# decrease below the recorded baseline (coverage.baseline.json). The number is
# committed on purpose: coverage is only useful as a trend you cannot silently
# undo, and the release gate (docs/roadmap-2.x.md) says the harness must be able
# to fail a change.
#
#   scripts/coverage.sh              check against the baseline (gate)
#   scripts/coverage.sh --record    write the current numbers into the baseline
#
# Raising the baseline is the expected direction: when you add tests, run
# --record and commit the new file with the change that earned it.
#
# COVERAGE_GO_TEST_ARGS is appended to `go test` (e.g. -skip TestCollectorManager|TestDiskCollector
# in a container with no real disk partitions, where that test cannot pass). The
# gate runs without it — and skipping a package that way lowers the measured
# number, so an agent figure below baseline in a container is expected, not a
# finding.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

MODE="${1:---check}"
BASELINE="coverage.baseline.json"
TMP=$(mktemp -d)
trap "rm -rf \"$TMP\"" EXIT

if [ ! -f "$BASELINE" ]; then
  echo "coverage: missing $BASELINE — run scripts/coverage.sh --record once, then commit it" >&2
  exit 1
fi

measure() { # $1 = module directory
  local dir="$1" profile
  local -a extra=()
  if [ -n "${COVERAGE_GO_TEST_ARGS:-}" ]; then
    # Word-split, quoted on use: a -skip regex containing "|" must survive as
    # one argument, which an unquoted expansion does not guarantee.
    read -r -a extra <<<"$COVERAGE_GO_TEST_ARGS"
  fi
  profile="$(pwd)/.coverage-$(basename "$dir").out"
  if ! ( cd "$dir" && go test -coverprofile="$profile" ${extra[@]+"${extra[@]}"} ./... \
      >"$TMP/$(basename "$dir").log" 2>&1 ); then
    echo "coverage: tests failed in $dir — fix the tests before measuring coverage" >&2
    tail -30 "$TMP/$(basename "$dir").log" >&2
    exit 1
  fi
  # go tool cover resolves package paths against the module, so it must run there.
  ( cd "$dir" && go tool cover -func="$profile" ) | awk '/^total:/ { print $3 }' | tr -d '%'
}

SERVER=$(measure server)
AGENT=$(measure agent)

python3 - "$BASELINE" "$SERVER" "$AGENT" "$MODE" <<'PY'
import json, sys, os

baseline_path, server, agent, mode = sys.argv[1], float(sys.argv[2]), float(sys.argv[3]), sys.argv[4]
measured = {"server": server, "agent": agent}

if mode == "--record":
    with open(baseline_path, "w") as fh:
        json.dump({k: round(v, 2) for k, v in sorted(measured.items())}, fh, indent=2, sort_keys=True)
        fh.write("\n")
    print(f"coverage: baseline recorded — server {server:.2f}%, agent {agent:.2f}%")
    print("coverage: commit coverage.baseline.json with the change that earned it")
    sys.exit(0)

baseline = json.load(open(baseline_path))
failed = False
for module, value in sorted(measured.items()):
    floor = baseline.get(module)
    if floor is None:
        print(f"coverage: {module} has no baseline entry — run --record")
        failed = True
        continue
    status = "ok" if value >= floor else "BELOW BASELINE"
    print(f"coverage: {module:6} {value:5.2f}%  (baseline {floor:.2f}%)  {status}")
    if value < floor:
        failed = True

if failed:
    print()
    print("Coverage dropped below the recorded baseline. Coverage is a floor, not a")
    print("suggestion: add the test that covers the code you changed. If a package")
    print("genuinely moved out of the tested set (code deleted, moved behind a build")
    print("tag), say so in the commit message and adjust coverage.baseline.json there.")
    sys.exit(1)
PY
