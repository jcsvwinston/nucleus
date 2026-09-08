#!/usr/bin/env bash
# run_fuzz_targets.sh — the fuzz lanes, in two modes over one list of targets.
#
# Every fuzz target in this repository is listed here, once, so the PR lane,
# the scheduled lane and `make fuzz` cannot drift from each other.
#
#   --seeds   Replay the committed seed corpora and nothing else. No mutation,
#             no fuzz instrumentation, so it reuses the ordinary test binaries
#             the `go test ./...` step already built. This is what runs on
#             every PR.
#   --fuzz    Mutate for FUZZTIME (default 5s) per target on top of those
#             seeds. This is what runs weekly and what `make fuzz` runs.
#             Default when no mode is given.
#
# Why the PR lane is --seeds and not a short --fuzz run, measured rather than
# assumed: `go test -fuzz` compiles the package and its dependencies with fuzz
# instrumentation, which is a build variant nothing else in the job produces.
# On a GitHub runner that cost was 44s for pkg/router and 36s for pkg/auth
# (pkg/model came in at 2s, riding on the two before it) against 28s of actual
# fuzzing — 116s in total, over the one-minute budget for this lane. The
# instrumented build is not amortised between runs either: actions/setup-go
# keys its cache on go.sum, so an unchanged go.sum means the cache is restored
# but never re-saved with the new entries. --seeds costs a few seconds, is
# deterministic, and still runs every property in this repository against every
# input that ever broke one. Mutation happens in .github/workflows/fuzz.yml,
# weekly and on demand, where a real budget is affordable.
#
# Both modes first assert that every target is still selectable
# (assert_run_selects.sh). `go test -run` and `go test -fuzz` share a
# false-green: both print a warning and exit 0 when their regex matches
# nothing, so a renamed target would drop out of both lanes with no signal —
# the NU7-4 class, in its fuzzing spelling.
#
# Usage:
#   scripts/ci/run_fuzz_targets.sh --seeds        # the PR lane
#   scripts/ci/run_fuzz_targets.sh --fuzz         # 5s per target
#   FUZZTIME=60s scripts/ci/run_fuzz_targets.sh --fuzz   # a real hunt
set -euo pipefail

FUZZTIME="${FUZZTIME:-5s}"
mode="${1:---fuzz}"
case "$mode" in
  --seeds|--fuzz) ;;
  *) echo "usage: run_fuzz_targets.sh [--seeds|--fuzz]" >&2; exit 2 ;;
esac
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# <package>:<target>. Add a new target here and it joins every lane.
targets=(
  "./pkg/router:FuzzMuxRouteMatching"
  "./pkg/router:FuzzCSRFGate"
  "./pkg/router:FuzzRealIPForwarding"
  "./pkg/auth:FuzzJWTValidate"
  "./pkg/model:FuzzSanitizeOrderBy"
)

# The packages in first-appearance order. No associative array: bash 3.2 is
# still what /usr/bin/env bash resolves to on a stock macOS, and every other
# guard in this directory runs there.
packages=""
for spec in "${targets[@]}"; do
  pkg="${spec%%:*}"
  case " ${packages} " in
    *" ${pkg} "*) ;;
    *) packages="${packages} ${pkg}" ;;
  esac
done

# The anchored alternation of the targets one package holds:
# "^FuzzA$|^FuzzB$", which is both a -run filter and the guard's argument.
alternation_for() {
  want="$1"
  out=""
  for spec in "${targets[@]}"; do
    pkg="${spec%%:*}"
    target="${spec##*:}"
    [ "$pkg" = "$want" ] || continue
    if [ -z "$out" ]; then
      out="^${target}\$"
    else
      out="${out}|^${target}\$"
    fi
  done
  printf '%s' "$out"
}

for pkg in ${packages}; do
  bash "${here}/assert_run_selects.sh" "${pkg}" "$(alternation_for "${pkg}")"
done

case "$mode" in
  --seeds)
    # -count=1 on purpose: the `go test ./...` step earlier in the same job
    # already ran these, so without it Go would report a cached result and
    # execute nothing. -v names each corpus file, which is what makes this
    # step readable in the log — a reviewer sees exactly which inputs ran.
    for pkg in ${packages}; do
      echo "--- replaying the seed corpora of ${pkg}"
      go test "${pkg}" -run "$(alternation_for "${pkg}")" -count=1 -v
    done
    echo "run_fuzz_targets: ${#targets[@]} seed corpora replayed."
    ;;
  --fuzz)
    # Best effort: a shared GOCACHE that another build is touching can make
    # the clean fail, and that is not a reason to fail the lane. Cleaning is
    # deliberate — CI restores GOCACHE between runs, and a corpus entry the
    # restore did not bring back whole aborts the run with an unhelpful
    # "no such file or directory".
    go clean -fuzzcache || echo "run_fuzz_targets: could not clean the fuzz cache; continuing" >&2
    for spec in "${targets[@]}"; do
      pkg="${spec%%:*}"
      target="${spec##*:}"
      echo "--- fuzzing ${target} in ${pkg} for ${FUZZTIME}"
      go test "${pkg}" -run '^$' -fuzz="^${target}\$" -fuzztime="${FUZZTIME}"
    done
    echo "run_fuzz_targets: ${#targets[@]} targets fuzzed for ${FUZZTIME} each."
    ;;
esac
