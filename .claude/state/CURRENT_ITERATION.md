# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

Close audit finding **F-4**: make the dependency firewall (`contracts/firewall_test.go`)
correctly resolve `/vN` (Semantic Import Versioning) imports, then dispose of
every third-party leak the fix reveals on stable surfaces.

## Scope

- in: firewall resolver fix; wrap `casbin.Enforcer` behind an unexported field
  in `pkg/authz`; bless the 6 structural / escape-hatch exposures (jwt, scs×2,
  redis×2, validator) via a narrow `blessedLeaks` allow-list; ADR-015;
  contract-inventory + CHANGELOG; additive freeze rebaseline.
- out: wrapping the scs/redis/validator escape hatches (blessed as intentional,
  per maintainer decision — Option A); any `Inner()` casbin escape hatch.

## Acceptance criteria

- [x] Firewall resolves `/vN` paths (last non-`vN` segment + override map for
      `go-redis`→`redis`, `minio-go`→`minio`); `HasSuffix` fallback removed.
- [x] `TestFirewall_NoThirdPartyTypesInStableAPIs` passes **and** is honest
      (every exposure either wrapped or explicitly blessed with an ADR cite).
- [x] `authz.Enforcer` no longer leaks casbin; freeze baseline diff is
      additions-only (`GetPolicy`/`GetGroupingPolicy`/`GetAllRoles`).
- [x] `go test ./...` green.
- [ ] ADR-015 + API_CONTRACT_INVENTORY.md + CHANGELOG updated and verified.
- [ ] Iteration-loop reviewers (code/security/contract/test/doc/changelog/gov) clean.
- [ ] PR opened, CI green, squash-merged.

## Status

### Done
- Resolver fix + `blessedLeaks` allow-list (6 entries) in `contracts/firewall_test.go`.
- Wrapped `authz.Enforcer`; 3 clean forwarder methods added for `pkg/admin`.
- Freeze rebaselined (+3 additive method lines, zero removals).
- ADR-015 authored.
- Full suite green; firewall + freeze green.

### In progress
- Inventory + CHANGELOG updates; iteration-loop review fan-out.

### Blocked
- (none)

## Files of interest

- contracts/firewall_test.go (resolver fix + blessedLeaks)
- pkg/authz/enforcer.go (casbin wrap + forwarders)
- contracts/baseline/api_exported_symbols.txt (+3 lines)
- docs/adrs/ADR-015-firewall-vn-resolution-and-leak-dispositions.md
- docs/reference/API_CONTRACT_INVENTORY.md (pending: record blessed exceptions)
- CHANGELOG.md (pending: Unreleased note)

## Notes / decisions log

- 2026-06-08 — Maintainer chose **Option A**: wrap casbin (accidental embed),
  bless the other 5 revealed leaks as intentional integration escape hatches.
- 2026-06-08 — F-4 understated in the audit: resolver fix revealed **7** leaks
  across pkg/auth, pkg/authz, pkg/validate, not the 2 (casbin/jwt) verified.
- 2026-06-08 — Branch `fix/f4-firewall-vn-resolution`. `main` is PR-only.
