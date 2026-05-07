#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Validate stable contract freeze baselines (no removals) for CLI contracts, config, and stable API symbols.
Also runs firewall tests to prevent third-party type leaks in stable APIs.

Usage:
  bash scripts/ci/check_contract_freeze.sh
USAGE
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

if [[ -z "${GOCACHE:-}" ]]; then
  export GOCACHE="$(pwd)/.cache/go-build"
fi
mkdir -p "$GOCACHE"

go test ./contracts -run '^TestContractFreeze_|^TestFirewall_' -count=1 -v
