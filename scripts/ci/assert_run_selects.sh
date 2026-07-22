#!/usr/bin/env bash
# assert_run_selects.sh — guard against the -run false-green (MAQ-5 / NU7-4).
#
# Several CI lanes execute a hand-picked subset of live tests with an anchored
# alternation, e.g. `go test -run '^TestA$|^TestB$' ./pkg/db`. `go test` exits
# 0 when a -run regex matches NOTHING, so renaming TestA silently drops it from
# CI while the lane stays green — the whole point of pinning those tests is
# lost with no signal. This is the NU7-4 class the directed review flagged.
#
# This guard asserts, at BUILD time via `go test -list` (which compiles the
# (tagged) test binary and prints the names of matching tests WITHOUT running
# them or any TestMain setup — so it needs no live service, even for the DB
# lanes), that every test a -run regex names is still selectable. A renamed or
# removed test makes its branch select nothing and this guard FAILS, naming it,
# before the expensive live steps ever start.
#
# Rationale for the -list approach over parsing `go test -json` Action:"run"
# events: it is decoupled from the execution step (it cannot break a passing
# lane), it runs without the live service so it fails fast and is reproducible
# locally for every lane, and it targets exactly the false-green cause — a
# -run filter that no longer selects its intended test. (A test that is
# selected but self-skips at runtime is a different class, not this guard's
# job; in the lanes the gating env is set, so selected tests run.)
#
# Usage: assert_run_selects.sh <pkg> <run-regex> [extra go test flags...]
#   assert_run_selects.sh ./pkg/db  '^TestSQLMatrix_ConnectAndPing$|^TestSQLMatrix_SchemaDrift$'
#   assert_run_selects.sh ./pkg/model '^TestCRUDLive_' -tags mssql
#
# Regex branches are split on `|`:
#   ^Name$   anchored literal — that exact test must be selectable.
#   ^Prefix  prefix family (no trailing `$`) — >=1 test starting with Prefix
#            must be selectable.
#
# Negative check (prove the guard bites):
#   scripts/ci/assert_run_selects.sh ./pkg/db '^TestThatWasRenamed$'   # -> EXIT 1
set -uo pipefail

pkg="${1:?usage: assert_run_selects.sh <pkg> <run-regex> [go test flags...]}"
regex="${2:?missing <run-regex>}"
shift 2

err_file="$(mktemp)"
trap 'rm -f "$err_file"' EXIT

if ! listing="$(go test -list "$regex" "$@" "$pkg" 2>"$err_file")"; then
  echo "assert_run_selects: 'go test -list' failed to build ${pkg} $*" >&2
  cat "$err_file" >&2
  exit 1
fi

# Keep only test-function name lines; drop the trailing "ok  <pkg> <elapsed>"
# summary and any build chatter. Go requires test functions to start with Test.
selected="$(printf '%s\n' "$listing" | grep -E '^Test' | sort -u || true)"

fail=0
IFS='|' read -ra branches <<< "$regex"
for b in "${branches[@]}"; do
  [ -z "$b" ] && continue
  lit="${b#^}"
  if [[ "$lit" == *'$' ]]; then
    name="${lit%\$}"
    if printf '%s\n' "$selected" | grep -qxF "$name"; then
      echo "ok: '${name}' is selectable in ${pkg}"
    else
      echo "FAIL: -run branch '^${name}\$' selects NO test in ${pkg} $* — renamed or removed?" >&2
      fail=1
    fi
  else
    n="$(printf '%s\n' "$selected" | grep -cE "^${lit}" || true)"
    if [ "${n:-0}" -ge 1 ]; then
      echo "ok: family '${lit}*' selects ${n} test(s) in ${pkg}"
    else
      echo "FAIL: -run family '^${lit}' selects NO test in ${pkg} $* — renamed or removed?" >&2
      fail=1
    fi
  fi
done

if [ "$fail" -ne 0 ]; then
  echo "assert_run_selects: a -run filter no longer selects its intended test(s) (MAQ-5 false-green guard)." >&2
  exit 1
fi
echo "assert_run_selects: all -run branches select their tests in ${pkg}."
