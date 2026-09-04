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

if [[ "$failed" -ne 0 ]]; then
  exit 1
fi
echo "OK: every sibling module resolves, builds and vets standalone"
