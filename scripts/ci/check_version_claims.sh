#!/usr/bin/env bash
# check_version_claims.sh — forcing function against version drift in the
# canonical documents (v1.2.0 audit backlog, NU-P0-2).
#
# The released version lives in .release-please-manifest.json. Every line
# in the documents below that carries an `x-release-please-version` marker
# must claim exactly that version. release-please's generic updater rewrites
# those lines on each release PR (see extra-files in release-please-config
# .json); this check makes CI fail loudly if anyone edits a claim by hand
# or adds a stale one.
set -euo pipefail

cd "$(dirname "$0")/../.."

manifest_version=$(sed -n 's/.*"\.": *"\([^"]*\)".*/\1/p' .release-please-manifest.json)
if [[ -z "$manifest_version" ]]; then
  echo "FAIL: could not read version for '.' from .release-please-manifest.json" >&2
  exit 1
fi

files=(README.md SPEC.md website/docs/intro.md)
status=0

for f in "${files[@]}"; do
  if ! grep -q "x-release-please-version" "$f"; then
    echo "FAIL: $f has no x-release-please-version marker — the version claim lost its updater annotation" >&2
    status=1
    continue
  fi
  while IFS= read -r line; do
    claimed=$(printf '%s\n' "$line" | grep -o 'v[0-9][0-9.]*[0-9]' | head -1 || true)
    if [[ -z "$claimed" ]]; then
      echo "FAIL: $f: marker line carries no version string: $line" >&2
      status=1
    elif [[ "$claimed" != "v$manifest_version" ]]; then
      echo "FAIL: $f claims $claimed but the released version is v$manifest_version: $line" >&2
      status=1
    fi
  done < <(grep "x-release-please-version" "$f")
done

if [[ $status -eq 0 ]]; then
  echo "OK: version claims in ${files[*]} match v$manifest_version"
fi
exit $status
