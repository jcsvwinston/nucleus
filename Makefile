# Nucleus repository Makefile.
#
# This file is the canonical entry point for local builds and the contract for
# the CI pipeline. `make ci` reproduces (modulo external services) what the
# GitHub Actions workflow runs.
#
# Targets are grouped:
#   * proto-*    — the admin observability proto (see admin/proto/)
#   * ui-*       — the admin observability web UI (see admin/ui/)
#   * server-*   — the standalone admin server binary
#   * agent-*    — the agent module (admin/agent/)
#   * core-*     — the root Go module (the framework + pkg/admin Data Studio)
#   * top-level  — composite targets (test, lint, build, ci)
#
# All paths are relative to the repository root. `cd` is used inside recipes
# so each target works regardless of the current working directory.

SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

ROOT := $(shell pwd)

# Allow callers to override executables (e.g. `make BUF=/opt/buf proto`).
GO         ?= go
BUF        ?= buf
NPM        ?= npm
GOLANGCI   ?= golangci-lint

# Output binaries land here; .gitignored at the repo root.
BIN_DIR    := $(ROOT)/bin
ADMIN_BIN  := $(BIN_DIR)/admin-server

# ----------------------------------------------------------------------------
# help — keep this first so a bare `make` is friendly.
# ----------------------------------------------------------------------------
.PHONY: help
help: ## Show this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} \
	  /^[a-zA-Z0-9_.-]+:.*?##/ { printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2 }' \
	  $(MAKEFILE_LIST)

# ----------------------------------------------------------------------------
# proto — generate Go and TypeScript stubs from admin/proto/**/*.proto.
# ----------------------------------------------------------------------------
.PHONY: proto proto-lint proto-breaking proto-format proto-clean
proto: ## Regenerate Go + TypeScript stubs from the .proto files.
	@command -v $(BUF) >/dev/null 2>&1 || { \
	  echo "buf not found on PATH. Install: https://buf.build/docs/installation/"; exit 1; }
	cd admin/proto && $(BUF) generate

proto-lint: ## Run buf lint against the proto module.
	@command -v $(BUF) >/dev/null 2>&1 || { \
	  echo "buf not found on PATH. Install: https://buf.build/docs/installation/"; exit 1; }
	cd admin/proto && $(BUF) lint

proto-breaking: ## Fail if the proto introduces breaking changes vs origin/main.
	@command -v $(BUF) >/dev/null 2>&1 || { \
	  echo "buf not found on PATH. Install: https://buf.build/docs/installation/"; exit 1; }
	cd admin/proto && $(BUF) breaking --against '$(ROOT)/.git#branch=main,subdir=admin/proto'

proto-format: ## Format all .proto files in place.
	@command -v $(BUF) >/dev/null 2>&1 || { \
	  echo "buf not found on PATH. Install: https://buf.build/docs/installation/"; exit 1; }
	cd admin/proto && $(BUF) format -w

proto-clean: ## Remove generated stubs (Go + TS).
	rm -rf admin/proto/gen/go admin/ui/src/gen/*
	@touch admin/proto/gen/.gitkeep admin/ui/src/gen/.gitkeep

# ----------------------------------------------------------------------------
# ui — admin observability web UI (admin/ui/).
# Note: pkg/admin/ui (the legacy Data Studio UI) is built by its own scripts
# and is not part of these targets. See pkg/admin/build-ui.sh.
# ----------------------------------------------------------------------------
.PHONY: ui-install ui-dev ui-build ui-typecheck ui-lint ui-clean
ui-install: ## Install admin UI npm dependencies (uses npm ci for a clean install).
	cd admin/ui && $(NPM) ci

ui-dev: ## Start Vite dev server on :5173 (proxies Connect-RPC to :8080).
	cd admin/ui && $(NPM) run dev

ui-build: ## Type-check and build the admin UI to admin/ui/dist/.
	cd admin/ui && $(NPM) run build

ui-typecheck: ## Run tsc --noEmit on the admin UI.
	cd admin/ui && $(NPM) run typecheck

ui-lint: ## Lint the admin UI (eslint, max 0 warnings).
	cd admin/ui && $(NPM) run lint

ui-clean: ## Remove admin UI build artefacts and node_modules.
	rm -rf admin/ui/dist admin/ui/node_modules

# ----------------------------------------------------------------------------
# server — admin observability server binary.
# ----------------------------------------------------------------------------
.PHONY: server-build server-dev server-test
server-build: ## Build the admin-server binary into bin/admin-server.
	mkdir -p $(BIN_DIR)
	cd admin/server && $(GO) build -o $(ADMIN_BIN) ./cmd/admin-server

server-dev: ## Run the admin-server binary against the local checkout.
	cd admin/server && $(GO) run ./cmd/admin-server

server-test: ## Test the admin/server module.
	cd admin/server && $(GO) test ./...

# ----------------------------------------------------------------------------
# agent — embedded observability agent.
# ----------------------------------------------------------------------------
.PHONY: agent-test
agent-test: ## Test the admin/agent module.
	cd admin/agent && $(GO) test ./...

# ----------------------------------------------------------------------------
# core — root Nucleus module (framework + pkg/admin Data Studio).
# ----------------------------------------------------------------------------
.PHONY: core-test core-test-race core-build core-vet
core-test: ## go test ./... in the root module.
	$(GO) test ./...

core-test-race: ## Race-detector test pass over the hot packages.
	$(GO) test -race ./pkg/... ./internal/cli ./cmd/nucleus

core-vet: ## go vet ./... in the root module.
	$(GO) vet ./...

core-build: ## go build ./... in the root module.
	$(GO) build ./...

# ----------------------------------------------------------------------------
# Composite targets.
# ----------------------------------------------------------------------------
.PHONY: build test lint ci all
build: core-build server-build ui-build ## Build core, admin server, and admin UI.

test: core-test agent-test server-test ## Run all Go tests across modules.

lint: proto-lint core-vet ui-lint ## Lint Go (vet), proto (buf), and UI (eslint).
	@command -v $(GOLANGCI) >/dev/null 2>&1 && $(GOLANGCI) run ./... || \
	  echo "[hint] golangci-lint not installed; skipping. Install: https://golangci-lint.run/"

ci: lint proto-breaking test ui-typecheck ui-build server-build ## What CI runs locally.
	@echo ""
	@echo "All CI gates passed locally."

all: ci ## Alias for ci.
