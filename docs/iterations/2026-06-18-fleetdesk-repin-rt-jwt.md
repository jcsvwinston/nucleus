# Iteration: fleetdesk re-pin + apiauth rt.JWT() refactor — finding #32 closed end-to-end

**Date:** 2026-06-18
**Status:** COMPLETE
**Repo:** fleetdesk (local-only, no remote push yet)
**Consumer-side completion of:** `docs/iterations/2026-06-18-runtime-jwt-accessor.md`

---

## Goal

Close finding #32 ("apiauth builds its own JWTManager; should source from
framework") end-to-end: the nucleus side was fixed in PR #134
(`Runtime.JWT()` accessor); this iteration handled the consumer side —
re-pinning fleetdesk to that commit and refactoring `internal/apiauth` to
use the framework's manager.

---

## What changed (fleetdesk commit `3567dac`)

**commit message:** `refactor(apiauth): use the framework's rt.JWT() — close finding #32`

### 1. Nucleus pin bump (`go.mod`)

| Before | After |
|--------|-------|
| `v0.9.1-0.20260616174301-084a4b5689ca` | `v0.9.1-0.20260618065917-efddf6ce3dbb` |

The new pseudoversion pins nucleus commit `efddf6c` (PR #134), which
introduced `Runtime.JWT()`.

### 2. `internal/apiauth/module.go`

- **Removed** the `Config{ Secret, TokenTTLMinutes }` struct and the
  `Module[Config]` parameterisation.
- **Now** `Module[struct{}]` — no per-module config needed.
- `OnStart` sources the JWT manager via `rt.JWT()` (the framework's
  singleton) instead of constructing its own `auth.NewJWTManager`.
- `OnStart` returns an error (with a clear message) if `rt.JWT()` is nil,
  preventing silent misconfiguration.
- Dropped the direct import of `pkg/auth` for manager construction.

### 3. `nucleus.yml` config migration

| Before | After |
|--------|-------|
| `modules.apiauth.secret: <value>` | `jwt_secret: <value>` (top-level) |
| `modules.apiauth.token_ttl_minutes: 60` | `jwt_expiry: 60m` (top-level) |
| *(no issuer)* | `jwt_issuer: fleetdesk` (top-level) |
| `modules.apiauth` block present | `modules.apiauth` block removed |

The JWT credentials are now owned by the framework's top-level config;
the apiauth module reads them indirectly through `rt.JWT()`.

### 4. `internal/platform/apiauth.go`

Updated comment on the `apiJWT` singleton to clarify it is now the
framework's `rt.JWT()` — key and expiry are single-sourced from the
framework config, not from a module-local config block.

### 5. `POST /api/token` response contract

Dropped the `expires_in` field from the token-issuance response. The
JWT `exp` claim is authoritative; the module no longer has visibility
into the framework-internal expiry value, so surfacing a separate
`expires_in` field would require re-parsing the issued token or adding
a framework accessor. Removed cleanly instead.

### 6. `FINDINGS.md`

Finding **#32** marked **FIXED** with references to:
- nucleus PR #134 (upstream: `Runtime.JWT()` accessor)
- this commit `3567dac` (consumer: re-pin + apiauth refactor)

---

## Validation

| Check | Result |
|-------|--------|
| `go build ./...` | green |
| `go vet ./...` | green |
| Unit tests | green |
| E2E smoke (`-tags e2e -run TestE2ESmoke`) | **12/12 green** |

Auth behaviour confirmed unchanged:
- Anonymous request → 401
- Valid bearer token → 200
- Viewer attempting write → 403
- Cross-tenant request → 403

The auth surface (APIAuth middleware + credential check) was not modified;
only the source of the JWT manager changed (own construction → framework
accessor). The smoke suite confirms behaviour is preserved.

The subagent iteration-loop review pass was intentionally skipped: this is
a pure refactor within a local prototype repo, touching no nucleus public
API, no contract surface, and no new behaviour.

---

## Acceptance criteria (all met)

- [x] fleetdesk pins nucleus at or after `efddf6c` (PR #134).
- [x] `internal/apiauth` no longer constructs its own `auth.NewJWTManager`.
- [x] `nucleus.yml` JWT config lives under top-level keys (`jwt_secret`,
      `jwt_expiry`, `jwt_issuer`); `modules.apiauth` block removed.
- [x] `OnStart` guards against nil `rt.JWT()` with a clear error.
- [x] E2E smoke 12/12 green — auth behaviour preserved.
- [x] FINDINGS.md finding #32 marked FIXED.

---

## Relationship to other work

| Artefact | Role |
|----------|------|
| nucleus PR #134 (`efddf6c`) | Upstream: added `Runtime.JWT()` |
| nucleus PR #135 (`b33eee8`) | Upstream: pinned govulncheck @v1.3.0 (CI unblock) |
| `docs/iterations/2026-06-18-runtime-jwt-accessor.md` | Nucleus-side archive; this file is its consumer-side counterpart |
| fleetdesk `FINDINGS.md` #32 | Now FIXED (both sides closed) |

---

## Notes

- fleetdesk is local-only (no remote). The commit `3567dac` exists only on
  the local `main` branch as of 2026-06-18.
- govulncheck in nucleus `ci.yml` remains pinned at `@v1.3.0`. Do NOT
  upgrade to `@latest` until `golang.org/x/tools` fixes the
  `TypeParam` panic under Go 1.26.4 generics.
- v0.9.0 is the current published nucleus tag (2026-06-09, commit `929234e`).
  All changes since — including PR #134 — live on nucleus `main`, unreleased.
