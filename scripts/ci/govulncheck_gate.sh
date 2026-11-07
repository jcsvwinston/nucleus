#!/usr/bin/env bash
# govulncheck_gate.sh — run govulncheck on a module and fail on every
# vulnerability the module's code CALLS, except the ones a dated, id-scoped
# exception tolerates while the Go vulnerability database lags the fix.
#
# govulncheck decides from two inputs: the module graph in front of it and the
# report the database publishes for each id. When upstream ships the fix
# before the database records it as `fixed`, the scan keeps reporting the
# fixed version — "Found in: module@vX.Y.Z, Fixed in: N/A" — and a lane that
# runs the bare binary stays red with no code change that could turn it
# green. The first such case was GO-2026-6909 in github.com/crewjam/saml:
# fixed upstream in v0.5.2 (commit 400b0e48aad1), the module pinned to it,
# its tests passing, and the SAML lane red for the report alone.
#
# This script keeps the scan AND makes the exception explicit, narrow and
# self-retiring:
#
#   - Narrow: a line in the exceptions file names ONE id, ONE module and ONE
#     version. A bump of the module, or the same id reaching the code through
#     another module, is not covered.
#   - Dated: the line carries the day it expires. From that day the finding
#     fails again, so the exception has to be re-argued, not forgotten.
#   - Self-retiring: the exception applies only while the database records
#     NO fixed version for that id and module. The day the report gains a
#     `fixed` event, govulncheck either stops reporting the pinned version
#     (fixed ≤ pinned) — and the line is flagged stale, to be deleted — or
#     keeps reporting it (fixed > pinned) — and the finding fails, because
#     the database now says the pin is short.
#
# What still fails, exception or not: every called vulnerability without a
# matching line, and every line that has expired. What never fails: findings
# at module or package level that the code does not call — the same policy
# as govulncheck's own exit status.
#
# Usage (run from the module directory, the way the lanes do):
#   bash scripts/ci/govulncheck_gate.sh <exceptions-file> [govulncheck args...]
#   bash ../../scripts/ci/govulncheck_gate.sh ../../scripts/ci/govulncheck_exceptions.txt ./...
#
# Exceptions file: one exception per line, whitespace-separated —
#   <osv-id> <module> <version> <expires YYYY-MM-DD> <reason, free text>
# Blank lines and `#` comments are ignored.
#
# The govulncheck version is pinned (GOVULNCHECK_VERSION) for the reason
# .github/workflows/ci.yml gives at its first `Run govulncheck` step; the
# database is fetched at run time, so a pinned binary still scans against
# current advisories.
#
# Negative check (prove the gate bites): record a scan once
# (`govulncheck -json ./... > scan.json`) and judge it offline with
# GOVULNCHECK_GATE_SCAN=scan.json — against an empty exceptions file, and
# against a copy of the line with its date in the past: both exit 1 naming
# the id. A line that matches nothing only warns, and says to delete it.

set -euo pipefail

GOVULNCHECK_VERSION="${GOVULNCHECK_VERSION:-v1.3.0}"

if [ "$#" -lt 1 ]; then
  echo "usage: $0 <exceptions-file> [govulncheck args...]" >&2
  exit 2
fi
exceptions="$1"
shift
if [ ! -f "$exceptions" ]; then
  echo "::error::govulncheck gate: exceptions file not found: $exceptions" >&2
  exit 2
fi
command -v jq >/dev/null || { echo "::error::govulncheck gate: jq is required" >&2; exit 2; }

today="$(date -u +%F)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# One scan, in JSON, so the verdict and the report come from the same run.
# govulncheck exits 0 in -json mode whatever it finds; the verdict is ours.
# GOVULNCHECK_GATE_SCAN names a recorded scan to judge instead of running one
# — for the negative checks, which should not need the network to bite.
if [ -n "${GOVULNCHECK_GATE_SCAN:-}" ]; then
  cp "$GOVULNCHECK_GATE_SCAN" "$work/scan.json"
else
  go run "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}" -json "$@" > "$work/scan.json"
fi

# The report for each id, reduced to what the gate needs: for every affected
# module, whether the database records any fixed version at all.
jq -c 'select(.osv) | .osv as $o
       | $o.affected[]
       | {id: $o.id, module: .package.name,
          fixed: ([.ranges[]?.events[]? | select(.fixed)] | length > 0)}' \
  "$work/scan.json" > "$work/osv.jsonl"

# The findings the module's own code calls (a trace with a function), one
# line per (id, module, version), with the first call site for the log.
jq -c 'select(.finding) | .finding
       | select(.trace[0].function)
       | {id: .osv, module: .trace[0].module, version: .trace[0].version,
          call: ((.trace | last | "\(.package).\(.function)") + " -> " +
                 "\(.trace[0].package).\(.trace[0].function)")}' \
  "$work/scan.json" \
  | jq -s -c 'group_by([.id, .module, .version]) | map(first) | .[]' \
  > "$work/called.jsonl"

fixed_recorded() { # <id> <module> → exit 0 when the database records a fixed version
  jq -e --arg id "$1" --arg m "$2" \
    'select(.id == $id and .module == $m and .fixed)' "$work/osv.jsonl" >/dev/null 2>&1
}

failures=0
tolerated=0
declare -a used=()

while IFS= read -r line; do
  id="$(jq -r .id <<<"$line")"
  module="$(jq -r .module <<<"$line")"
  version="$(jq -r .version <<<"$line")"
  call="$(jq -r .call <<<"$line")"
  key="$id $module $version"

  # The matching exception, if any: same id, module and version.
  exc="$(grep -E -v '^[[:space:]]*(#|$)' "$exceptions" \
        | awk -v id="$id" -v m="$module" -v v="$version" '$1==id && $2==m && $3==v {print; exit}' || true)"
  if [ -z "$exc" ]; then
    echo "::error::govulncheck gate: $id is called by this module ($module@$version: $call) and has no exception — see https://pkg.go.dev/vuln/$id"
    failures=$((failures + 1))
    continue
  fi
  expires="$(awk '{print $4}' <<<"$exc")"
  reason="$(tr -s '[:space:]' ' ' <<<"$exc" | cut -d' ' -f5- | sed 's/ *$//')"
  used+=("$key")

  if ! [[ "$expires" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]]; then
    echo "::error::govulncheck gate: the exception for $id has no valid expiry date (got '$expires'); the finding fails ($module@$version: $call)"
    failures=$((failures + 1))
    continue
  fi
  if [[ "$today" > "$expires" ]]; then
    echo "::error::govulncheck gate: the exception for $id expired on $expires; the finding fails again ($module@$version: $call). Re-argue it with a new date, or fix the dependency."
    failures=$((failures + 1))
    continue
  fi
  if fixed_recorded "$id" "$module"; then
    echo "::error::govulncheck gate: the database now records a fixed version for $id in $module, and govulncheck still reports $version — the pin is short. The exception no longer applies ($call)."
    failures=$((failures + 1))
    continue
  fi
  echo "::warning::govulncheck gate: $id tolerated in $module@$version until $expires while the database records no fixed version — $reason ($call)"
  tolerated=$((tolerated + 1))
done < "$work/called.jsonl"

# Lines that tolerate nothing any more: the database caught up, or the module
# moved. They are debt, not danger — say so, and ask for the deletion.
while IFS= read -r exc; do
  [ -z "$exc" ] && continue
  id="$(awk '{print $1}' <<<"$exc")"
  module="$(awk '{print $2}' <<<"$exc")"
  version="$(awk '{print $3}' <<<"$exc")"
  key="$id $module $version"
  seen=0
  for u in "${used[@]+"${used[@]}"}"; do [ "$u" = "$key" ] && seen=1; done
  if [ "$seen" -eq 0 ]; then
    if fixed_recorded "$id" "$module"; then
      echo "::warning::govulncheck gate: the exception for $id ($module@$version) is stale — the database now records a fixed version and the scan no longer reports it. Delete the line."
    else
      echo "::warning::govulncheck gate: the exception for $id ($module@$version) matched no finding — the module moved or the scan changed. Delete the line."
    fi
  fi
done < <(grep -E -v '^[[:space:]]*(#|$)' "$exceptions" || true)

called_total="$(wc -l < "$work/called.jsonl" | tr -d ' ')"
echo "govulncheck gate: $called_total called vulnerability(ies), $tolerated tolerated by exception, $failures failing (govulncheck ${GOVULNCHECK_VERSION}, exceptions: $exceptions)"
if [ "$failures" -gt 0 ]; then
  exit 1
fi
echo "OK: no called vulnerability without a live exception"
