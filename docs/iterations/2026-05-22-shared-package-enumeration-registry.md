# Iteration archive — 2026-05-22 shared package-enumeration registry

> Archived 2026-05-22 as part of the session-end `/handoff`. Landed as the
> feature commit `6e6a075` (`test(contracts): single registry for freeze +
> firewall package sets`) on `main`, followed by the usual
> `chore(state): close` state commit. Candidate #1 from the prior queue
> (surfaced by architect-reviewer 2026-05-21 during the freeze-scanner
> coverage-gap iteration).

## Goal

Replace the two hand-maintained `pkg/*` package slices in the contract
scanners — `contracts/freeze_test.go` (API no-removal freeze) and
`contracts/firewall_test.go` (third-party type-leak scan) — with a single
`allPublicPackages()` source-of-truth that each scanner derives from via a
filter predicate, so coverage omissions become machine-visible rather than
discovered by accident. The drift was real: `pkg/observability` had been
silently absent from both lists until an architect-reviewer caught it.

## Scope

### In

- New `contracts/packages_test.go`:
  - `publicPackage{relative, lifecycle, frozen, firewalled, note}` and a
    `lifecycle` string type (`stable` / `transitional` / `experimental` /
    `uninventoried`).
  - `allPublicPackages()` — the authoritative registry of all 21 top-level
    `pkg/*` packages, sorted by path, with each deliberate scanner exclusion
    documented in a per-row `note` and a header block.
  - `frozenPackages()` / `firewalledPackages()` filter helpers and an
    `importPath()` method (import paths derive from a `modulePath` const).
  - Two guard tests:
    `TestPublicPackages_RegistryMatchesFilesystem` (a top-level `pkg/*` dir
    with Go source absent from the registry — or a stale row pointing at a
    nonexistent dir — fails the test) and
    `TestPublicPackages_FrozenMatchesLifecycle` (`frozen` iff
    `lifecycle == stable`).
  - Discovery helpers `discoverTopLevelPublicPackages` + `hasGoSource`
    (non-recursive; build constraints are not evaluated, documented inline).
- `contracts/freeze_test.go` — `stableAPISymbolBaselineLines` now loops over
  `frozenPackages()` instead of the inline slice; deliberate-omission prose
  moved into the registry, replaced with a short pointer comment.
- `contracts/firewall_test.go` — `TestFirewall_NoThirdPartyTypesInStableAPIs`
  now uses `firewalledPackages()`; firewall-vs-freeze divergence prose moved
  into the registry.

### Out

- **Nested-package coverage.** The scanners and the new registry/guard cover
  only top-level `pkg/*` (the scanners parse one dir non-recursively). Four
  nested public packages remain uncovered: `pkg/auth/secrets`,
  `pkg/observability/hooks`, `pkg/tasks/providers/asynq`,
  `pkg/tasks/providers/memory`. Deferred to a new candidate #1 (not a
  regression — never covered).
- **`pkg/observability` lifecycle decision.** Tagged `uninventoried` for now;
  giving it an inventory row + posture is candidate #2.

## Acceptance criteria — all met

- [x] Behaviour-preserving: `contracts/baseline/api_exported_symbols.txt`
  unchanged; freeze passes without `NUCLEUS_UPDATE_CONTRACT_BASELINE=1`.
- [x] Freeze set still exactly 17 packages; firewall set still exactly 18.
- [x] `go test ./...` green; `gofmt` / `go vet` clean.
- [x] Iteration loop: architect-reviewer PASS, code-reviewer NITS (all
  addressed: gofmt doc-comment reflow, single `allPublicPackages()` call in
  the filter helpers, build-tag note), contract-guardian PASS (behaviour
  preservation verified on all 4 points), test-runner PASS.

## Outcome

Landed as feature commit `6e6a075`. The two scanners now share one registry;
the `frozen`/`firewalled` booleans reproduce the previous freeze (17) and
firewall (18) sets exactly. A new `pkg/*` directory, or a `stable` promotion,
now surfaces as a registry change (or a red guard test) instead of a silent
gap.

## Follow-ups opened

- **Candidate #1 (new): nested-package contract coverage** — recursive
  discovery + a lifecycle decision per nested package; natural trigger is the
  first nested package promoted to `stable`.
- **Candidate #2 (carried): `pkg/observability` inventory entry + lifecycle**
  — flip its row from `lifecycleUninventoried` once decided; the
  frozen⟺lifecycle invariant will enforce consistency.

## Notes / decisions log

- 2026-05-22 — Implemented and reviewed in one session. Scope held to
  top-level `pkg/*` deliberately to keep the change behaviour-preserving and
  reviewable; nested coverage split out. `lifecycleUninventoried` sentinel
  introduced so the registry can carry `pkg/observability` honestly (no
  inventory row yet) while the frozen⟺lifecycle==stable guard still forces
  `frozen=false` for it. Pre-existing WARN (not introduced here): the
  `baselinePath`/`repoRoot` derivation uses `runtime.Caller(0)`, so the
  contract tests assume they run against the source tree.
