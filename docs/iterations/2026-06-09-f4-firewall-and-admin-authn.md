# Iteration: F-4 Firewall /vN Resolution + Admin RBAC eft + Admin /api/* AuthN

> Archived: 2026-06-09
> Status: COMPLETE — all three PRs squash-merged to `main`; main is clean.

## Goal

Close audit finding **F-4**: make the dependency firewall (`contracts/firewall_test.go`)
correctly resolve `/vN` (Semantic Import Versioning) imports, then dispose of
every third-party leak the fix reveals on stable surfaces. Also close two
adjacent admin-panel findings (RBAC eft column missing; /api/* routes unauthenticated).

## Scope

- in: firewall resolver fix; wrap `casbin.Enforcer` behind an unexported field
  in `pkg/authz`; bless the 6 structural / escape-hatch exposures (jwt, scs×2,
  redis×2, validator) via a narrow `blessedLeaks` allow-list; ADR-015;
  contract-inventory + CHANGELOG; additive freeze rebaseline; surface `eft`
  column in admin RBAC inspector; enforce `authMiddleware` on all `/api/*`
  routes in `pkg/admin/panel.go`; ADR-016.
- out: wrapping the scs/redis/validator escape hatches (blessed as intentional,
  per maintainer decision — Option A); any `Inner()` casbin escape hatch.

## Acceptance criteria

- [x] Firewall resolves `/vN` paths (last non-`vN` segment + override map for
      `go-redis`→`redis`, `minio-go`→`minio`); `HasSuffix` fallback removed.
- [x] `TestFirewall_NoThirdPartyTypesInStableAPIs` passes and is honest
      (every exposure either wrapped or explicitly blessed with an ADR cite).
- [x] `authz.Enforcer` no longer leaks casbin; freeze baseline diff is
      additions-only (`GetPolicy`/`GetGroupingPolicy`/`GetAllRoles`).
- [x] `go test ./...` green.
- [x] ADR-015 + API_CONTRACT_INVENTORY.md + CHANGELOG updated and verified.
- [x] Iteration-loop reviewers (code/security/contract/test/doc/changelog/gov) clean.
- [x] PR #98 opened, CI green, squash-merged.
- [x] Admin RBAC inspector surfaces `eft` (allow/deny) column; regression test added.
- [x] PR #99 opened, CI green, squash-merged.
- [x] All `/api/*` admin routes mounted behind `authMiddleware`; `warnAdminAuthDisabled`
      WARN emitted when `Auth==nil`; ADR-016 authored.
- [x] PR #100 opened, CI green, squash-merged.

## Status

### Done

All three PRs merged.

**PR #98 (commit 0b5d144)** — `fix(contracts): resolve firewall blindness to /vN imports + wrap casbin (F-4, ADR-015)`
- Fixed `contracts/firewall_test.go` `/vN` (Semantic Import Versioning) resolver
  blindness; the prior `HasSuffix("/vN")` fallback could silently drop real
  module segments.
- Wrapped `casbin.Enforcer` behind an unexported field in `pkg/authz/enforcer.go`;
  added 3 Casbin-free forwarder methods (`GetPolicy`, `GetGroupingPolicy`,
  `GetAllRoles`) so `pkg/admin` retains access without importing Casbin directly.
- Freeze baseline diff: +3 additive lines, zero removals — no contract breakage.
- Blessed 6 structural/escape-hatch third-party exposures (jwt, scs×2, redis×2,
  validator) via a narrow `blessedLeaks` allow-list with ADR-015 citations.
- **Discovery**: resolver fix revealed 7 leaks, not the 2 (casbin/jwt) the
  pre-fix audit had verified. The extra 5 were all intentional escape hatches,
  confirmed by the maintainer and blessed accordingly.
- ADR-015 authored at `docs/adrs/ADR-015-firewall-vn-resolution-and-leak-dispositions.md`.
- API_CONTRACT_INVENTORY.md + CHANGELOG + website auth page updated.

**PR #99 (commit 898f074)** — `fix(admin): surface eft (allow/deny) in the RBAC policy inspector`
- `pkg/admin/rbac.go` `handleListRBACPolicies` now includes the `eft` column
  in policy rows returned to the UI.
- Regression test added covering both `allow` and `deny` policy entries.

**PR #100 (commit f39ff75)** — `fix(admin): enforce authentication at the router edge for /api/* (ADR-016)`
- Restructured `pkg/admin/panel.go` `mountRoutes`: extracted `mountAPIRoutes`
  helper; all `/api/*` sub-routes now mount inside a nested group that inherits
  `authMiddleware`, closing the unauthenticated-API-access gap.
- Added `warnAdminAuthDisabled` which emits a `WARN`-level slog line when
  the admin panel is started with `Auth==nil`.
- Structural tests (route-group shape) + behavioural tests (authn enforcement
  on protected endpoints) added.
- ADR-016 authored at `docs/adrs/ADR-016-admin-api-auth-enforcement.md`.

### In progress
- (none)

### Blocked
- (none)

## Files of interest

- `contracts/firewall_test.go` (resolver fix + `blessedLeaks` allow-list)
- `pkg/authz/enforcer.go` (casbin wrap + 3 forwarder methods)
- `contracts/baseline/api_exported_symbols.txt` (+3 lines: `GetPolicy`, `GetGroupingPolicy`, `GetAllRoles`)
- `docs/adrs/ADR-015-firewall-vn-resolution-and-leak-dispositions.md`
- `docs/adrs/ADR-016-admin-api-auth-enforcement.md`
- `pkg/admin/rbac.go` (eft column surfaced in `handleListRBACPolicies`)
- `pkg/admin/panel.go` (`mountAPIRoutes`, `warnAdminAuthDisabled`)
- `docs/reference/API_CONTRACT_INVENTORY.md` (blessed exceptions recorded)
- `CHANGELOG.md` (Unreleased entries for all three PRs)

## Notes / decisions log

- 2026-06-08 — Maintainer chose **Option A**: wrap casbin (accidental embed),
  bless the other 5 revealed leaks as intentional integration escape hatches.
  Option B (wrap all six) was available but deferred — the escape hatches are
  load-bearing consumer API.
- 2026-06-08 — F-4 understated in the prior audit: resolver fix revealed **7**
  leaks across `pkg/auth`, `pkg/authz`, `pkg/validate`, not the 2 (casbin/jwt)
  the pre-fix audit had counted. The extra 5 are intentional escape hatches and
  are now formally blessed in `blessedLeaks`.
- 2026-06-08 — Branch `fix/f4-firewall-vn-resolution` used for PR #98.
- 2026-06-09 — PRs #99 and #100 completed the F-4 adjacent admin findings.
- 2026-06-09 — Open follow-up (LOW, non-blocking): normalize admin 401 body so
  it does not leak raw `err.Error()` strings (`pkg/admin/handlers.go`
  `authErrorToDomain`). Tracked as `task_19c389c9`.
