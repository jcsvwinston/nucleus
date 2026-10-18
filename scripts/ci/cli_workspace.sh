#!/usr/bin/env bash
# cli_workspace.sh — write a go.work that builds the CLI module against this
# tree.
#
# The CLI is a module of its own (cmd/nucleus/go.mod, A12 N1) so that what it
# alone needs — the five database engines — stays out of the go.mod every
# application inherits. It requires the framework at a version, never through
# a `replace`: `go install .../cmd/nucleus@vX.Y.Z` refuses a module whose
# go.mod carries one. Between releases that version is the last one cut, and
# on a release branch it is the one being cut, which does not exist yet. A
# workspace `use` says where a module's source is, not which version the
# graph resolves, so the go command would still fetch that version's go.mod
# from the proxy; the versioned replace below points it at this tree too
# (a workspace module replaced at ALL versions is refused, so it names the
# version).
#
# The same holds for every sibling module the CLI requires and this script
# links: the go command reads the required version's go.mod from the proxy
# even when the module is in `use`, and a module that is born in the same
# cut as the CLI's requirement on it (providers/tasks-asynq at 2.0, the
# 1.38.0 trap) has no such version yet. Each linked module the CLI requires
# gets a versioned replace of its own, so the tree is what resolves.
#
# Usage: cli_workspace.sh <path of the go.work to write> [module dir ...]
#   bash scripts/ci/cli_workspace.sh go.work                       # the CLI
#   bash scripts/ci/cli_workspace.sh go.work drivers/postgres ...  # + drivers
#   bash scripts/ci/cli_workspace.sh go.work providers/tasks-asynq # + a provider
#   GOWORK=$RUNNER_TEMP/go.work ...  # outside the checkout, as release.yml does
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
out=${1:?usage: cli_workspace.sh <go.work path> [module dir ...]}
shift

floor=$(awk '$1 == "github.com/jcsvwinston/nucleus" {print $2; exit}' "$root/cmd/nucleus/go.mod")
if [[ -z "$floor" ]]; then
  echo "cli_workspace.sh: cmd/nucleus/go.mod does not require github.com/jcsvwinston/nucleus" >&2
  exit 1
fi

{
  echo "go $(awk '$1 == "go" {print $2; exit}' "$root/go.mod")"
  echo
  echo "use ("
  echo "	$root"
  echo "	$root/cmd/nucleus"
  for m in "$@"; do echo "	$root/${m%/}"; done
  echo ")"
  echo
  echo "replace github.com/jcsvwinston/nucleus $floor => $root"
  for m in "$@"; do
    m=${m%/}
    path=$(awk '$1 == "module" {print $2; exit}' "$root/$m/go.mod")
    pin=$(awk -v p="$path" '$1 == p {print $2; exit}' "$root/cmd/nucleus/go.mod")
    [[ -n "$pin" ]] && echo "replace $path $pin => $root/$m"
  done
} > "$out"
