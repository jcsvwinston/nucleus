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

# internal/cli/new.go: defaultPinnedFrameworkVersion — the tag `nucleus new`
# writes into every generated go.mod. It shipped one release stale (v1.3.1
# released, scaffolds pinning v1.3.0 — NU5-3) because its "bump on every tag"
# comment relied on a human. Now release-please rewrites it and this check
# fails if it drifts.
files=(README.md SPEC.md website/docs/intro.md internal/cli/new.go)
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

# ---------------------------------------------------------------------------
# Scaffold Go directives (5ª ronda). scaffoldGoVersion / scaffoldToolchain in
# internal/cli/new.go must mirror the framework's own go.mod — they are the
# `go` and `toolchain` directives every generated project starts with. Nothing
# rewrites them automatically (release-please only manages the release
# version), so this belt is the only thing standing between a go.mod bump and
# scaffolds pinning an outdated toolchain.
# ---------------------------------------------------------------------------
gomod_go=$(awk '$1 == "go" {print $2; exit}' go.mod)
gomod_toolchain=$(awk '$1 == "toolchain" {print $2; exit}' go.mod)
scaffold_go=$(sed -n 's/.*scaffoldGoVersion = "\([^"]*\)".*/\1/p' internal/cli/new.go)
scaffold_toolchain=$(sed -n 's/.*scaffoldToolchain = "\([^"]*\)".*/\1/p' internal/cli/new.go)

if [[ "$scaffold_go" != "$gomod_go" ]]; then
  echo "FAIL: scaffoldGoVersion is \"$scaffold_go\" but go.mod's go directive is \"$gomod_go\"" >&2
  status=1
fi
if [[ "$scaffold_toolchain" != "$gomod_toolchain" ]]; then
  echo "FAIL: scaffoldToolchain is \"$scaffold_toolchain\" but go.mod's toolchain directive is \"${gomod_toolchain:-<none>}\"" >&2
  status=1
fi
if [[ $status -eq 0 ]]; then
  echo "OK: scaffold Go directives (go=$scaffold_go, toolchain=${scaffold_toolchain:-<none>}) match go.mod"
fi

# ---------------------------------------------------------------------------
# Package-status coherence (v1.6.0 re-audit, NU-1).
#
# The README's package table and docs/reference/API_CONTRACT_INVENTORY.md both
# publish a maturity status per package. The inventory is the source of truth
# (it is what the contract-freeze gate reads); the README is the shop window.
# They drifted once already: pkg/observability was promoted to `stable` in
# v1.3.0 (v1 gate W1) and the inventory said so, but the README still said
# `experimental` — so the front page understated the guarantee we had actually
# committed to. This check fails CI when the two disagree.
#
# Both tables are pipe-delimited with the package in column 1 and the status in
# column 2; the README wraps the package in a markdown link, the inventory does
# not. We normalise both to "pkg/name<TAB>status" and diff.
# ---------------------------------------------------------------------------
inventory=docs/reference/API_CONTRACT_INVENTORY.md

extract_status() {
  # $1 = file. Emits "pkg/name<TAB>status" for every package table row.
  # awk, not sed: BSD sed (macOS, where contributors run this) has no `\?`,
  # so a single regex covering both the linked and unlinked package cell is
  # not portable between here and the Ubuntu CI runner.
  awk -F'|' '
    NF >= 4 {
      pkg = $2; st = $3
      if (match(pkg, /pkg\/[a-z\/]+/) == 0) next
      pkg = substr(pkg, RSTART, RLENGTH)
      gsub(/[` ]/, "", st)
      if (st !~ /^[a-z]+$/) next
      print pkg "\t" st
    }
  ' "$1" | sort -u
}

readme_status=$(extract_status README.md)
inventory_status=$(extract_status "$inventory")

if [[ -z "$readme_status" || -z "$inventory_status" ]]; then
  echo "FAIL: could not parse the package table out of README.md or $inventory — did the table format change?" >&2
  exit 1
fi

# Only packages listed in BOTH documents are compared: the inventory is
# deliberately broader (it lists internal packages the README does not).
while IFS=$'\t' read -r pkg readme_st; do
  inv_st=$(printf '%s\n' "$inventory_status" | awk -F'\t' -v p="$pkg" '$1 == p {print $2}')
  [[ -z "$inv_st" ]] && continue
  if [[ "$readme_st" != "$inv_st" ]]; then
    echo "FAIL: $pkg is \`$readme_st\` in README.md but \`$inv_st\` in $inventory" >&2
    status=1
  fi
done <<< "$readme_status"

if [[ $status -eq 0 ]]; then
  echo "OK: package statuses in README.md match $inventory"
fi
exit $status
