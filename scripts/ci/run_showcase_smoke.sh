#!/usr/bin/env bash
# run_showcase_smoke.sh — EXECUTES examples/showcase_demo and asserts both
# integration cases end to end (7ª ronda, QM7-1c).
#
# The example spent eight minor releases pinned to a prehistoric dependency
# set because nothing in CI ever ran it — it was not even built. This smoke
# boots the app exactly as a reader would (standalone module build, GOWORK
# off, SQLite) and exercises:
#
#   Caso 1 (quarkbridge): the shop API routes, whose handlers run Quark
#     through the bridged client — list the seeded article, create one, and
#     read it back.
#   Caso 2 (quarkdatasource): Orbit's Data Studio backed by the Quark
#     models — log into /admin with the bootstrap credentials and list the
#     registered models and their rows through the admin API.
#
# Exit 0 only when every assertion holds. The app log is dumped on failure.
set -euo pipefail

cd "$(dirname "$0")/../.."
EXAMPLE_DIR="examples/showcase_demo"
BASE_URL="http://127.0.0.1:8091"

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
  cat "$workdir/app.log" >&2 || true
  exit 1
}

echo "== build (standalone, GOWORK=off)"
(cd "$EXAMPLE_DIR" && GOWORK=off go build -o "$workdir/showcase" .)

echo "== boot"
cp "$EXAMPLE_DIR/nucleus.yaml" "$workdir/"
# exec so $! is the app itself, not a wrapping subshell — otherwise the
# cleanup kill reaps the subshell and leaves the app orphaned on :8091.
(cd "$workdir" && exec ./showcase >"$workdir/app.log" 2>&1) &
app_pid=$!

ready=0
for _ in $(seq 1 60); do
  if curl -sf "$BASE_URL/healthz" >/dev/null 2>&1; then
    ready=1
    break
  fi
  if ! kill -0 "$app_pid" 2>/dev/null; then
    fail "the app exited before becoming ready"
  fi
  sleep 1
done
[[ "$ready" -eq 1 ]] || fail "the app did not answer /healthz within 60s"
echo "OK: /healthz answers"

echo "== caso 1: shop API through the quarkbridge-wrapped client"
list=$(curl -sf "$BASE_URL/api/articles") || fail "GET /api/articles"
echo "$list" | grep -q '"Hello, Quantum"' \
  || fail "seeded article missing from GET /api/articles: $list"
echo "OK: GET /api/articles returns the seeded article"

created=$(curl -sf -X POST "$BASE_URL/api/articles" \
  -H 'Content-Type: application/json' \
  -d '{"author_id":1,"title":"smoke-probe","body":"created by run_showcase_smoke"}') \
  || fail "POST /api/articles"
echo "$created" | grep -q '"smoke-probe"' \
  || fail "POST /api/articles did not echo the created article: $created"

relist=$(curl -sf "$BASE_URL/api/articles") || fail "GET /api/articles (re-list)"
echo "$relist" | grep -q '"smoke-probe"' \
  || fail "created article missing from re-list: $relist"
echo "OK: POST /api/articles creates and the row reads back"

echo "== caso 2: Data Studio backed by quarkdatasource"
jar="$workdir/cookies.txt"
login_code=$(curl -s -o "$workdir/login.out" -w '%{http_code}' -c "$jar" \
  -X POST "$BASE_URL/admin/login" \
  --data-urlencode 'username=admin' \
  --data-urlencode 'password=showcase-demo') || fail "POST /admin/login"
case "$login_code" in
  2??|3??) ;;
  *) fail "admin login answered HTTP $login_code: $(cat "$workdir/login.out")" ;;
esac

models=$(curl -sf -b "$jar" "$BASE_URL/admin/api/models") \
  || fail "GET /admin/api/models (is the session cookie missing?)"
echo "$models" | grep -q 'Author' || fail "Author model missing from Data Studio: $models"
echo "$models" | grep -q 'Article' || fail "Article model missing from Data Studio: $models"
echo "OK: Data Studio lists the Quark-backed models"

authors=$(curl -sf -b "$jar" "$BASE_URL/admin/api/models/Author") \
  || fail "GET /admin/api/models/Author"
echo "$authors" | grep -q 'Ada Lovelace' \
  || fail "seeded author missing from Data Studio records: $authors"
echo "OK: Data Studio serves the Quark rows (seeded author present)"

echo "PASS: showcase_demo smoke — both integration cases answered"
