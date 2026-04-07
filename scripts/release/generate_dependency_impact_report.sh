#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Generate dependency impact report for release validation.

Usage:
  bash scripts/release/generate_dependency_impact_report.sh [options]

Options:
  --output <path>             Optional markdown output path
  --base-ref <git-ref>        Baseline ref for dependency comparison
  --enforce-critical-review   Exit non-zero when critical dependencies changed
  -h, --help                  Show this help
USAGE
}

output_path=""
base_ref=""
enforce_critical_review=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output)
      output_path="${2:-}"
      shift 2
      ;;
    --base-ref)
      base_ref="${2:-}"
      shift 2
      ;;
    --enforce-critical-review)
      enforce_critical_review=1
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

resolve_base_ref() {
  local head_tags tag
  head_tags="$(git tag --points-at HEAD --list 'v*' | tr '\n' ' ')"

  while IFS= read -r tag; do
    [[ -z "$tag" ]] && continue
    if [[ " $head_tags " == *" $tag "* ]]; then
      continue
    fi
    echo "$tag"
    return
  done < <(git tag --list 'v*' --sort=-v:refname)

  if git rev-parse --verify HEAD~1 >/dev/null 2>&1; then
    echo "HEAD~1"
    return
  fi

  echo "HEAD"
}

extract_direct_requirements() {
  awk '
    /^require \(/ {in_block=1; next}
    in_block && /^\)/ {in_block=0; next}
    in_block {
      if ($0 ~ /\/\/[[:space:]]*indirect/) next
      if (NF >= 2) print $1, $2
      next
    }
    /^require[[:space:]]+/ {
      if ($0 ~ /\/\/[[:space:]]*indirect/) next
      if (NF >= 3) print $2, $3
    }
  '
}

is_critical_module() {
  local module="$1"
  local critical
  for critical in \
    github.com/casbin/casbin/v2 \
    github.com/go-sql-driver/mysql \
    github.com/jackc/pgx/v5 \
    github.com/microsoft/go-mssqldb \
    github.com/redis/go-redis/v9 \
    github.com/sijms/go-ora/v2 \
    github.com/hibiken/asynq \
    modernc.org/sqlite \
    go.opentelemetry.io/otel; do
    if [[ "$module" == "$critical" || "$module" == "$critical"/* ]]; then
      return 0
    fi
  done
  return 1
}

if [[ -z "$base_ref" ]]; then
  base_ref="$(resolve_base_ref)"
fi

start_utc="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
branch="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)"
commit="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"

work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

base_mod="$work_dir/base.go.mod"
head_direct="$work_dir/head_direct.txt"
base_direct="$work_dir/base_direct.txt"
changes_raw="$work_dir/changes_raw.tsv"
changes_enriched="$work_dir/changes_enriched.tsv"

extract_direct_requirements < go.mod | sort -u > "$head_direct"

baseline_status="available"
if git show "${base_ref}:go.mod" > "$base_mod" 2>/dev/null; then
  extract_direct_requirements < "$base_mod" | sort -u > "$base_direct"
else
  baseline_status="missing"
  : > "$base_direct"
fi

awk '
  NR==FNR {base[$1]=$2; next}
  {
    head[$1]=$2
  }
  END {
    for (m in head) {
      if (!(m in base)) {
        printf "added\t%s\t-\t%s\n", m, head[m]
      } else if (base[m] != head[m]) {
        printf "changed\t%s\t%s\t%s\n", m, base[m], head[m]
      }
    }
    for (m in base) {
      if (!(m in head)) {
        printf "removed\t%s\t%s\t-\n", m, base[m]
      }
    }
  }
' "$base_direct" "$head_direct" | sort -k2,2 > "$changes_raw"

critical_changes=0
total_changes=0

while IFS=$'\t' read -r change_type module old_version new_version; do
  [[ -z "$change_type" ]] && continue
  total_changes=$((total_changes + 1))
  critical="no"
  if is_critical_module "$module"; then
    critical="yes"
    critical_changes=$((critical_changes + 1))
  fi
  printf '%s\t%s\t%s\t%s\t%s\n' "$change_type" "$module" "$old_version" "$new_version" "$critical" >> "$changes_enriched"
done < "$changes_raw"

go_mod_changed="no"
go_sum_changed="no"
if [[ "$baseline_status" == "available" ]]; then
  if ! git diff --quiet "${base_ref}...HEAD" -- go.mod; then
    go_mod_changed="yes"
  fi
  if ! git diff --quiet "${base_ref}...HEAD" -- go.sum; then
    go_sum_changed="yes"
  fi
fi

decision="NO CRITICAL DEPENDENCY CHANGE"
if [[ "$critical_changes" -gt 0 ]]; then
  decision="CRITICAL REVIEW REQUIRED"
fi
if [[ "$baseline_status" != "available" ]]; then
  decision="BASELINE UNAVAILABLE"
fi

report_file="$work_dir/dependency_impact_report.md"
{
  echo "# Dependency Impact Report"
  echo
  echo "- Generated at (UTC): $start_utc"
  echo "- Branch: \`$branch\`"
  echo "- Commit: \`$commit\`"
  echo "- Baseline ref: \`$base_ref\`"
  echo "- Baseline status: $baseline_status"
  echo
  echo "## Summary"
  echo
  echo "- Direct dependency changes: $total_changes"
  echo "- Critical dependency changes: $critical_changes"
  echo "- go.mod changed vs baseline: $go_mod_changed"
  echo "- go.sum changed vs baseline: $go_sum_changed"
  echo "- Decision: $decision"
  echo
  echo "## Changed Direct Dependencies"
  echo

  if [[ "$total_changes" -eq 0 ]]; then
    echo "No direct dependency changes detected."
  else
    echo "| Type | Module | Old | New | Critical |"
    echo "| --- | --- | --- | --- | --- |"
    while IFS=$'\t' read -r change_type module old_version new_version critical; do
      echo "| $change_type | \`$module\` | \`$old_version\` | \`$new_version\` | $critical |"
    done < "$changes_enriched"
  fi

  echo
  echo "## Critical Dependency Set"
  echo
  echo "- \`github.com/casbin/casbin/v2\`"
  echo "- \`github.com/go-sql-driver/mysql\`"
  echo "- \`github.com/jackc/pgx/v5\`"
  echo "- \`github.com/microsoft/go-mssqldb\`"
  echo "- \`github.com/redis/go-redis/v9\`"
  echo "- \`github.com/sijms/go-ora/v2\`"
  echo "- \`github.com/hibiken/asynq\`"
  echo "- \`modernc.org/sqlite\`"
  echo "- \`go.opentelemetry.io/otel\` (and submodules)"
} > "$report_file"

if [[ -n "$output_path" ]]; then
  mkdir -p "$(dirname "$output_path")"
  cp "$report_file" "$output_path"
  echo "Dependency impact report written to $output_path"
else
  cat "$report_file"
fi

if [[ "$enforce_critical_review" -eq 1 && "$critical_changes" -gt 0 ]]; then
  echo "Critical dependency changes detected." >&2
  exit 2
fi
