# Iteration archive — 2026-05-21 freeze-scanner package-coverage gap

> Archived 2026-05-21 as part of the session-end `/handoff`. Landed as a
> combined `fix(contracts)` commit on `main` (2026-05-21) — feature files
> and state-close files committed together, per the owner's decision to
> skip the usual two-commit feature-then-`chore(state)` split. Candidate
> #2 from the prior queue (the follow-up opened by PR #77 /
> constructor-gap architect-reviewer WARN).

## Goal

Add `pkg/circuit` and `pkg/health` to the API no-removal freeze baseline;
close the third-party-leak firewall scan gap for `pkg/admin`,
`pkg/health`, and `pkg/nucleus`; add a cross-reference note to
`docs/reference/API_CONTRACT_INVENTORY.md` linking the inventory's
`stable` lifecycle tag to the freeze inclusion rule.

## Scope

### In

- `contracts/freeze_test.go` — `pkg/circuit` and `pkg/health` added to
  the `packages` slice with an explicit inclusion-rule comment (freeze ==
  inventory-`stable`); four deliberate omissions documented in the same
  comment (`pkg/openapi` experimental; `pkg/outbox` transitional +
  `NewKafkaBridge` unfinished; `pkg/admin` transitional;
  `pkg/observability` no inventory entry yet).
- `contracts/firewall_test.go` — `pkg/admin`, `pkg/health`, and
  `pkg/nucleus` added to the firewall scan; a comment explains why the
  firewall list intentionally differs from the freeze list (firewall ==
  all packages wrapping a forbidden third-party import; freeze == only
  inventory-`stable`).
- `contracts/baseline/api_exported_symbols.txt` — regenerated via
  `NUCLEUS_UPDATE_CONTRACT_BASELINE=1`; +28 lines (circuit + health
  exported symbols), 0 removals.
- `docs/reference/API_CONTRACT_INVENTORY.md` — "Freeze Enforcement"
  section updated with a coupled-change note: promoting a package to
  `stable` is a coupled change (add to `freeze_test.go` + rebaseline).

### Out

- No runtime behaviour; no public API changes; no CLI surface; no
  CHANGELOG entry; no semver bump. Pure internal contract-test coverage
  improvement — matches PR #77 precedent.

## Acceptance criteria — all met

- [x] `pkg/circuit` and `pkg/health` added to the `packages` slice in
  `contracts/freeze_test.go` with an inclusion-rule comment (freeze ==
  inventory-`stable`).
- [x] Four deliberate omissions documented in the same comment
  (`pkg/openapi` experimental; `pkg/outbox` transitional +
  `NewKafkaBridge` unfinished; `pkg/admin` transitional;
  `pkg/observability` no inventory entry).
- [x] Baseline regenerated via `NUCLEUS_UPDATE_CONTRACT_BASELINE=1`;
  +28 lines (circuit + health symbols), 0 removals.
- [x] `pkg/admin`, `pkg/health`, `pkg/nucleus` added to
  `contracts/firewall_test.go`; firewall comment explains why the firewall
  list intentionally differs from the freeze list.
- [x] `docs/reference/API_CONTRACT_INVENTORY.md` "Freeze Enforcement"
  section notes that promoting a package to `stable` is a coupled change
  (add to `freeze_test.go` + rebaseline).
- [x] `go test ./...` exit 0 (28 packages, 0 failures).
- [x] `bash scripts/ci/check_contract_freeze.sh` passes.
- [x] `gofmt` + `go vet` clean.
- [x] No CHANGELOG entry and no semver bump (pure internal contract-test
  coverage, no user-facing behaviour change — matches PR #77 precedent).

## Status

### Done (2026-05-21 — combined `fix(contracts)` commit on `main`)

All four files modified and verified green. circuit + health are now
removal-protected per the inventory-`stable` rule. The firewall scan now
covers admin, health, and nucleus (all verified leak-free). The
API_CONTRACT_INVENTORY.md Freeze Enforcement note makes the coupling
explicit so future lifecycle promotions do not repeat the gap.

No CHANGELOG entry and no semver bump — deliberate, matching the PR #77
precedent for pure governance-tooling improvements with zero user-facing
behaviour change.

### Iteration loop

Full 9-subagent loop. Results:

- **contract-guardian** PASS — verified +28/−0 additive; all entries
  legitimate exported symbols for packages already tagged `stable` in the
  inventory; no DEP/MA warranted; inclusion-rule comment and coupled-change
  inventory note both correct; CLI/config baselines untouched.
- **architect-reviewer** PASS-with-WARN (no blockers) — WARN flags two
  follow-up candidates (see below): the near-identical `packages` slice
  drift risk across the two scanner files, and the absence of any scanner
  coverage for `pkg/observability`. Endorsed the firewall expansion as
  in-bounds.
- **code-reviewer** PASS (NITS applied — comment wording).
- **test-runner** PASS (contracts lane + full `pkg` lane + vet + fmt).
- **security-auditor** PASS (test-tooling only; no runtime surface).
- **examples-maintainer** PASS (no example surface touched).
- **doc-updater** PASS (inventory note is the intended doc change).
- **changelog-writer** NO-entry (deliberate — no user-facing change).
- **governance-checker** PASS (strengthens the SLO no-removal enforcement;
  no SLO/CI-matrix/release-checklist update needed).

### In progress

- (none)

### Blocked

- (none)

## Follow-ups opened by this iteration

1. **Shared package-enumeration helper for contract scanners.** Surfaced
   by architect-reviewer. `contracts/freeze_test.go` and
   `contracts/firewall_test.go` hand-maintain near-identical `packages`
   slices; drift risk is real (the `pkg/observability` omission from the
   constructor-gap iteration is evidence). Extract a single
   `allPublicPackages()` source-of-truth so each scanner applies a filter
   predicate and omissions become machine-visible. Medium effort.

2. **`pkg/observability` inventory entry + firewall scan.** Surfaced by
   architect-reviewer. It is a real public `pkg/*` package with no
   `API_CONTRACT_INVENTORY.md` row and no scanner coverage (currently
   leak-free, imports nothing forbidden). Needs a lifecycle-tag decision
   (e.g. `experimental` or a new `internal-facing` annotation) — an owner
   call before freeze inclusion can be decided.

## Files of interest

- `contracts/freeze_test.go` — `packages` slice extended; inclusion-rule
  and deliberate-omissions comments added.
- `contracts/firewall_test.go` — `packages` slice extended; firewall-vs-
  freeze divergence explained in comment.
- `contracts/baseline/api_exported_symbols.txt` — +28 circuit + health
  exported symbols; 0 removals.
- `docs/reference/API_CONTRACT_INVENTORY.md` — Freeze Enforcement coupled-
  change note.

## Notes / decisions log

- 2026-05-21 — Package-coverage-gap work landed as a combined
  `fix(contracts)` commit on `main`. circuit + health were genuinely
  `stable` per the inventory; this iteration only added the
  removal-protection that was missing — no lifecycle tag was changed.
  Firewall expansion (admin/health/nucleus) endorsed in-bounds; all three
  verified leak-free. Two new follow-up candidates added per
  architect-reviewer findings (#1 shared pkg-enum helper, #2 observability
  inventory entry). Deliberate no-CHANGELOG, no-semver decision matched
  to PR #77 precedent.
