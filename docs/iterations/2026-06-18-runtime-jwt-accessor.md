# Iteration Archive — Runtime.JWT() accessor + govulncheck CI unblock

> Archived 2026-06-18 by `session-curator`.
> All acceptance-criteria boxes satisfied; iteration COMPLETE.

## Goal

Expose `Runtime.JWT() *auth.JWTManager` as the 5th `nucleus.Runtime` service
accessor (alongside Session / Authorizer / Mailer / Storage) so that framework
modules can mint and verify bearer tokens via the shared, already-configured
`*auth.JWTManager` — eliminating the need for a duplicate manager in consumer
code (fleetdesk finding #32).

## Scope

- in: `pkg/nucleus/runtime.go` (interface + concrete impl), `pkg/nucleus/runtime_test.go`,
  `contracts/baseline/api_exported_symbols.txt`, `docs/adrs/ADR-010.md`,
  `docs/reference/API_CONTRACT_INVENTORY.md`, `CHANGELOG.md`,
  `website/docs/features/auth.md`.
- in: `.github/workflows/ci.yml` — govulncheck pin fix (required to unblock CI
  before the main accessor PR could merge).
- out: implementation changes to `pkg/auth` internals; new `auth.JWTManager`
  methods; `Runtime.DBForTenant`; any other Runtime accessor; fleetdesk re-pin
  (consumer-side follow-up, not in scope for this iteration).

## Acceptance criteria — ALL SATISFIED

- [x] `Runtime.JWT()` returns the framework-configured `*auth.JWTManager`
      when signing material is present (`jwt_secret` / `jwt_keys[]`).
- [x] `Runtime.JWT()` degrades to nil on an unbacked runtime or when no
      signing material is configured (consistent with Session/Authorizer/Mailer/Storage).
- [x] Interface updated: `iface-method:Runtime.JWT` added to
      `contracts/baseline/api_exported_symbols.txt` (additive only).
- [x] `pkg/nucleus/runtime_test.go`: `TestRuntimeJWTExposesAppInstance` passes;
      `TestRuntimeServiceAccessorsUnbackedAreNil` renamed and extended to cover JWT.
- [x] Full iteration loop green: architect WARN→addressed, security WARN→addressed,
      code review nits→addressed, contract-guardian PASS, website-curator UPDATED,
      `-race` + freeze + firewall green.
- [x] CI Required Gate unblocked (govulncheck pinned to v1.3.0).
- [x] ADR-010 amended with 2026-06-18 JWT-accessor entry.
- [x] `API_CONTRACT_INVENTORY.md` and `CHANGELOG.md` updated (semver minor).
- [x] `website/docs/features/auth.md` updated with `rt.JWT()` module-access snippet;
      drift guard + Docusaurus build + `docs-content-verifier` green.

## Final status: COMPLETE (2026-06-18)

Two PRs merged to nucleus `main`:
- **PR #135** (`b33eee8`): `ci: pin govulncheck to v1.3.0` — CI unblock.
- **PR #134** (`efddf6c`): `feat(nucleus): Runtime.JWT() — module access to the
  framework's JWT manager`.

## Done log

**2026-06-18 — PR #135 (`b33eee8`): `ci: pin govulncheck to v1.3.0`**

The CI Required Gate was failing on every PR. Root cause: `govulncheck@latest`
pulled x/vuln v1.4.0 which in turn pulls golang.org/x/tools v0.46.0; that
version's `typesinternal.ForEachElement` panics with
"ForEachElement called on type containing *types.TypeParam" when run against
this generics-heavy module under Go 1.26.4. The scan aborts with a tool crash,
not a vulnerability finding, so it blocked all PRs regardless of diff content.

Resolution: reproduced locally (`@latest` panics, `@v1.3.0` clean), pinned
`.github/workflows/ci.yml` to `govulncheck@v1.3.0` with a TODO comment to
unpin once a newer golang.org/x/tools release fixes the TypeParam panic.
Memory note `ci_security_gates` updated to document the tool-panic failure
mode and the pin.

**2026-06-18 — PR #134 (`efddf6c`): `feat(nucleus): Runtime.JWT()`**

Added `JWT() *auth.JWTManager` to the `nucleus.Runtime` interface and its
`appRuntime` concrete implementation in `pkg/nucleus/runtime.go`. The concrete
implementation retrieves the manager from `app.App.JWT()` when the backing
`*app.App` is non-nil; returns nil otherwise (same degrade-to-nil posture as
all prior accessors).

Review loop outcomes:

- **architect-reviewer WARN → addressed**: The warning noted that completing
  the Runtime service surface would be overclaiming given transitional
  `App.Outbox` and experimental `App.Observability`. The godoc comment was
  qualified: "JWT completes the current stable service accessor surface"
  explicitly notes `App.Outbox` and `App.Observability` are deliberately
  deferred. The status line in ADR-010 was updated accordingly.
- **security-auditor WARN → addressed**: Returning the full `*auth.JWTManager`
  (which includes `RotateKey`/`RemoveKey` mutation methods) was flagged as a
  potential misuse vector from module code. Resolution: documented the mutation
  caveat prominently in the `JWT()` godoc rather than introducing a
  narrowed read-only interface, for consistency with `Authorizer()` (which
  already returns a mutable `*authz.Enforcer`). The decision record is in the
  ADR-010 amendment.
- **code-reviewer nits → addressed**: minor godoc wording and test naming
  improvements applied.
- **contract-guardian PASS**: single additive baseline line
  (`iface-method:Runtime.JWT`); no removals; freeze test green.
- **website-curator UPDATED**: `website/docs/features/auth.md` frontmatter
  updated, `rt.JWT()` module-access code snippet added. Drift guard, Docusaurus
  build, and `docs-content-verifier` all green.
- **test-runner**: `go test ./...` + `-race` green; contract freeze test green;
  firewall (import restriction) green. PR #134 rebased onto PR #135's fixed
  main before merge.

## Files changed (by PR)

**PR #135:**
- `.github/workflows/ci.yml` — govulncheck step pinned `@latest` → `@v1.3.0`.

**PR #134:**
- `pkg/nucleus/runtime.go` — `JWT() *auth.JWTManager` added to `Runtime`
  interface + `appRuntime.JWT()` implementation; godoc with mutation caveat.
- `pkg/nucleus/runtime_test.go` — `TestRuntimeJWTExposesAppInstance` added;
  `TestRuntimeServiceAccessorsUnbackedAreNil` renamed + extended.
- `contracts/baseline/api_exported_symbols.txt` — +1 line:
  `iface-method:Runtime.JWT`.
- `docs/adrs/ADR-010.md` — 2026-06-18 JWT-accessor amendment; status line
  updated to note stable-surface completion with explicit deferred items.
- `docs/reference/API_CONTRACT_INVENTORY.md` — `Runtime.JWT()` row added.
- `CHANGELOG.md` — `Added` entry under `Unreleased`; semver hint: minor.
- `website/docs/features/auth.md` — frontmatter date + `rt.JWT()`
  module-access snippet.

## Follow-ups (not in scope, carried forward)

- **Re-pin fleetdesk** to nucleus @ `efddf6c` and refactor
  `internal/apiauth` to use `rt.JWT()` instead of its own
  `auth.NewJWTManager`. This closes finding #32 on the consumer side and
  validates the new accessor in the real prototype. Mark fleetdesk FINDINGS #32
  FIXED when done.
- **Unpin govulncheck** in `.github/workflows/ci.yml` once a newer
  golang.org/x/tools release fixes the TypeParam panic (upstream issue; track
  x/vuln releases).
- Remaining open friction PRs: #33 (openapi security schemes), #34
  (pre-authz identity hook / reachability-row footgun); plus earlier backlog
  #18, #24, #25, #26, #27 (HIGH), #29, #30.
- Data Studio Phases 0/A/B/C (Phase 0 = ADR for finding #9).

## Findings status (delta from previous iteration)

- FIXED this iteration (nucleus side): #32 (`Runtime.JWT()` — PR #134).
- Consumer-side close pending: re-pin fleetdesk + refactor `internal/apiauth`.
- All other findings unchanged from the 2026-06-17 archive.
