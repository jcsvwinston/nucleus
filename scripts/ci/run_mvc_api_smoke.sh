#!/usr/bin/env bash
# run_mvc_api_smoke.sh — BOOTS examples/mvc_api and asserts it answers.
#
# The reference application is the file the quickstart embeds and the
# starting point the README names, and for one release it did not start:
# the database driver had moved to its own module and the example never
# imported it, so `go run .` compiled and then exited with "the sqlite
# driver ... is not imported yet". A build check cannot catch that — only
# running the thing can — which is what this does: apply its migrations
# with the CLI built from this tree, boot it on a free port with a
# throwaway database, and hit /healthz and the /notes resource (the second
# one touches the database, so it proves the driver is linked).
#
# It honours whatever workspace is active. CI runs it inside a go.work that
# links the example and drivers/sqlite to the tree under review, so a change
# to pkg/ is exercised here before it is released.
set -euo pipefail

cd "$(dirname "$0")/../.."
root=$(pwd)
EXAMPLE_DIR="$root/examples/mvc_api"

workdir=$(mktemp -d)
app_pid=""
cleanup() {
  if [[ -n "$app_pid" ]] && kill -0 "$app_pid" 2>/dev/null; then
    kill "$app_pid" 2>/dev/null || true
    wait "$app_pid" 2>/dev/null || true
  fi
  rm -rf "$workdir"
}
trap cleanup EXIT

fail() {
  echo "FAIL: $1" >&2
  echo "--- app log ---" >&2
  cat "$workdir/app.log" 2>/dev/null >&2 || true
  exit 1
}

port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()' 2>/dev/null || echo 18090)
BASE_URL="http://127.0.0.1:$port"
db_url="sqlite://$workdir/mvc_api_smoke.db"

echo "== build the CLI from this tree"
go build -o "$workdir/nucleus" ./cmd/nucleus

echo "== build the example"
(cd "$EXAMPLE_DIR" && go build -o "$workdir/mvc_api" .)

echo "== migrate (throwaway database)"
(cd "$EXAMPLE_DIR" && NUCLEUS_DATABASES__DEFAULT__URL="$db_url" \
  "$workdir/nucleus" migrate --config config/nucleus.yaml --migrations migrations up) \
  || fail "nucleus migrate up"

echo "== boot on :$port"
# exec so $! is the app itself, not a wrapping subshell.
(cd "$EXAMPLE_DIR" && NUCLEUS_PORT="$port" NUCLEUS_DATABASES__DEFAULT__URL="$db_url" \
  exec "$workdir/mvc_api" >"$workdir/app.log" 2>&1) &
app_pid=$!

ready=0
for _ in $(seq 1 30); do
  if curl -sf "$BASE_URL/healthz" >/dev/null 2>&1; then
    ready=1
    break
  fi
  if ! kill -0 "$app_pid" 2>/dev/null; then
    fail "the example exited before becoming ready (a missing driver import fails exactly here)"
  fi
  sleep 1
done
[[ "$ready" -eq 1 ]] || fail "the example did not answer /healthz within 30s"
echo "OK: /healthz answers"

echo "== the notes resource (touches the database)"
created=$(curl -sf -X POST "$BASE_URL/notes" \
  -H 'Content-Type: application/json' \
  -d '{"title":"smoke","body":"created by run_mvc_api_smoke"}') || fail "POST /notes"
echo "$created" | grep -q '"smoke"' || fail "POST /notes did not echo the note: $created"

list=$(curl -sf "$BASE_URL/notes") || fail "GET /notes"
echo "$list" | grep -q '"smoke"' || fail "created note missing from GET /notes: $list"
echo "OK: POST /notes creates and GET /notes reads it back"

echo "PASS: examples/mvc_api boots and serves its resource"
