#!/usr/bin/env bash
# check_adr_index.sh — every ADR appears in the index.
#
# The index sat at ADR-022 while the directory held 29: seventeen decisions
# were reachable only by listing the directory, which is exactly the state
# an index exists to prevent. Nothing was watching it, so it rotted for
# five arcs without anybody noticing — the same shape as every other fossil
# this repo guards against.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."

index="docs/adrs/README.md"
status=0

for f in docs/adrs/ADR-*.md; do
  name=$(basename "$f")
  if ! grep -q "($name)" "$index"; then
    echo "FAIL: $name is not listed in $index — an ADR nobody can find from the index is a decision nobody will read" >&2
    status=1
  fi
done

# And the reverse: a link to an ADR that no longer exists.
while IFS= read -r target; do
  if [[ ! -f "docs/adrs/$target" ]]; then
    echo "FAIL: $index links $target, which does not exist" >&2
    status=1
  fi
done < <(grep -oE '\(ADR-[0-9]+[^)]*\.md\)' "$index" | tr -d '()')

if [[ $status -eq 0 ]]; then
  echo "OK: every ADR is listed in $index, and every link resolves ($(ls docs/adrs/ADR-*.md | wc -l | tr -d ' ') records)"
fi
exit $status
