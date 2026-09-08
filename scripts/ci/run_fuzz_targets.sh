#!/usr/bin/env bash
# run_fuzz_targets.sh — the short seeded fuzz lane.
#
# Every fuzz target in this repository is listed here, once. CI runs this
# script; `make fuzz` runs the same script, so the local and the CI lane cannot
# drift.
#
# What it does per target:
#
#   1. Asserts the target is still selectable (assert_run_selects.sh).
#      `go test -fuzz=<regex>` prints "warning: no fuzz tests to fuzz" and
#      exits 0 when the regex matches nothing, so renaming a target would drop
#      it from this lane with no signal — the NU7-4 false-green, in its
#      fuzzing spelling.
#   2. Mutates for FUZZTIME (default 5s) on top of the seed corpus committed
#      under <pkg>/testdata/fuzz/<Target>/.
#
# The seed corpus itself is not this script's job: `go test ./...` replays
# every seed on every run, in the normal test lane, for free and
# deterministically. This script is what turns the targets from a corpus
# replay into actual fuzzing. Budget: 5s x 5 targets plus build ~= 35s.
#
# `go clean -fuzzcache` first, deliberately: CI restores GOCACHE between runs,
# and a corpus entry the restore did not bring back whole aborts the run with
# an unhelpful "no such file or directory". Each run starts from the committed
# corpus and nothing else.
#
# Usage:
#   scripts/ci/run_fuzz_targets.sh            # 5s per target
#   FUZZTIME=60s scripts/ci/run_fuzz_targets.sh   # a longer local hunt
set -euo pipefail

FUZZTIME="${FUZZTIME:-5s}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# <package>:<target>. Add a new target here and it joins both lanes.
targets=(
  "./pkg/router:FuzzMuxRouteMatching"
  "./pkg/router:FuzzCSRFGate"
  "./pkg/router:FuzzRealIPForwarding"
  "./pkg/auth:FuzzJWTValidate"
  "./pkg/model:FuzzSanitizeOrderBy"
)

for spec in "${targets[@]}"; do
  pkg="${spec%%:*}"
  target="${spec##*:}"
  bash "${here}/assert_run_selects.sh" "${pkg}" "^${target}\$"
done

# Best effort: a shared GOCACHE that another build is touching can make the
# clean fail, and that is not a reason to fail the lane.
go clean -fuzzcache || echo "run_fuzz_targets: could not clean the fuzz cache; continuing" >&2

for spec in "${targets[@]}"; do
  pkg="${spec%%:*}"
  target="${spec##*:}"
  echo "--- fuzzing ${target} in ${pkg} for ${FUZZTIME}"
  go test "${pkg}" -run '^$' -fuzz="^${target}\$" -fuzztime="${FUZZTIME}"
done

echo "run_fuzz_targets: ${#targets[@]} targets fuzzed for ${FUZZTIME} each."
