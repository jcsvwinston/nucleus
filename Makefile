# Nucleus repository Makefile.
#
# Canonical entry point for local builds. `make ci` reproduces (modulo external
# services) what the GitHub Actions workflow runs for the root module.
#
# Nucleus is a single Go module at the repository root. The admin / observability
# subsystem (the panel, the cluster agent, the proto + server) was extracted to
# the separate `orbit` module (ADR-019) and is no longer built from this repo.

SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

ROOT := $(shell pwd)

# Allow callers to override executables (e.g. `make GO=/opt/go/bin/go test`).
GO         ?= go
GOLANGCI   ?= golangci-lint

# ----------------------------------------------------------------------------
# help — keep this first so a bare `make` is friendly.
# ----------------------------------------------------------------------------
.PHONY: help
help: ## Show this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} \
	  /^[a-zA-Z0-9_.-]+:.*?##/ { printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2 }' \
	  $(MAKEFILE_LIST)

# ----------------------------------------------------------------------------
# core — the root Nucleus module (the framework).
# ----------------------------------------------------------------------------
.PHONY: build test test-race vet
build: ## go build ./... in the root module.
	$(GO) build ./...

test: ## go test ./... in the root module.
	$(GO) test ./...

test-race: ## Race-detector test pass over the hot packages.
	$(GO) test -race ./pkg/... ./internal/cli ./cmd/nucleus

vet: ## go vet ./... in the root module.
	$(GO) vet ./...

# ----------------------------------------------------------------------------
# Composite targets.
# ----------------------------------------------------------------------------
.PHONY: lint ci all
lint: vet ## Lint Go (vet + golangci-lint if installed).
	@command -v $(GOLANGCI) >/dev/null 2>&1 && $(GOLANGCI) run ./... || \
	  echo "[hint] golangci-lint not installed; skipping. Install: https://golangci-lint.run/"

ci: lint test ## Legacy alias — prefer `make check`, which also runs the guards.
	@echo ""
	@echo "All CI gates passed locally."

.PHONY: check guards regen-baselines
check: vet guards test ## The cheap CI lanes: vet, every local guard, tests. Run before opening a PR.
	@echo ""
	@echo "check OK — heavier required lanes run in CI (db matrix, jobs-redis, storage-minio, showcase smoke)."

guards: ## The repo guards CI enforces that run fine locally.
	./scripts/ci/check_version_claims.sh
	bash scripts/ci/check_docs_product_voice.sh
	bash scripts/ci/check_adr_index.sh
	bash scripts/ci/check_internal_docs_drift.sh
	bash scripts/ci/check_docs_archive_freshness.sh
	bash scripts/ci/check_example_pins.sh
	bash scripts/ci/check_contract_freeze.sh
	$(GO) run ./scripts/website/gen-config-reference
	@git diff --quiet website/docs/reference/configuration.md || 	  { echo "config reference stale: commit the regenerated website/docs/reference/configuration.md"; exit 1; }
	bash scripts/website/check-coverage.sh --strict

regen-baselines: ## Regenerate the frozen API/CLI baselines after an intentional surface change.
	NUCLEUS_UPDATE_CONTRACT_BASELINE=1 $(GO) test ./contracts/ -run 'APIExportedSymbols|CLIJSON' -count=1
	@echo "Regenerated. config_key_patterns.txt and cli_primary_commands.txt are maintained BY HAND — update them in the same change if your surface touched them."

all: ci ## Alias for ci.
