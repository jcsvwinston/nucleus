#!/usr/bin/env bash
# check_example_pins.sh — the suite pins in examples/**/go.mod must equal the
# LATEST published tag of each sibling repo (7ª ronda, QM7-1d).
#
# Why this design: showcase_demo sat pinned to nucleus v0.10.0 / orbit v0.2.0
# for eight minor releases because nothing compared its go.mod against
# anything. The versions the suite certifies live in the quantum umbrella
# repo, which this repo cannot see from CI — so the enforceable practical
# rule is "an example must pin the newest tag each sibling has published".
# The latest tags are fetched live with `git ls-remote --tags` (public repos,
# no auth, one fetch per repo); a pins file inside this repo was rejected
# because it can go stale exactly like the go.mod did — it would only move
# the fossil one file over.
#
# Consequence, on purpose: when a sibling repo cuts a tag, this check fails
# here until the example is re-pinned (and `GOWORK=off go mod tidy` is run).
# That failure IS the feature — it is the reminder this example never had.
#
# Only direct requires of github.com/<owner>/* are checked; `// indirect`
# lines are the solver's business. Monorepo submodules (orbit/quarkbridge,
# orbit/agent, …) map to their `<subdir>/vX.Y.Z` tag prefix.
#
# Usage:
#   bash scripts/ci/check_example_pins.sh              # every examples/*/go.mod
#   bash scripts/ci/check_example_pins.sh path/go.mod  # explicit list (tests)
set -euo pipefail

cd "$(dirname "$0")/../.."

OWNER="jcsvwinston"

mods=("$@")
if [[ ${#mods[@]} -eq 0 ]]; then
  mods=(examples/*/go.mod)
fi
if [[ ${#mods[@]} -eq 0 || ! -f "${mods[0]}" ]]; then
  echo "FAIL: no examples/*/go.mod found to check" >&2
  exit 1
fi

# One ls-remote per repo, cached across modules.
tags_dir=$(mktemp -d)
trap 'rm -rf "$tags_dir"' EXIT

repo_tags() {
  # $1 = bare repo name (e.g. "orbit"). Prints "refs/tags/..." names.
  local repo="$1" cache="$tags_dir/$1.tags"
  if [[ ! -f "$cache" ]]; then
    if ! git ls-remote --tags "https://github.com/$OWNER/$repo" 2>/dev/null \
        | awk '{print $2}' | sed -e 's|\^{}$||' | sort -u > "$cache"; then
      echo "FAIL: could not list tags of github.com/$OWNER/$repo (network/repo problem — this guard needs tag access to bite)" >&2
      exit 1
    fi
  fi
  cat "$cache"
}

latest_version() {
  # $1 = repo, $2 = tag prefix ("" or "subdir/"). Prints "vX.Y.Z" or nothing.
  local repo="$1" prefix="$2"
  repo_tags "$repo" \
    | sed -e "s|^refs/tags/||" \
    | grep -E "^${prefix}v[0-9]+\.[0-9]+\.[0-9]+$" \
    | sed -e "s|^${prefix}||" \
    | sort -V | tail -1
}

status=0
for mod in "${mods[@]}"; do
  checked=0
  while read -r path version; do
    [[ -z "$path" ]] && continue
    checked=$((checked + 1))

    rest="${path#github.com/$OWNER/}"
    repo="${rest%%/*}"
    subdir=""
    if [[ "$rest" == */* ]]; then
      subdir="${rest#*/}/"
    fi

    latest=$(latest_version "$repo" "$subdir")
    if [[ -z "$latest" ]]; then
      echo "FAIL: $mod: $path — no published tag matching '${subdir}vX.Y.Z' found in github.com/$OWNER/$repo" >&2
      status=1
      continue
    fi
    if [[ "$version" != "$latest" ]]; then
      echo "FAIL: $mod pins $path $version but the latest published tag is $latest — re-pin the example (then GOWORK=off go mod tidy)" >&2
      status=1
    else
      echo "OK: $mod: $path $version is the latest published tag"
    fi
  done < <(grep -E "github\.com/$OWNER/[A-Za-z0-9._/-]+ v[0-9][^ ]*" "$mod" \
             | grep -v '// indirect' \
             | sed -e 's/^require //' -e 's/^[[:space:]]*//' \
             | awk '{print $1, $2}')

  if [[ "$checked" -eq 0 ]]; then
    echo "FAIL: $mod has no direct github.com/$OWNER/* require — the parser or the module changed shape; fix this check" >&2
    status=1
  fi
done

if [[ $status -eq 0 ]]; then
  echo "OK: every example pins the latest published sibling tags"
fi
exit $status
