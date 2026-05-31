# Iteration Archive — 2026-05-29 docs: align Go floor to 1.26 across shipped docs (audit Block 8 — README go-version cross-check)

> Archived by `session-curator` on 2026-05-29.
> Status at archival: **COMPLETE** — PR #88 squash-merged to `main`
> (commit `6ce4831`, "docs: align Go floor to 1.26 across shipped docs
> (audit Block 8) (#88)"). All 12 CI checks passed.

---

## Goal

Replace every stale `Go 1.25+` floor claim across shipped documentation with
the correct `Go 1.26+ (matches the go 1.26.3 directive in go.mod)` string,
resolving audit Block 8 item OTH-2 / README go-version cross-check from
`docs/audits/2026-05-29-exhaustive-audit.md`.

The `Go 1.25+` claim became a lie when the Phase 4 scaffold (PR #84,
2026-05-29) moved `go.mod` to `go 1.26.3`. Five prose documents and one
plugin-build fixture still advertised 1.25 after that merge.

## Scope

- in: `README.md`, `docs/QUICKSTART.md`, `CONTRIBUTING.md`,
  `docs/reference/DEVELOPER_MANUAL.md`,
  `docs/governance/ENTERPRISE_LONG_TERM_ROADMAP.md`,
  `docs/guides/TESTING_GUIDE.md` (plugin-build fixture `go.mod` block),
  `CHANGELOG.md` (new `[Unreleased] § Documentation` entry).
- out: any `pkg/` changes, any `internal/` changes, new features, tests,
  contracts, behaviour changes. Historical `CHANGELOG` entries (lines
  432/535/691 describing prior released versions) intentionally left
  untouched. `website/installation.md`, `CLAUDE.md`,
  `examples/mvc_api/README.md`, and `admin/BENCHMARKS.md` already carried
  the correct floor — no edit needed.

## Acceptance criteria

- [x] `README.md` line 240: `Go 1.25+` → `Go 1.26+ (matches the go 1.26.3 directive in go.mod)`.
- [x] `docs/QUICKSTART.md` line 10: same substitution.
- [x] `CONTRIBUTING.md` line 10: same substitution.
- [x] `docs/reference/DEVELOPER_MANUAL.md` line 49: same substitution.
- [x] `docs/governance/ENTERPRISE_LONG_TERM_ROADMAP.md` line 435: same substitution.
- [x] `docs/guides/TESTING_GUIDE.md` line 540: plugin-build fixture `go.mod`
      block bumped `go 1.25` → `go 1.26` for consistency with the real floor.
- [x] `CHANGELOG.md`: new entry under `[Unreleased] § Documentation`
      recording the go-version floor correction.
- [x] All 12 CI checks passed on PR #88 (no tracked Go code was changed;
      build matrix + contract freeze unaffected).
- [x] PR opened and merged via the protected-`main` flow — PR #88
      squash-merged 2026-05-29; `main` advanced `de5a922..6ce4831`.

## Status at archival: COMPLETE

### Done

- 2026-05-29 — `README.md`, `docs/QUICKSTART.md`, `CONTRIBUTING.md`,
  `docs/reference/DEVELOPER_MANUAL.md`,
  `docs/governance/ENTERPRISE_LONG_TERM_ROADMAP.md`: prose floor claim
  updated from `Go 1.25+` to `Go 1.26+ (matches the go 1.26.3 directive
  in go.mod)`.
- 2026-05-29 — `docs/guides/TESTING_GUIDE.md`: plugin-build fixture
  `go.mod` code block bumped `go 1.25` → `go 1.26`.
- 2026-05-29 — `CHANGELOG.md`: `[Unreleased] § Documentation` entry added
  documenting the go-version floor correction across the five prose files.
- 2026-05-29 — PR #88 squash-merged to `main`; `main` now at commit
  `6ce4831`. 7 files changed (+7/−6).

## Notes / decisions log

- 2026-05-29 — Resolves audit Block 8 OTH-2 / README go-version cross-check
  (`docs/audits/2026-05-29-exhaustive-audit.md`): the `Go 1.25+` floor was
  introduced when the repository was initialised against Go 1.25; the Phase 4
  scaffold (PR #84) advanced `go.mod` to `go 1.26.3` but doc updates lagged.
- 2026-05-29 — Audit Block 8 also contained: FW-6 (CORS) — shipped via PR
  #82 (2026-05-29-audit-remediation); OTH-1 (examples + CLAUDE.md map) —
  shipped via PR #86 (2026-05-29-examples-reconciliation). With OTH-2 / this
  PR closed, **Block 8 is fully complete**.
- 2026-05-29 — **MILESTONE: This PR (#88) closes the LAST outstanding item
  from the 2026-05-29 exhaustive audit. The entire audit (Blocks 1-8) is
  now fully remediated:**
  - Blocks 1-5 (P0/P1 framework + CLI): shipped via PR #82.
  - Blocks 6-7 (P0 docs): shipped via PR #82.
  - Block 8 (P2 hygiene): FW-6 shipped via PR #82; OTH-1 shipped via PR #86;
    OTH-2 (README go-version cross-check) shipped via PR #88.
- 2026-05-29 — Semver impact: **none**. Pure documentation correction; no
  shipped behaviour changed; no new public API, CLI, or config key.
- 2026-05-29 — All CI passed: 12/12 checks green on PR #88.
- 2026-05-29 — This iteration was scope-light and was not pre-written into
  `CURRENT_ITERATION.md`; this archive is the primary record. References:
  `docs/audits/2026-05-29-exhaustive-audit.md` (full audit, now fully
  remediated — useful reference for what was done across all eight blocks).
