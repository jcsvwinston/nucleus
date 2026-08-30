#!/usr/bin/env bash
# check_example_pins.sh — the suite pins in examples/**/go.mod must be at the
# LATEST published tag of each sibling repo, or at most ONE MINOR behind it
# (7ª ronda QM7-1d; tolerance added in the QCD-debt arc, 2026-08-30).
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
# Why the one-minor tolerance: the original strict-equality tooth meant that
# ANY sibling tag turned this check red on EVERY open PR of this repo at once
# (showcase-smoke feeds CI Required Gate) — the 1.23.0 train needed SIX
# emergency re-pin chores just to keep merging. The tolerance mirrors the
# root-edge rule orbit's check_internal_pins already uses (≤1 minor of lag,
# same major, orbit#131): a patch or a single minor behind WARNs loudly but
# stays green, so a mid-train tag no longer blocks unrelated PRs; the SECOND
# minor of lag goes red. The reminder still exists — it is the WARN plus
# that clock — and closing the lag is now mechanical:
# `bash scripts/release/repin_examples.sh` (the writer this judge pairs
# with), or the repin-showcase.yml workflow which opens the chore PR itself.
# The end-of-train re-pin to the exact certified set remains a fixed step of
# the release procedure (docs/governance/RELEASE_CHECKLIST.md § Post-Release).
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

lag_class() {
  # $1 = pinned "vX.Y.Z", $2 = latest published "vX.Y.Z".
  # Prints: exact | tolerable (≤1 minor behind, same major) | stale | ahead.
  local pin="${1#v}" latest="${2#v}" pM pm lM lm
  if [[ "$pin" == "$latest" ]]; then echo exact; return; fi
  IFS=. read -r pM pm _ <<<"$pin"
  IFS=. read -r lM lm _ <<<"$latest"
  if [[ "$pM" != "$lM" ]]; then echo stale; return; fi
  # A pin NEWER than any published tag is a different disease (typo, or a
  # tag that was deleted): never tolerate it.
  if [[ "$(printf '%s\n%s\n' "$pin" "$latest" | sort -V | tail -1)" == "$pin" ]]; then
    echo ahead; return
  fi
  if (( lm - pm <= 1 )); then echo tolerable; else echo stale; fi
}

status=0
warned=0
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
    case "$(lag_class "$version" "$latest")" in
      exact)
        echo "OK: $mod: $path $version is the latest published tag"
        ;;
      tolerable)
        echo "WARN: $mod pins $path $version; the latest published tag is $latest — within the one-minor tolerance, but close the lag (bash scripts/release/repin_examples.sh) before the NEXT minor turns this red" >&2
        warned=1
        ;;
      ahead)
        echo "FAIL: $mod pins $path $version, which is NEWER than any published tag ($latest) — a pin must point at a tag that exists" >&2
        status=1
        ;;
      *)
        echo "FAIL: $mod pins $path $version but the latest published tag is $latest — more than one minor behind (or major drift); re-pin the example (bash scripts/release/repin_examples.sh)" >&2
        status=1
        ;;
    esac
  done < <(grep -E "github\.com/$OWNER/[A-Za-z0-9._/-]+ v[0-9][^ ]*" "$mod" \
             | grep -v '// indirect' \
             | sed -e 's/^require //' -e 's/^[[:space:]]*//' \
             | awk '{print $1, $2}')

  if [[ "$checked" -eq 0 ]]; then
    echo "FAIL: $mod has no direct github.com/$OWNER/* require — the parser or the module changed shape; fix this check" >&2
    status=1
  fi
done

if [[ $status -eq 0 && $warned -eq 0 ]]; then
  echo "OK: every example pins the latest published sibling tags"
elif [[ $status -eq 0 ]]; then
  echo "OK (with WARNs): every pin is within the one-minor tolerance, but at least one lags — close it before the next sibling minor"
fi
exit $status
