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

if command -v goreleaser >/dev/null 2>&1; then
  GORELEASER_CMD=(goreleaser)
elif command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  GORELEASER_CMD=(docker run --rm -v "$ROOT_DIR:/workspace" -w /workspace goreleaser/goreleaser:v2.14.1)
else
  # The pinned version keeps a stable CLI surface for rehearsal.
  GORELEASER_CMD=(env GONOSUMDB=github.com/goreleaser/goreleaser/v2 go run github.com/goreleaser/goreleaser/v2@v2.14.1)
fi

echo "[1/5] Running Go tests"
go test ./...

echo "[2/5] Validating GoReleaser configuration"
"${GORELEASER_CMD[@]}" check

echo "[3/5] Building snapshot artifacts (no publish)"
"${GORELEASER_CMD[@]}" release --snapshot --clean --skip=publish --skip=announce

REPORT_DIR="dist/reports"
mkdir -p "$REPORT_DIR"

echo "[4/5] Generating compatibility report artifact"
bash scripts/release/generate_compatibility_report.sh \
  --output "$REPORT_DIR/compatibility_report.md" \
  --enforce-threshold

echo "[5/5] Generating dependency impact report artifact"
bash scripts/release/generate_dependency_impact_report.sh \
  --output "$REPORT_DIR/dependency_impact_report.md"

echo "Release rehearsal completed successfully."
