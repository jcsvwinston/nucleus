#!/usr/bin/env bash
# test_with_engines.sh — run the framework's tests with PostgreSQL, MySQL,
# SQL Server and Oracle linked in, the way the live DB matrix lanes need them.
#
# Why this exists (NU-106, A12 N1): a module's go.mod lists every module its
# own tests import, and the framework's go.mod is the one every application
# inherits. Its tests therefore link SQLite alone (internal/testsqlite); the
# other four engines would have put four drivers and their dependencies into
# the build graph of an application that chose none of them. The lanes that
# run the tests against a real server link those engines here instead, on top
# of SQLite, and the tests themselves do not change — the matrix tests stay
# where they are, next to the unexported code they exercise.
#
# How: a throwaway workspace holds this tree and the four driver modules, and
# an overlay adds one file to every package under test — an external test
# file that imports drivers/postgres, drivers/mysql, drivers/mssql and
# drivers/oracle. Each driver module registers its database/sql driver and
# its classifier, exactly as it does in an application. Nothing is written
# into the tree, and the root go.mod is not touched.
#
# A lane that runs a matrix test WITHOUT this script fails loudly rather than
# going green: the framework answers an unlinked engine with its guided error
# ("import _ .../drivers/postgres").
#
# Usage: the arguments of `go test`. Package patterns must start with "./".
#   bash scripts/ci/test_with_engines.sh ./pkg/db -run '^TestSQLMatrix_ConnectAndPing$' -v
#   bash scripts/ci/test_with_engines.sh -tags mssql ./pkg/model -run '^TestCRUDLive_' -v
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
engines=(postgres mysql mssql oracle)

patterns=()
tags=()
prev=""
for a in "$@"; do
  case "$a" in
    ./*) patterns+=("$a") ;;
  esac
  case "$prev" in -tags) tags=(-tags "$a") ;; esac
  case "$a" in -tags=*) tags=("$a") ;; esac
  prev=$a
done
if [[ ${#patterns[@]} -eq 0 ]]; then
  echo "test_with_engines.sh: no package pattern (they start with ./) in: $*" >&2
  exit 2
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

{
  echo "go $(awk '$1 == "go" {print $2; exit}' "$root/go.mod")"
  echo
  echo "use ("
  echo "	$root"
  for e in "${engines[@]}"; do echo "	$root/drivers/$e"; done
  echo ")"
} > "$work/go.work"
export GOWORK="$work/go.work"

overlay="$work/overlay.json"
mkdir -p "$work/files"
{
  echo '{"Replace": {'
  sep=""
  n=0
  while IFS='|' read -r dir name; do
    [[ -n "$dir" ]] || continue
    n=$((n + 1))
    f="$work/files/$n.go"
    {
      echo "package ${name}_test"
      echo
      echo "// Linked by scripts/ci/test_with_engines.sh: the engines the live DB matrix"
      echo "// connects to, through their driver modules (NU-106)."
      echo "import ("
      for e in "${engines[@]}"; do echo "	_ \"github.com/jcsvwinston/nucleus/drivers/$e\""; done
      echo ")"
    } > "$f"
    printf '%s"%s": "%s"' "$sep" "$dir/zz_engines_linked_by_ci_test.go" "$f"
    sep=$',\n'
  done < <(cd "$root" && go list ${tags[@]+"${tags[@]}"} -f '{{.Dir}}|{{.Name}}' "${patterns[@]}")
  echo
  echo '}}'
} > "$overlay"

cd "$root"
go test -overlay="$overlay" "$@"
