#!/usr/bin/env bash
# check_modules_standalone.sh — every sibling module (drivers/*, exporters/*,
# providers/*) must resolve, build and vet on its own, with no workspace.
#
# Why: the module lanes in CI link the modules to the tree under review with
# `go work init`, which is right for testing a change to pkg/db against the
# module that consumes it — and wrong as the ONLY check, because a workspace
# masks a go.mod that does not resolve from the proxy. That is how eleven
# modules pinned a nucleus release that did not yet contain the packages
# they import, and `cd drivers/sqlite && go build ./...` failed for anyone
# who tried it while CI stayed green (audit 2026-09, NU-1).
#
# The "standalone" a reader gets is the published pin, so this runs with the
# workspace OFF: `go mod tidy` must be a no-op (a change means go.mod/go.sum
# drifted from what the module needs — the diff is printed and the files are
# left as tidy wrote them, so the fix is to commit it), then build and vet.
#
# Usage:
#   bash scripts/ci/check_modules_standalone.sh              # every module
#   bash scripts/ci/check_modules_standalone.sh drivers/sqlite providers/ldap
#   bash scripts/ci/check_modules_standalone.sh cmd/nucleus  # the CLI alone
set -euo pipefail

cd "$(dirname "$0")/../.."

modules=("$@")
if [[ ${#modules[@]} -eq 0 ]]; then
  for m in drivers/*/ exporters/*/ providers/*/; do
    [[ -f "$m/go.mod" ]] && modules+=("${m%/}")
  done
fi

failed=0
for m in "${modules[@]}"; do
  [[ "$m" == cmd/nucleus ]] && continue # checked below, by check_cli
  echo "== $m"
  if ! (
    cd "$m"
    export GOWORK=off
    before=$(cat go.mod go.sum 2>/dev/null | shasum)
    go mod tidy
    after=$(cat go.mod go.sum 2>/dev/null | shasum)
    if [[ "$before" != "$after" ]]; then
      git --no-pager diff -- go.mod go.sum || true
      echo "FAIL: $m: go mod tidy changed go.mod/go.sum — commit what it wrote" >&2
      exit 1
    fi
    go build ./...
    go vet ./...
  ); then
    failed=1
    echo "FAIL: $m does not build standalone" >&2
  fi
done

# The CLI is a module too (cmd/nucleus/go.mod, A12 N1), and the one people
# `go install`. It is checked the way `go install` resolves it — workspace
# off, checksums from the checksum database — and NOT tidied: release-please
# rewrites its framework requirement to the version each release cuts, so on
# main its go.sum trails that one entry by design, and `go install
# pkg@version` does not read the module's go.sum anyway. Two requirements
# cannot be checked here, and the lane says so instead of passing quietly:
# a version the proxy does not have yet (a release branch), and a version
# that still ships cmd/nucleus itself — every release up to v1.31.0 — under
# which the package is provided twice and Go refuses with `ambiguous
# import`. The first release cut after the CLI moved ends the second case.
check_cli() {
  local floor out dir
  floor=$(awk '$1 == "github.com/jcsvwinston/nucleus" {print $2; exit}' cmd/nucleus/go.mod)
  if ! out=$(GOWORK=off GOFLAGS='' go mod download -json "github.com/jcsvwinston/nucleus@$floor" 2>&1); then
    echo "NOTICE: cmd/nucleus requires github.com/jcsvwinston/nucleus $floor, which the proxy does not serve yet (a release branch); not checked standalone"
    return 0
  fi
  dir=$(printf '%s\n' "$out" | sed -n 's/^[[:space:]]*"Dir": "\(.*\)",$/\1/p')
  if [[ -d "$dir/cmd/nucleus" ]]; then
    echo "NOTICE: cmd/nucleus requires github.com/jcsvwinston/nucleus $floor, a release that still ships cmd/nucleus itself; not checkable standalone until the first release cut without it"
    return 0
  fi
  (
    cd cmd/nucleus
    saved=$(mktemp -d)
    cp go.mod go.sum "$saved/"
    trap 'cp "$saved/go.mod" "$saved/go.sum" . && rm -rf "$saved"' EXIT
    GOWORK=off GOFLAGS=-mod=mod go build ./...
    GOWORK=off GOFLAGS=-mod=mod go vet ./...
  )
}

if [[ $# -eq 0 || " $* " == *" cmd/nucleus "* ]]; then
  echo "== cmd/nucleus"
  if ! check_cli; then
    failed=1
    echo "FAIL: cmd/nucleus does not build against the framework release it requires" >&2
  fi
fi

if [[ "$failed" -ne 0 ]]; then
  exit 1
fi
echo "OK: every sibling module resolves, builds and vets standalone"
