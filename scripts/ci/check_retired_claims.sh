#!/usr/bin/env bash
# check_retired_claims.sh — the living documentation must not describe a
# world the code has left.
#
# Each pattern below is a claim that was true once and is now false, found
# still standing in README, SPEC or the public site after the change that
# retired it (audit 2026-09, NU-24/25/57/60, QM-4). A reader who meets
# "MSSQL sits behind -tags mssql" on the first screen and then finds
# `nucleus add sqlserver` in the CLI stops trusting both. Prose drifts
# silently; this makes the drift a red build.
#
# Scope: the LIVING docs — README.md, SPEC.md, CONTRIBUTING.md, docs/ (minus
# the directories that are records by nature: adrs, deprecations,
# migration_assistants, iterations, audits, reports — an ADR or a
# deprecation notice names what it replaced, and a minute is a minute) and
# website/docs (never the immutable versioned_docs snapshots).
#
# To keep a legitimate mention (a deprecation record, a "this used to be"
# sentence), put `retired-claims-allow` on the line before it.
#
# Usage: bash scripts/ci/check_retired_claims.sh
set -uo pipefail

cd "$(dirname "$0")/../.."

RULES=$(cat <<'EOR'
-tags mssql	Build tags are gone: SQL Server ships as drivers/mssql (`nucleus add sqlserver`).
-tags oracle	Build tags are gone: Oracle ships as drivers/oracle (`nucleus add oracle`).
single Go module	Nucleus is a core module plus optional driver/exporter/provider modules.
MountOpenAPI\(	App.MountOpenAPI was removed in v0.12.0; the mount is MountOpenAPIHandler(pattern, openapi.Handler(provider)).
`noop`[^\n]{0,12}`smtp`[^\n]{0,6}`sendgrid`|sendgrid_\*	The built-in sendgrid mail driver and its sendgrid_* keys were removed (DEP-2026-002); mail ships noop, smtp and external nucleus-plugin-<provider> senders (a plugin named sendgrid is fine — listing it as a built-in driver is not).
EOR
)

files=()
while IFS= read -r f; do files+=("$f"); done < <(
  {
    ls README.md SPEC.md CONTRIBUTING.md 2>/dev/null
    find docs -type f -name '*.md' \
      -not -path 'docs/adrs/*' -not -path 'docs/deprecations/*' \
      -not -path 'docs/migration_assistants/*' -not -path 'docs/iterations/*' \
      -not -path 'docs/audits/*' -not -path 'docs/reports/*'
    find website/docs -type f \( -name '*.md' -o -name '*.mdx' \)
  } | sort
)

status=0
while IFS=$'\t' read -r pattern advice; do
  [[ -z "$pattern" ]] && continue
  while IFS= read -r hit; do
    [[ -z "$hit" ]] && continue
    file="${hit%%:*}"
    rest="${hit#*:}"
    line="${rest%%:*}"
    if [[ "$line" -gt 1 ]]; then
      prev=$(sed -n "$((line - 1))p" "$file")
      [[ "$prev" == *"retired-claims-allow"* ]] && continue
    fi
    if [[ "$status" -eq 0 ]]; then
      echo "Retired claims found in the living documentation:" >&2
      echo >&2
    fi
    status=1
    echo "  $hit" >&2
    echo "    -> $advice" >&2
  done < <(grep -nE -- "$pattern" "${files[@]}" 2>/dev/null)
done <<< "$RULES"

if [[ "$status" -ne 0 ]]; then
  echo >&2
  echo "Fix the sentence, or put 'retired-claims-allow' on the line before a deliberate mention." >&2
  exit 1
fi
echo "OK: no retired claims in README, SPEC, docs/ or website/docs"
