# Nucleus repository Makefile.
#
# Canonical entry point for local builds. `make ci` reproduces (modulo external
# services) what the GitHub Actions workflow runs for the root module.
#
# The framework is the Go module at the repository root; the database drivers,
# telemetry exporters and cloud/LDAP providers are sibling modules under
# drivers/, exporters/ and providers/ (ADR-030/031), and the CLI is one under
# cmd/nucleus (ADR-038). The admin / observability
# subsystem (the
# panel, the cluster agent, the proto + server) was extracted to the separate
# `orbit` module (ADR-019) and is no longer built from this repo.

SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

ROOT := $(shell pwd)

# Allow callers to override executables (e.g. `make GO=/opt/go/bin/go test`).
GO         ?= go
GOLANGCI   ?= golangci-lint

# The workflow linter, pinned exactly — the same rule the action pins follow,
# and the same one govulncheck follows in ci.yml. Keep in step with the version
# the `Lint the workflows themselves` step of .github/workflows/ci.yml runs.
# actionlint shells out to shellcheck for the `run:` scripts when it finds one
# on PATH; CI's ubuntu-latest ships it, a local run without it checks the
# workflow structure only.
ACTIONLINT_VERSION ?= v1.7.12

# ----------------------------------------------------------------------------
# help — keep this first so a bare `make` is friendly.
# ----------------------------------------------------------------------------
.PHONY: help
help: ## Show this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} \
	  /^[a-zA-Z0-9_.-]+:.*?##/ { printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2 }' \
	  $(MAKEFILE_LIST)

# ----------------------------------------------------------------------------
# core — the framework (the root module), the CLI (cmd/nucleus, a module of its
# own) and internal/testdeps (the tests the framework's go.mod must not carry).
#
# The CLI requires the framework at a release and links the five driver
# modules, so it is built against this tree through a workspace. These targets
# keep theirs under .tmp/ (ignored), so a go.work of your own is left alone;
# `make workspace` writes one at the root for an editor.
# ----------------------------------------------------------------------------
CLI_WORK    := $(ROOT)/.tmp/cli.go.work
CLI_DRIVERS := drivers/postgres drivers/mysql drivers/sqlite drivers/mssql drivers/oracle

.PHONY: build test test-race vet fuzz workspace hello-size cli-workspace
cli-workspace:
	@mkdir -p .tmp
	@bash scripts/ci/cli_workspace.sh $(CLI_WORK) $(CLI_DRIVERS)

workspace: ## Write ./go.work linking the CLI and the driver modules to this tree (git ignores it).
	bash scripts/ci/cli_workspace.sh go.work $(CLI_DRIVERS)

build: cli-workspace ## go build the framework and the CLI.
	$(GO) build ./...
	cd cmd/nucleus && GOWORK=$(CLI_WORK) $(GO) build ./...

test: cli-workspace ## go test the framework, internal/testdeps and the CLI (this replays every fuzz seed corpus).
	$(GO) test ./...
	cd internal/testdeps && GOWORK=off $(GO) test ./...
	cd cmd/nucleus && GOWORK=$(CLI_WORK) $(GO) test ./...

test-race: cli-workspace ## Race-detector test pass over the hot packages and the CLI.
	$(GO) test -race ./pkg/... ./internal/cli
	cd cmd/nucleus && GOWORK=$(CLI_WORK) $(GO) test -race ./...

fuzz: ## Mutate the parsing surfaces for 5s per target (FUZZTIME=60s for a real hunt; CI replays the seeds on every PR and mutates weekly).
	bash scripts/ci/run_fuzz_targets.sh --fuzz

vet: cli-workspace ## go vet the framework, internal/testdeps and the CLI.
	$(GO) vet ./...
	cd internal/testdeps && GOWORK=off $(GO) vet ./...
	cd cmd/nucleus && GOWORK=$(CLI_WORK) $(GO) vet ./...

hello-size: ## Measure hello and hello+sqlite against the ceilings the CI lane enforces.
	bash scripts/ci/check_hello_size.sh

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
	@echo "check OK — heavier required lanes run in CI (db matrix, jobs-redis, storage-minio, sibling modules standalone, example smokes)."

guards: ## The repo guards CI enforces that run fine locally.
	./scripts/ci/check_version_claims.sh
	bash scripts/ci/check_docs_product_voice.sh
	bash scripts/ci/check_retired_claims.sh
	bash scripts/ci/check_adr_index.sh
	bash scripts/ci/check_versioned_docs_markers.sh
	bash scripts/ci/check_internal_docs_drift.sh
	bash scripts/ci/check_docs_archive_freshness.sh
	bash scripts/ci/check_contract_freeze.sh
	bash scripts/ci/check_action_pins.sh
	$(GO) run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION) -no-color
	$(GO) run ./scripts/website/gen-config-reference
	@git diff --quiet website/docs/reference/configuration.md || 	  { echo "config reference stale: commit the regenerated website/docs/reference/configuration.md"; exit 1; }
	bash scripts/website/check-coverage.sh --strict

regen-baselines: ## Regenerate the frozen API/CLI baselines after an intentional surface change.
	NUCLEUS_UPDATE_CONTRACT_BASELINE=1 $(GO) test ./contracts/ -run 'APIExportedSymbols|CLIJSON' -count=1
	@echo "Regenerated. config_key_patterns.txt and cli_primary_commands.txt are maintained BY HAND — update them in the same change if your surface touched them."

all: ci ## Alias for ci.
