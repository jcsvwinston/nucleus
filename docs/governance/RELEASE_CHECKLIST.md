# Release Checklist

> **v1.0 specifically:** the qualitative gate lives in [`docs/V1_GATE.md`](../V1_GATE.md) — every §A item closed or §B-waived before tagging.

Reference date: 2026-06-21.
Status: Current release validation checklist.

This checklist defines the required validation steps for Nucleus release candidates.

## Pre-Release Validation

### 0. Docs snapshot (minor releases only)

- [ ] Cut the documentation snapshot BEFORE the release PR merges:
      `bash scripts/release/cut_docs_snapshot.sh <X.Y.0>`
  - The suite site serves the docs of the **pinned tag**, so a snapshot
    added after the tag does not reach readers until the next release.
  - Patch releases need no snapshot: the minor's documentation still holds.
  - `scripts/ci/check_docs_archive_freshness.sh` fails when a published
    minor has no snapshot, so this step cannot be skipped silently.
- [ ] Cut it LAST among the round's documentation changes. The snapshot is a
      byte-for-byte copy of `website/docs/` at the moment you run the script,
      and it is then served forever under `/nucleus/<X.Y.0>/`. A snapshot cut
      before a prose correction lands freezes the wrong text permanently —
      and the guards will not notice, because the archived copy is
      consistent with itself. Re-cut (delete the directory, drop the entry
      from `versions.json`, run the script again) if a docs PR merges after
      you cut.

### 0b. Re-pin the sibling provider modules (EVERY release)

- [ ] Bump the framework `require` in `providers/*/go.mod` to the version
      being released MINUS one, as its own `fix(ldap): ...` commit on `main`
      BEFORE the round's release PRs merge:
      `providers/ldap/go.mod` → `github.com/jcsvwinston/nucleus v<previous>`.
  - Release PRs are cut per component (`separate-pull-requests`, see 0c),
    so the re-pin opens its own `providers/ldap` release PR. Merge that one
    BEFORE the root's: the suite's `manifest-guard §3b` refuses a module
    tag that is not an ancestor of the pinned root, so a module tagged
    after the root tag cannot be certified.
  - These modules require the ROOT of this repository, an edge that can
    never be perfectly current: any release that contains the `require`
    is by definition later than it. The suite's `manifest-guard §5b`
    tolerates exactly **one** release behind and calls anything older
    staleness.
  - **This is not optional for patch releases.** Every root release pushes
    the edge one position further back, so a release that skips this step
    is certifiable, and the NEXT one is not — which is exactly how
    v1.17.1 left the suite unable to certify until `providers/ldap` was
    re-pinned on its own.
  - The guard prints an `AVISO` (never an `ok`) while the lag exists, so
    a set that certifies with the warning is one release away from
    failing.

### 0c. Release PRs are per-component branches — do not regress it

`release-please-config.json` sets `separate-pull-requests: true`, and that
setting is what makes tagging work at all, not a style choice. Measured on
v1.18.0, v1.19.0 and v1.20.0 (merged with `autorelease: pending`, no tag
cut) against v1.17.2 and v1.20.1 (tagged fine): with a shared release PR
(branch `release-please--branches--main`), a PR that releases ONLY the root
carries a single `<details>` section with no component prefix, and
release-please's `buildRelease` then takes its "standalone PR" path, which
compares the BRANCH's component (none on the shared branch) against the
configured one (the Go module path). The mismatch logs
`PR component: undefined does not match configured component: ...`, no
release is created, and every later run aborts with
`There are untagged, merged release PRs outstanding`. A PR that happens to
carry a `providers/ldap` section next to the root's takes the
multi-component parse path instead, which matches sections by body — that
is why every failure was a root-only PR and every success had a module
riding along, and why the failure merely LOOKED correlated with minors
(they always shipped root-only). With per-component branches the branch
name carries the component and the standalone path matches.

Recovery, if a merged release PR ever sits `autorelease: pending` with no
tag again: `gh release create vX.Y.Z --target <full merge SHA>` (short
SHAs are rejected) with the CHANGELOG notes, then relabel the PR to
`autorelease: tagged` — without the relabel the next cut keeps aborting.

Same token, second consequence: tags cut with the GITHUB_TOKEN fire no
`push` events (workflow-recursion guard), so the Release Please workflow
dispatches `release.yml` explicitly after a root cut. Without that chain
every auto-tagged release ships with zero assets — measured 2026-08-30:
v1.0.0 through v1.20.1 all have none; the only releases with binaries are
the ones whose tags were pushed manually while tagging was broken.

### 1. Contract Freeze Tests

- [ ] Run contract freeze tests: `bash scripts/ci/check_contract_freeze.sh`
  - Validates no removals from stable CLI commands
  - Validates no removals from stable config key patterns
  - Validates no removals from stable API exported symbols
  - Validates no third-party type leaks in stable APIs (firewall tests)

### 2. Compatibility Harness

- [ ] Run compatibility harness: `bash scripts/ci/run_compatibility_harness.sh --min-pass-rate 100 --enforce-threshold`
  - Runs three fixture profiles (restored 2026-07-07, v1 gate A-6): `core-build` (stable-surface compilation), `mvc-api` (build + tests of `examples/mvc_api` against the current tree), and `showcase-suite` (`examples/showcase_demo` compiled against the current tree via an ephemeral `go.work`, with quark/orbit at their released tags).
  - Note: of the pre-2026-05-16 trio, `admin-heavy` is obsolete (admin extracted to the orbit module, ADR-019) and `plugin-heavy` returns with the plugin reference examples (ADR-010 Phase 4).

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
- [ ] Re-pin the examples to the fresh tag set. The repin-showcase workflow
      opens the chore PR itself after every Release Please run; after a
      SIBLING repo tags, or at train end, trigger it by hand
      (`gh workflow run repin-showcase.yml`) or run
      `bash scripts/release/repin_examples.sh` locally. The pins guard
      (`check_example_pins.sh`) tolerates one minor of lag with a WARN so a
      mid-train tag no longer reddens every open PR; the SECOND minor of
      lag goes red — the chore is still a fixed train step, just a
      mechanical one.
- [ ] Update strategic/status docs when milestone posture changes
