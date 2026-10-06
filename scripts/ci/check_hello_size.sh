#!/usr/bin/env bash
# check_hello_size.sh — the hello-world stays small (NU-106, A12 N1).
#
# An application's cost in dependencies is decided by what the framework's
# go.mod lists and by what pkg/app imports, and both grow one reasonable
# change at a time: the hello-world went from 87 to 92 modules after ADR-031
# with nothing watching, and the framework's go.mod carried four database
# engines and miniredis that only its own tests and the CLI used. This lane
# builds two programs against this tree and fails when any figure passes its
# ceiling in scripts/ci/hello-size/ceilings.tsv:
#
#   hello          pkg/app and nothing else (the program ADR-031 measured)
#   hello+sqlite   the same, plus drivers/sqlite — the smallest program that
#                  talks to a database
#
# and for each, four figures:
#
#   modules    the build list, `go list -m all` — what `go mod download`
#              fetches and what a vulnerability scan of the module walks;
#   linked     the modules whose code is in the binary (`go version -m`);
#              the build list overstates the cost, this does not;
#   packages   `go list -deps`, standard library included;
#   stripped   the binary built with -trimpath -ldflags='-s -w', in bytes.
#
# Everything is measured for linux/amd64 with CGO off, and -trimpath keeps the
# checkout's and the module cache's paths out of the binary, so the figures
# are the same on a laptop and on the runner. A ceiling that is reached on purpose is
# raised in the same pull request, with the reason in its message: the point
# is that growth is a decision somebody makes, not a drift somebody finds.
#
# Usage: bash scripts/ci/check_hello_size.sh [--report-only]
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
ceilings="$root/scripts/ci/hello-size/ceilings.tsv"
report_only=0
[[ "${1:-}" == "--report-only" ]] && report_only=1

export GOWORK=off GOFLAGS=-mod=mod GOOS=linux GOARCH=amd64 CGO_ENABLED=0
gover=$(awk '$1 == "go" {print $2; exit}' "$root/go.mod")

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# write_program DIR DRIVER — a module that links pkg/app (and DRIVER, when
# given), resolved against this tree through replace directives.
write_program() {
  local dir=$1 driver=${2:-}
  mkdir -p "$dir"
  {
    echo "module example.com/hello"
    echo
    echo "go $gover"
    echo
    echo "replace github.com/jcsvwinston/nucleus => $root"
    [[ -n "$driver" ]] && echo "replace github.com/jcsvwinston/nucleus/drivers/$driver => $root/drivers/$driver"
  } > "$dir/go.mod"
  {
    echo "package main"
    echo
    echo "import ("
    echo "	\"context\""
    echo
    echo "	\"github.com/jcsvwinston/nucleus/pkg/app\""
    [[ -n "$driver" ]] && echo "	_ \"github.com/jcsvwinston/nucleus/drivers/$driver\""
    echo ")"
    echo
    echo "func main() {"
    echo "	a, err := app.New(&app.Config{})"
    echo "	if err != nil {"
    echo "		panic(err)"
    echo "	}"
    echo "	_ = a.Run(context.Background())"
    echo "}"
  } > "$dir/main.go"
  if ! (cd "$dir" && go mod tidy >/dev/null 2>"$dir/tidy.log"); then
    cat "$dir/tidy.log" >&2
    exit 1
  fi
}

# measure DIR — prints "modules linked packages stripped".
measure() {
  local dir=$1 modules packages linked stripped
  (
    cd "$dir"
    modules=$(go list -m all | wc -l | tr -d ' ')
    packages=$(go list -deps . | wc -l | tr -d ' ')
    go build -trimpath -ldflags='-s -w' -o hello .
    linked=$(go version -m hello | awk '$1 == "dep"' | wc -l | tr -d ' ')
    stripped=$(wc -c < hello | tr -d ' ')
    echo "$modules $linked $packages $stripped"
  )
}

write_program "$work/hello"
write_program "$work/hello-sqlite" sqlite

# Plain variables, not an associative array: macOS ships bash 3.2.
got_hello=$(measure "$work/hello")
got_hello_sqlite=$(measure "$work/hello-sqlite")

# The engine the program chose and no other: the property ADR-031 and NU-8
# bought, checked on the program rather than on a package listing.
engines='github.com/(jackc/pgx|go-sql-driver/mysql|microsoft/go-mssqldb|sijms/go-ora)|modernc.org/sqlite'
status=0
if (cd "$work/hello" && go version -m hello | awk '$1 == "dep" {print $2}' | grep -E "^($engines)"); then
  echo "FAIL: hello links a database engine it never imported" >&2
  status=1
fi
linked_engines=$(cd "$work/hello-sqlite" && go version -m hello | awk '$1 == "dep" {print $2}' | grep -E "^($engines)" || true)
if [[ "$linked_engines" != "modernc.org/sqlite" ]]; then
  echo "FAIL: hello+sqlite must link SQLite and no other engine; it links: ${linked_engines:-nothing}" >&2
  status=1
fi

summary="${GITHUB_STEP_SUMMARY:-/dev/null}"
{
  echo "### The hello-world stays small"
  echo
  echo "linux/amd64, CGO off, $(go version | awk '{print $3}'). Ceilings in \`scripts/ci/hello-size/ceilings.tsv\`."
  echo
  echo "| program | modules | linked | packages | stripped (bytes) |"
  echo "|---|---:|---:|---:|---:|"
} >> "$summary"

printf '%-14s %8s %8s %9s %12s\n' program modules linked packages stripped
while IFS=$'\t' read -r name cmod clink cpkg cbytes; do
  [[ "$name" == \#* || -z "$name" ]] && continue
  case "$name" in
    hello) read -r m l p s <<< "$got_hello" ;;
    hello+sqlite) read -r m l p s <<< "$got_hello_sqlite" ;;
    *) echo "FAIL: $ceilings names a program this script does not build: $name" >&2; status=1; continue ;;
  esac
  printf '%-14s %8s %8s %9s %12s   (ceilings %s %s %s %s)\n' "$name" "$m" "$l" "$p" "$s" "$cmod" "$clink" "$cpkg" "$cbytes"
  echo "| $name | $m / $cmod | $l / $clink | $p / $cpkg | $s / $cbytes |" >> "$summary"
  for pair in "modules:$m:$cmod" "linked modules:$l:$clink" "packages:$p:$cpkg" "stripped bytes:$s:$cbytes"; do
    IFS=: read -r what have ceiling <<< "$pair"
    if (( have > ceiling )); then
      echo "FAIL: $name: $what $have is over the ceiling $ceiling" >&2
      status=1
    fi
  done
done < "$ceilings"

if [[ $status -ne 0 ]]; then
  cat >&2 <<'MSG'

Something the hello-world links, or the framework's go.mod lists, grew. Find
it with `go mod graph` / `go list -deps` in the program the script builds
(rerun with --report-only to see the figures without failing). If the growth
is intended, raise the ceiling in scripts/ci/hello-size/ceilings.tsv in the
same pull request and say why in its description.
MSG
  [[ $report_only -eq 1 ]] && exit 0
  exit 1
fi
echo "OK: hello and hello+sqlite are within their ceilings"
