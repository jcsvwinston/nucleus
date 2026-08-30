#!/usr/bin/env bash
# repin_examples.sh — the WRITER that pairs with scripts/ci/check_example_pins.sh.
#
# The judge demands that every direct require of github.com/<owner>/* in
# examples/*/go.mod be at the latest published sibling tag (one minor of lag
# tolerated with a WARN). Closing that lag used to be six hand edits per
# release train; this script makes it one command: it rewrites each pin to
# the latest published tag (same ls-remote + sort -V logic as the judge —
# keep the two in sync) and runs `GOWORK=off go mod tidy` per touched module.
#
# Usage:
#   bash scripts/release/repin_examples.sh          # rewrite + tidy
#   bash scripts/release/repin_examples.sh --check  # dry-run; exit 1 if lagging
#
# The repin-showcase.yml workflow runs this after every release of this repo
# and opens the chore PR; run it by hand (or `gh workflow run
# repin-showcase.yml`) after a SIBLING repo tags, since sibling repos cannot
# trigger our workflows without a cross-repo token.
set -euo pipefail

cd "$(dirname "$0")/../.."

OWNER="jcsvwinston"
MODE="${1:-write}"

tags_dir=$(mktemp -d)
trap 'rm -rf "$tags_dir"' EXIT

repo_tags() {
  local repo="$1" cache="$tags_dir/$1.tags"
  if [[ ! -f "$cache" ]]; then
    if ! git ls-remote --tags "https://github.com/$OWNER/$repo" 2>/dev/null \
        | awk '{print $2}' | sed -e 's|\^{}$||' | sort -u > "$cache"; then
      echo "FAIL: could not list tags of github.com/$OWNER/$repo (network/repo problem)" >&2
      exit 1
    fi
  fi
  cat "$cache"
}

latest_version() {
  local repo="$1" prefix="$2"
  repo_tags "$repo" \
    | sed -e "s|^refs/tags/||" \
    | grep -E "^${prefix}v[0-9]+\.[0-9]+\.[0-9]+$" \
    | sed -e "s|^${prefix}||" \
    | sort -V | tail -1
}

changed_any=0
for mod in examples/*/go.mod; do
  dir=$(dirname "$mod")
  changed_here=0
  while read -r path version; do
    [[ -z "$path" ]] && continue

    rest="${path#github.com/$OWNER/}"
    repo="${rest%%/*}"
    subdir=""
    if [[ "$rest" == */* ]]; then
      subdir="${rest#*/}/"
    fi

    latest=$(latest_version "$repo" "$subdir")
    if [[ -z "$latest" ]]; then
      echo "FAIL: $mod: $path — no published tag matching '${subdir}vX.Y.Z' found" >&2
      exit 1
    fi
    if [[ "$version" != "$latest" ]]; then
      echo "repin: $mod: $path $version → $latest"
      changed_here=1
      changed_any=1
      if [[ "$MODE" != "--check" ]]; then
        (cd "$dir" && GOWORK=off go mod edit -require="$path@$latest")
      fi
    fi
  done < <(grep -E "github\.com/$OWNER/[A-Za-z0-9._/-]+ v[0-9][^ ]*" "$mod" \
             | grep -v '// indirect' \
             | sed -e 's/^require //' -e 's/^[[:space:]]*//' \
             | awk '{print $1, $2}')

  if [[ $changed_here -eq 1 && "$MODE" != "--check" ]]; then
    echo "tidy: $dir (GOWORK=off go mod tidy)"
    (cd "$dir" && GOWORK=off go mod tidy)
  fi
done

if [[ $changed_any -eq 0 ]]; then
  echo "OK: every example already pins the latest published sibling tags"
elif [[ "$MODE" == "--check" ]]; then
  exit 1
else
  echo "Done. Review the diff and commit as: chore(examples): re-pina el showcase a los últimos tags publicados"
fi
