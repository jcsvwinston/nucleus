# Release Checklist

> **v1.0 specifically:** the qualitative gate lives in [`docs/V1_GATE.md`](../V1_GATE.md) — every §A item closed or §B-waived before tagging.

Reference date: 2026-06-21.
Status: Current release validation checklist.

This checklist defines the required validation steps for Nucleus release candidates.

## Pre-Release Validation

### 1. Contract Freeze Tests

- [ ] Run contract freeze tests: `bash scripts/ci/check_contract_freeze.sh`
  - Validates no removals from stable CLI commands
  - Validates no removals from stable config key patterns
  - Validates no removals from stable API exported symbols
  - Validates no third-party type leaks in stable APIs (firewall tests)

### 2. Compatibility Harness

- [ ] Run compatibility harness: `bash scripts/ci/run_compatibility_harness.sh --min-pass-rate 100 --enforce-threshold`
  - Tests the `core-build` profile (`go build ./pkg/... ./cmd/nucleus ./internal/cli/...`)
  - Note: the previous fixture profiles (`minimal-api`, `admin-heavy`, `plugin-heavy`) were removed in the ADR-010 Phase 1 iteration on 2026-05-16; they return in v0.9.X with the new reference applications (ADR-010 Phase 4). Until then, this lane covers stable-surface compilation only.

### 3. Dependency Impact Report

- [ ] Generate dependency impact report: `bash scripts/release/generate_dependency_impact_report.sh --enforce-critical-review`
  - Tracks direct dependency changes
  - Flags critical dependency version bumps
  - Validates no new third-party types in stable APIs
  - Confirms firewall tests pass

### 4. Full Compatibility Report

- [ ] Generate full compatibility report: `bash scripts/release/generate_compatibility_report.sh --enforce-threshold`
  - Combines fixture harness results
  - Combines stable contract test results
  - Provides overall compatibility decision
  - Must output "READY" for release to proceed

## 5. Test Suite

- [ ] Run full test suite: `go test ./...`
- [ ] Ensure all critical root-module packages pass (app, router, model, db, auth)

## 6. Documentation and Changelog

- [ ] Ensure `CHANGELOG.md` includes all user-facing changes
- [ ] Ensure README and relevant docs match shipped behavior

## 7. Version and Tag

- [ ] Confirm target version (`v0.x.y` or `v0.x.y-rcN`)
- [ ] Create and push tag from a clean `main` commit

## 8. CI/Release Workflows

Verify:

- [ ] `CI Required Gate` green — all constituent jobs pass: `test` (includes `govulncheck ./...`, blocking), `db-matrix-required`, `db-matrix-live-mssql`, `db-matrix-live-oracle`, `compatibility-harness`, `contract-freeze`
- [ ] Release workflow completes
- [ ] Release asset smoke checks pass

## 9. Compatibility Gates (Mandatory)

Before tagging, attach and review:

- [ ] Compatibility report (fixture app + stable contract summary)
- [ ] Exploratory DB stability report (when any engines remain exploratory; currently none — mssql/oracle are required as of 2026-05-12)
- [ ] Dependency impact report for critical dependencies
- [ ] Explicit manual critical-dependency review note (for releases where impact report flags critical changes)
- [ ] Contract inventory review (`API`/`CLI`/`config` lifecycle tags)
- [ ] Deprecation notice + migration assistant docs (when active deprecations exist)
- [ ] Explicit compatibility statement:
  - `no breaking changes`, or
  - `major-only breaking changes with migration plan`

Policy reference:

- `docs/governance/COMPATIBILITY_SLO.md`

Local generation commands:

```bash
bash scripts/ci/run_compatibility_harness.sh --output docs/reports/compatibility_harness_latest.md --enforce-threshold
bash scripts/release/generate_compatibility_report.sh --output dist/reports/compatibility_report.md --enforce-threshold
bash scripts/release/generate_dependency_impact_report.sh --output dist/reports/dependency_impact_report.md
# optional but recommended when critical dependency changes are detected:
# docs/reports/dependency_critical_review_<date>.md
```

Contract inventory references:

- `docs/reference/API_CONTRACT_INVENTORY.md`
- `docs/reference/CLI_CONTRACT_MATRIX.md`
- `docs/reference/CONFIG_KEY_REGISTRY.md`
- `docs/governance/DEPRECATION_TEMPLATE.md`
- `docs/governance/MIGRATION_ASSISTANT_CONVENTIONS.md`

## 10. Artifact Review

- [ ] Check release artifacts include expected OS/arch matrix and checksums

## 11. Post-Release

- [ ] Verify `nucleus version` prints the expected release version
- [ ] Update strategic/status docs when milestone posture changes
