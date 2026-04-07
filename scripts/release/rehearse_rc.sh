#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

if command -v go >/dev/null 2>&1; then
  :
else
  echo "error: go is required" >&2
  exit 1
fi

if command -v node >/dev/null 2>&1; then
  :
else
  echo "error: node is required" >&2
  exit 1
fi

if command -v goreleaser >/dev/null 2>&1; then
  GORELEASER_CMD=(goreleaser)
elif command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  GORELEASER_CMD=(docker run --rm -v "$ROOT_DIR:/workspace" -w /workspace goreleaser/goreleaser:v2.14.1)
else
  # The pinned version keeps a stable CLI surface for rehearsal.
  GORELEASER_CMD=(env GONOSUMDB=github.com/goreleaser/goreleaser/v2 go run github.com/goreleaser/goreleaser/v2@v2.14.1)
fi

echo "[1/7] Running Go tests"
go test ./...

echo "[2/7] Running MVC/API/Admin smoke test"
go test ./examples/mvc_api -run TestExampleMVCAPIAdmin_Smoke -v

echo "[3/7] Checking Admin UI JavaScript syntax"
node --check pkg/admin/ui/components.js
node --check pkg/admin/ui/app.js

echo "[4/7] Validating GoReleaser configuration"
"${GORELEASER_CMD[@]}" check

echo "[5/7] Building snapshot artifacts (no publish)"
"${GORELEASER_CMD[@]}" release --snapshot --clean --skip=publish --skip=announce

REPORT_DIR="dist/reports"
mkdir -p "$REPORT_DIR"

echo "[6/7] Generating compatibility report artifact"
bash scripts/release/generate_compatibility_report.sh \
  --output "$REPORT_DIR/compatibility_report.md" \
  --enforce-threshold

echo "[7/7] Generating dependency impact report artifact"
bash scripts/release/generate_dependency_impact_report.sh \
  --output "$REPORT_DIR/dependency_impact_report.md"

echo "Release rehearsal completed successfully."
