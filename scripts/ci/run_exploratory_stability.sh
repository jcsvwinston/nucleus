#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Run repeated CI workflow_dispatch executions and summarize MSSQL/Oracle exploratory job stability.

Usage:
  bash scripts/ci/run_exploratory_stability.sh [options]

Options:
  --runs <n>             Number of workflow runs to trigger (default: 5)
  --branch <name>        Git ref/branch to run (default: current branch)
  --repo <owner/name>    GitHub repository (default: inferred from origin remote)
  --workflow-file <file> Workflow file to dispatch (default: ci.yml)
  --interval <seconds>   Poll interval for run watch (default: 10)
  --min-rate-mssql <n>   Minimum MSSQL success rate percentage (default: 80)
  --min-rate-oracle <n>  Minimum Oracle success rate percentage (default: 80)
  --enforce-threshold    Exit non-zero if promotion threshold is not met
  --output <path>        Optional markdown output file
  -h, --help             Show this help

Prerequisites:
  - gh CLI authenticated (`gh auth login`) or GH_TOKEN env set
  - CI workflow supports `workflow_dispatch`
EOF
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

infer_repo_from_remote() {
  local remote
  remote="$(git config --get remote.origin.url || true)"
  remote="${remote%.git}"
  case "$remote" in
    git@github.com:*)
      echo "${remote#git@github.com:}"
      ;;
    https://github.com/*)
      echo "${remote#https://github.com/}"
      ;;
    http://github.com/*)
      echo "${remote#http://github.com/}"
      ;;
    *)
      return 1
      ;;
  esac
}

normalize_conclusion() {
  local value="${1:-}"
  if [[ -z "$value" || "$value" == "null" ]]; then
    echo "missing"
    return
  fi
  echo "$value"
}

contains_id() {
  local needle="$1"
  shift
  local item
  for item in "$@"; do
    if [[ "$item" == "$needle" ]]; then
      return 0
    fi
  done
  return 1
}

runs=5
branch="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo main)"
repo=""
workflow_file="ci.yml"
interval=10
output_path=""
min_rate_mssql=80
min_rate_oracle=80
enforce_threshold=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --runs)
      runs="${2:-}"
      shift 2
      ;;
    --branch)
      branch="${2:-}"
      shift 2
      ;;
    --repo)
      repo="${2:-}"
      shift 2
      ;;
    --workflow-file)
      workflow_file="${2:-}"
      shift 2
      ;;
    --interval)
      interval="${2:-}"
      shift 2
      ;;
    --min-rate-mssql)
      min_rate_mssql="${2:-}"
      shift 2
      ;;
    --min-rate-oracle)
      min_rate_oracle="${2:-}"
      shift 2
      ;;
    --enforce-threshold)
      enforce_threshold=1
      shift
      ;;
    --output)
      output_path="${2:-}"
      shift 2
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

if ! [[ "$runs" =~ ^[0-9]+$ ]] || [[ "$runs" -lt 1 ]]; then
  echo "--runs must be a positive integer" >&2
  exit 1
fi
if ! [[ "$interval" =~ ^[0-9]+$ ]] || [[ "$interval" -lt 1 ]]; then
  echo "--interval must be a positive integer" >&2
  exit 1
fi
if ! [[ "$min_rate_mssql" =~ ^[0-9]+$ ]] || [[ "$min_rate_mssql" -lt 0 || "$min_rate_mssql" -gt 100 ]]; then
  echo "--min-rate-mssql must be an integer between 0 and 100" >&2
  exit 1
fi
if ! [[ "$min_rate_oracle" =~ ^[0-9]+$ ]] || [[ "$min_rate_oracle" -lt 0 || "$min_rate_oracle" -gt 100 ]]; then
  echo "--min-rate-oracle must be an integer between 0 and 100" >&2
  exit 1
fi

require_command gh

if ! gh auth status >/dev/null 2>&1; then
  echo "GitHub auth missing. Run: gh auth login (or set GH_TOKEN)." >&2
  exit 1
fi

if [[ -z "$repo" ]]; then
  if ! repo="$(infer_repo_from_remote)"; then
    echo "Unable to infer repository. Provide --repo <owner/name>." >&2
    exit 1
  fi
fi

echo "Repository: $repo"
echo "Branch: $branch"
echo "Workflow file: $workflow_file"
echo "Runs: $runs"
echo "Promotion thresholds: MSSQL >= ${min_rate_mssql}% | Oracle >= ${min_rate_oracle}%"
echo

started_at="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
declare -a run_ids=()

for i in $(seq 1 "$runs"); do
  echo "[$i/$runs] Dispatching workflow..."
  gh workflow run "$workflow_file" --repo "$repo" --ref "$branch" >/dev/null

  new_id=""
  for _ in $(seq 1 120); do
    while IFS= read -r candidate; do
      [[ -z "$candidate" ]] && continue
      if ! contains_id "$candidate" "${run_ids[@]+"${run_ids[@]}"}"; then
        new_id="$candidate"
        break
      fi
    done < <(
      gh run list \
        --repo "$repo" \
        --workflow "CI" \
        --branch "$branch" \
        --event workflow_dispatch \
        --limit 30 \
        --json databaseId,createdAt \
        --jq '.[] | select(.createdAt >= "'"$started_at"'") | .databaseId'
    )

    if [[ -n "$new_id" ]]; then
      break
    fi
    sleep 3
  done

  if [[ -z "$new_id" ]]; then
    echo "Failed to detect dispatched run id for attempt $i" >&2
    exit 1
  fi

  run_ids+=("$new_id")
  echo "[$i/$runs] Captured run id: $new_id"
done

echo
echo "Waiting for workflow runs to finish..."
echo

for id in "${run_ids[@]}"; do
  echo "Watching run $id"
  gh run watch "$id" --repo "$repo" --interval "$interval" --exit-status || true
done

total_runs="${#run_ids[@]}"
mssql_success=0
mssql_fail=0
mssql_other=0
oracle_success=0
oracle_fail=0
oracle_other=0

report_file=""
if [[ -n "$output_path" ]]; then
  mkdir -p "$(dirname "$output_path")"
  report_file="$output_path"
else
  report_file="$(mktemp)"
fi

{
  echo "# Exploratory Stability Report"
  echo
  echo "- Generated at (UTC): $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  echo "- Repository: \`$repo\`"
  echo "- Branch: \`$branch\`"
  echo "- Workflow file: \`$workflow_file\`"
  echo "- Runs analyzed: $total_runs"
  echo
  echo "| Run ID | Workflow | MSSQL job | Oracle job | URL |"
  echo "| --- | --- | --- | --- | --- |"
} >"$report_file"

for id in "${run_ids[@]}"; do
  workflow_conclusion="$(normalize_conclusion "$(gh run view "$id" --repo "$repo" --json conclusion --jq '.conclusion')")"
  run_url="$(gh run view "$id" --repo "$repo" --json url --jq '.url')"
  mssql_conclusion="$(normalize_conclusion "$(gh run view "$id" --repo "$repo" --json jobs --jq '[.jobs[] | select(.name=="DB Matrix Live (mssql)") | .conclusion][0]')")"
  oracle_conclusion="$(normalize_conclusion "$(gh run view "$id" --repo "$repo" --json jobs --jq '[.jobs[] | select(.name=="DB Matrix Live (oracle)") | .conclusion][0]')")"

  case "$mssql_conclusion" in
    success) mssql_success=$((mssql_success + 1)) ;;
    failure|cancelled|timed_out|action_required|startup_failure) mssql_fail=$((mssql_fail + 1)) ;;
    *) mssql_other=$((mssql_other + 1)) ;;
  esac
  case "$oracle_conclusion" in
    success) oracle_success=$((oracle_success + 1)) ;;
    failure|cancelled|timed_out|action_required|startup_failure) oracle_fail=$((oracle_fail + 1)) ;;
    *) oracle_other=$((oracle_other + 1)) ;;
  esac

  echo "| $id | $workflow_conclusion | $mssql_conclusion | $oracle_conclusion | $run_url |" >>"$report_file"
done

mssql_rate=$((mssql_success * 100 / total_runs))
oracle_rate=$((oracle_success * 100 / total_runs))
promotion_ready=1
if [[ "$mssql_rate" -lt "$min_rate_mssql" || "$oracle_rate" -lt "$min_rate_oracle" ]]; then
  promotion_ready=0
fi

{
  echo
  echo "## Summary"
  echo
  echo "- MSSQL success: $mssql_success/$total_runs (${mssql_rate}%)"
  echo "- MSSQL failed: $mssql_fail/$total_runs"
  echo "- MSSQL other/missing: $mssql_other/$total_runs"
  echo "- Oracle success: $oracle_success/$total_runs (${oracle_rate}%)"
  echo "- Oracle failed: $oracle_fail/$total_runs"
  echo "- Oracle other/missing: $oracle_other/$total_runs"
  echo
  echo "## Promotion Readiness"
  echo
  echo "- Threshold MSSQL: >= ${min_rate_mssql}%"
  echo "- Threshold Oracle: >= ${min_rate_oracle}%"
  if [[ "$promotion_ready" -eq 1 ]]; then
    echo "- Decision: READY (threshold met)"
  else
    echo "- Decision: NOT READY (threshold not met)"
  fi
} >>"$report_file"

if [[ -n "$output_path" ]]; then
  echo
  echo "Report written to $report_file"
else
  cat "$report_file"
  rm -f "$report_file"
fi

if [[ "$enforce_threshold" -eq 1 && "$promotion_ready" -eq 0 ]]; then
  echo "Promotion threshold not met." >&2
  exit 2
fi
