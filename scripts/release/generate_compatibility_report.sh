#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Generate release compatibility report artifact (fixture harness + stable contract summary).

Usage:
  bash scripts/release/generate_compatibility_report.sh [options]

Options:
  --output <path>         Optional markdown output path
  --min-fixture-rate <n>  Minimum fixture harness pass rate percentage (default: 100)
  --enforce-threshold     Exit non-zero if compatibility threshold is not met
  -h, --help              Show this help
USAGE
}

output_path=""
min_fixture_rate=100
enforce_threshold=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output)
      output_path="${2:-}"
      shift 2
      ;;
    --min-fixture-rate)
      min_fixture_rate="${2:-}"
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

if ! [[ "$min_fixture_rate" =~ ^[0-9]+$ ]] || [[ "$min_fixture_rate" -lt 0 || "$min_fixture_rate" -gt 100 ]]; then
  echo "--min-fixture-rate must be an integer between 0 and 100" >&2
  exit 1
fi

start_utc="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
branch="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)"
commit="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"

work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

harness_report="$work_dir/fixture_harness.md"
harness_status="success"
if ! bash scripts/ci/run_compatibility_harness.sh --output "$harness_report" --min-pass-rate "$min_fixture_rate"; then
  harness_status="failure"
fi

declare -a check_names
declare -a check_statuses
declare -a check_durations
declare -a check_commands
declare -a check_logs

checks_total=0
checks_passed=0

run_check() {
  local name="$1"
  local command="$2"
  local log_path="$work_dir/${name}.log"
  local started ended duration status

  started="$(date +%s)"
  if bash -lc "$command" >"$log_path" 2>&1; then
    status="success"
    checks_passed=$((checks_passed + 1))
  else
    status="failure"
  fi
  ended="$(date +%s)"
  duration=$((ended - started))

  checks_total=$((checks_total + 1))
  check_names+=("$name")
  check_statuses+=("$status")
  check_durations+=("${duration}s")
  check_commands+=("$command")
  check_logs+=("$log_path")
}

run_check "stable-api-app-core" "go test ./pkg/app -run '^Test(AppNew_|AppRegisterModel|AppShutdown_|AppMethods_)' -count=1"
run_check "stable-api-http-data" "go test ./pkg/router ./pkg/model ./pkg/db -count=1"
run_check "stable-cli" "go test ./internal/cli -count=1"
run_check "stable-plugin-sdk" "go test ./pkg/plugins ./examples/plugins/... -count=1"
run_check "stable-config" "go test ./pkg/app -run '^TestLoadConfig_|^TestConfig_' -count=1"
run_check "stable-contract-freeze" "bash scripts/ci/check_contract_freeze.sh"
run_check "firewall-type-leaks" "go test ./contracts -run '^TestFirewall_' -count=1"
run_check "stable-contract-docs" "test -f docs/reference/API_CONTRACT_INVENTORY.md && test -f docs/reference/CLI_CONTRACT_MATRIX.md && test -f docs/reference/CONFIG_KEY_REGISTRY.md && test -f docs/governance/DEPRECATION_TEMPLATE.md && test -f docs/governance/MIGRATION_ASSISTANT_CONVENTIONS.md && test -f docs/templates/deprecation_notice.md && test -f docs/templates/migration_assistant.md"

contracts_rate=$((checks_passed * 100 / checks_total))
overall_decision="READY"
if [[ "$harness_status" != "success" || "$checks_passed" -ne "$checks_total" ]]; then
  overall_decision="NOT READY"
fi

report_file="$work_dir/compatibility_report.md"
{
  echo "# Compatibility Report"
  echo
  echo "- Generated at (UTC): $start_utc"
  echo "- Branch: \`$branch\`"
  echo "- Commit: \`$commit\`"
  echo
  echo "## Fixture Applications"
  echo
  echo "- Harness status: $harness_status"
  echo
  tail -n +2 "$harness_report"
  echo
  echo "## Stable Contract Summary"
  echo
  echo "| Contract Scope | Status | Duration | Command |"
  echo "| --- | --- | --- | --- |"

  for idx in "${!check_names[@]}"; do
    echo "| ${check_names[$idx]} | ${check_statuses[$idx]} | ${check_durations[$idx]} | \`${check_commands[$idx]}\` |"
  done

  echo
  echo "- Stable contract checks passed: $checks_passed/$checks_total (${contracts_rate}%)"
  echo "- Compatibility statement: $( [[ "$overall_decision" == "READY" ]] && echo 'no breaking changes detected in validated stable contracts' || echo 'compatibility risk detected; blocking for remediation' )"
  echo "- Decision: $overall_decision"

  failed_any=0
  for idx in "${!check_names[@]}"; do
    if [[ "${check_statuses[$idx]}" != "success" ]]; then
      if [[ "$failed_any" -eq 0 ]]; then
        echo
        echo "## Contract Failure Snippets"
        echo
      fi
      failed_any=1
      echo "### ${check_names[$idx]}"
      echo
      echo '```text'
      tail -n 60 "${check_logs[$idx]}"
      echo '```'
      echo
    fi
  done
} >"$report_file"

if [[ -n "$output_path" ]]; then
  mkdir -p "$(dirname "$output_path")"
  cp "$report_file" "$output_path"
  echo "Compatibility report written to $output_path"
else
  cat "$report_file"
fi

if [[ "$enforce_threshold" -eq 1 && "$overall_decision" != "READY" ]]; then
  echo "Compatibility threshold not met." >&2
  exit 2
fi
