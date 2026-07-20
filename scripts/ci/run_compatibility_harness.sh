#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Run fixture-app compatibility harness checks and summarize profile status.

Usage:
  bash scripts/ci/run_compatibility_harness.sh [options]

Options:
  --output <path>         Optional markdown report output path
  --min-pass-rate <n>     Minimum pass rate percentage (default: 100)
  --enforce-threshold     Exit non-zero when pass rate is below threshold
  -h, --help              Show this help
USAGE
}

output_path=""
min_pass_rate=100
enforce_threshold=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output)
      output_path="${2:-}"
      shift 2
      ;;
    --min-pass-rate)
      min_pass_rate="${2:-}"
      shift 2
      ;;
    --enforce-threshold)
      enforce_threshold=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
done

if ! [[ "$min_pass_rate" =~ ^[0-9]+$ ]] || [[ "$min_pass_rate" -lt 0 || "$min_pass_rate" -gt 100 ]]; then
  echo "--min-pass-rate must be an integer between 0 and 100" >&2
  exit 1
fi

start_utc="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
branch="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)"
commit="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"

work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

profiles_total=0
profiles_passed=0

declare -a profile_names
declare -a profile_statuses
declare -a profile_durations
declare -a profile_logs
declare -a profile_commands

run_profile() {
  local name="$1"
  local command="$2"
  local log_path="$work_dir/${name}.log"
  local started ended duration status

  started="$(date +%s)"
  if bash -lc "$command" >"$log_path" 2>&1; then
    status="success"
    profiles_passed=$((profiles_passed + 1))
  else
    status="failure"
  fi
  ended="$(date +%s)"
  duration=$((ended - started))

  profiles_total=$((profiles_total + 1))
  profile_names+=("$name")
  profile_statuses+=("$status")
  profile_durations+=("${duration}s")
  profile_logs+=("$log_path")
  profile_commands+=("$command")
}

# Fixture profiles (restored 2026-07-07, v1 gate A-6). The historical
# trio (minimal-api, admin-heavy, plugin-heavy) died with the 2026-05-16
# examples purge; admin-heavy is obsolete since the admin moved to the
# orbit module (ADR-019), and plugin examples have not returned yet
# (ADR-010 Phase 4). Today's profiles are backed by the reference apps
# that actually exist:
#
#   core-build     — build-only check of the stable surface (kept from
#                    the interim harness; distinct from `go test ./...`).
#   mvc-api        — examples/mvc_api (part of this module): builds and
#                    runs its tests against the CURRENT tree.
#   showcase-suite — examples/showcase_demo (separate module pinning
#                    released nucleus/quark/orbit tags): an ephemeral
#                    go.work swaps in the CURRENT tree so the suite app
#                    compiles against HEAD while quark/orbit resolve
#                    from their released tags.
# GOWORK=off pins the standalone profiles to this module even when the
# repo is checked out inside a larger workspace (e.g. the Quantum suite
# umbrella) — the harness must measure the same thing everywhere.
run_profile "core-build" "GOWORK=off go build ./pkg/... ./cmd/nucleus ./internal/cli/..."
run_profile "mvc-api" "GOWORK=off go build ./examples/mvc_api/... && GOWORK=off go test ./examples/mvc_api/..."

repo_root="$(pwd)"
# The go.work directive must be >= the `go` directive of EVERY module it
# uses. The example module can carry a higher floor than the root: its
# pinned released deps set their own minimum (orbit v1.4.3 moved the
# example to go 1.26.5 while the framework's go.mod stayed at 1.26.4), so
# take the highest of the two instead of assuming the root's.
go_directive="$( { awk '/^go /{print $2; exit}' go.mod; awk '/^go /{print $2; exit}' examples/showcase_demo/go.mod; } | sort -V | tail -1)"
showcase_gowork="$work_dir/showcase.go.work"
cat >"$showcase_gowork" <<EOF
go $go_directive

use (
	$repo_root
	$repo_root/examples/showcase_demo
)
EOF
run_profile "showcase-suite" "cd '$repo_root/examples/showcase_demo' && GOWORK='$showcase_gowork' go build ./..."

pass_rate=$((profiles_passed * 100 / profiles_total))
decision="READY"
if [[ "$pass_rate" -lt "$min_pass_rate" ]]; then
  decision="NOT READY"
fi

report_file="$work_dir/report.md"
{
  echo "# Compatibility Harness Report"
  echo
  echo "- Generated at (UTC): $start_utc"
  echo "- Branch: \`$branch\`"
  echo "- Commit: \`$commit\`"
  echo "- Profiles analyzed: $profiles_total"
  echo
  echo "| Profile | Status | Duration | Command |"
  echo "| --- | --- | --- | --- |"

  for idx in "${!profile_names[@]}"; do
    echo "| ${profile_names[$idx]} | ${profile_statuses[$idx]} | ${profile_durations[$idx]} | \`${profile_commands[$idx]}\` |"
  done

  echo
  echo "## Summary"
  echo
  echo "- Passed profiles: $profiles_passed/$profiles_total (${pass_rate}%)"
  echo "- Threshold: >= ${min_pass_rate}%"
  echo "- Decision: ${decision}"

  failed_any=0
  for idx in "${!profile_names[@]}"; do
    if [[ "${profile_statuses[$idx]}" != "success" ]]; then
      if [[ "$failed_any" -eq 0 ]]; then
        echo
        echo "## Failure Snippets"
        echo
      fi
      failed_any=1
      echo "### ${profile_names[$idx]}"
      echo
      echo '```text'
      tail -n 40 "${profile_logs[$idx]}"
      echo '```'
      echo
    fi
  done
} >"$report_file"

if [[ -n "$output_path" ]]; then
  mkdir -p "$(dirname "$output_path")"
  cp "$report_file" "$output_path"
  echo "Compatibility harness report written to $output_path"
else
  cat "$report_file"
fi

if [[ "$enforce_threshold" -eq 1 && "$pass_rate" -lt "$min_pass_rate" ]]; then
  echo "Compatibility harness threshold not met." >&2
  exit 2
fi
