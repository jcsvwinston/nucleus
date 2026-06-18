# Iteration: OpenAPI security schemes + requirements — finding #33 closed end-to-end

**Date:** 2026-06-18
**Status:** COMPLETE
**Repos:** nucleus (PR #138, merged main); fleetdesk (commit `8686574`, local-only)
**Finding closed:** #33 — `pkg/openapi` had no way to declare security schemes

---

## Goal

Close finding #33 ("pkg/openapi Document/Components/Operation have no
security-scheme support; bearer auth is undeclarable in a contract")
end-to-end: add the full OpenAPI 3.1 security-scheme surface to nucleus's
experimental `pkg/openapi`, then wire it in the fleetdesk consumer so the
live `/openapi.json` carries a `bearerAuth` scheme declaration and correct
per-operation overrides.

---

## What changed

### Nucleus — PR #138 (`0d3d875`, merged → main 2026-06-18)

**commit message:** `feat(openapi): declare security schemes + requirements (finding #33)`

#### 1. `pkg/openapi/openapi.go`

New types and helpers added (all purely additive; `pkg/openapi` is
experimental — no contract baseline change required):

| Symbol | Kind | Purpose |
|--------|------|---------|
| `SecurityScheme` | struct | OpenAPI 3.1 security scheme object (`type`, `scheme`, `bearerFormat`, `name`, `in`, `flows`, `openIdConnectUrl`) |
| `SecurityRequirement` | type alias | `map[string][]string` — maps scheme name to required scopes |
| `Components.SecuritySchemes` | field | `map[string]*SecurityScheme` on the existing `Components` struct |
| `Document.Security` | field | `[]SecurityRequirement` — document-level global requirement |
| `Operation.Security` | field | `*[]SecurityRequirement` — pointer so an explicit empty slice survives `omitempty` |
| `BearerAuthScheme(name)` | helper func | Returns a pre-filled HTTP bearer/JWT `SecurityScheme` |
| `APIKeyScheme(name, in)` | helper func | Returns a pre-filled API-key `SecurityScheme` |
| `AddSecurityScheme(doc, name, scheme)` | helper func | Adds a scheme to `doc.Components.SecuritySchemes`, initialising the map if nil |
| `Require(names...)` | helper func | Returns a `SecurityRequirement` with empty scope slices for each name |
| `RequireSecurity(op, names...)` | helper func | Sets `Operation.Security` to a single `Require(names...)` |
| `PublicSecurity(op)` | helper func | Sets `Operation.Security` to `&[]SecurityRequirement{}` — emits `"security": []`, explicitly overriding the global requirement |

Key design decision: `Operation.Security` is `*[]SecurityRequirement` (pointer,
not value). A nil pointer means "inherit the global `Document.Security`";
`PublicSecurity()` sets it to a non-nil empty slice, which JSON-encodes as
`"security": []` and signals "no auth required" to OpenAPI tooling. This
distinction is lost with a value type because an empty value and an absent
value are indistinguishable under `omitempty`.

#### 2. `pkg/openapi/openapi_test.go`

Tests added:
- Round-trip marshal/unmarshal of a document with `bearerAuth` scheme +
  global security requirement.
- Per-operation `RequireSecurity` check.
- `PublicSecurity()` check: confirms `"security": []` appears in JSON output.
- Nil-document guard: `AddSecurityScheme` and `PublicSecurity` on a nil
  `Document` return without panic.

#### 3. `docs/reference/API_CONTRACT_INVENTORY.md`

`pkg/openapi` section updated: security-scheme surface listed as
experimental, with a note that the `*[]SecurityRequirement` pointer
convention is the canonical way to express operation-level public overrides.

#### 4. `CHANGELOG.md`

Entry added under `Unreleased`:
```
### Added
- `pkg/openapi`: `SecurityScheme`, `SecurityRequirement`, helpers
  `BearerAuthScheme`, `APIKeyScheme`, `AddSecurityScheme`, `Require`,
  `RequireSecurity`, `PublicSecurity` — OpenAPI 3.1 security-scheme
  support (experimental). Finding #33.
```

#### Iteration loop outcomes

| Step | Agent | Verdict |
|------|-------|---------|
| architect-reviewer | checked SPEC + ADR consistency; purely additive to experimental pkg | PASS |
| contract-guardian | confirmed pkg/openapi is experimental; no baseline change; API_CONTRACT_INVENTORY updated | PASS |
| security-auditor | no auth surface changed; additive types only; no injection risk | PASS |
| code-reviewer | raised: gofmt drift (fixed); missing round-trip test (added); missing nil-doc test (added); godoc missing on helpers (added) | PASS after fixes |
| test-runner | `go test ./pkg/openapi/...` green; `go test ./...` green; CI green | PASS |

---

### Fleetdesk — commit `8686574` (local-only, 2026-06-18)

**commit message:** `feat(contracts): declare bearer auth in the OpenAPI contract — close finding #33`

#### 1. `go.mod` — nucleus pin bump

| Before | After |
|--------|-------|
| `v0.9.1-0.20260618065917-efddf6ce3dbb` (efddf6c) | `v0.9.1-0.20260618152739-0d3d8758ecd3` (0d3d875) |

#### 2. `internal/contracts/openapi.go`

- Called `AddSecurityScheme(doc, "bearerAuth", BearerAuthScheme("bearerAuth"))`
  to register the HTTP bearer/JWT scheme in `doc.Components.SecuritySchemes`.
- Set `Document.Security = []SecurityRequirement{Require("bearerAuth")}` so
  every operation inherits the bearer requirement by default.
- Called `PublicSecurity(&tokenOp)` on `POST /api/token` to emit
  `"security": []`, marking the token-issuance endpoint as publicly
  accessible without auth.

#### 3. `FINDINGS.md`

Finding **#33** marked **FIXED** with references to:
- nucleus PR #138 (`0d3d875`) — upstream implementation
- fleetdesk commit `8686574` — consumer declaration

#### 4. `README.md` (fleetdesk)

OpenAPI feature-matrix row updated: "security schemes" column changed from
"pending" to "done (#33)".

---

## Validation

| Check | Scope | Result |
|-------|-------|--------|
| `go build ./...` | fleetdesk | green |
| `go vet ./...` | fleetdesk | green |
| `go test ./...` | nucleus | green |
| `go test ./...` | fleetdesk | green |
| E2E smoke (`-tags e2e -run TestE2ESmoke`) | fleetdesk | **12/12 green** |
| Live `/openapi.json` — `securitySchemes.bearerAuth` present | fleetdesk | confirmed |
| Live `/openapi.json` — `Document.security: [{bearerAuth: []}]` | fleetdesk | confirmed |
| Live `POST /api/token` — `security: []` in operation | fleetdesk | confirmed |
| nucleus CI | PR #138 | green |

---

## Acceptance criteria (all met)

- [x] `pkg/openapi` exposes `SecurityScheme`, `SecurityRequirement`, and
      operation-level `Security` field.
- [x] `BearerAuthScheme`, `APIKeyScheme`, `AddSecurityScheme`, `Require`,
      `RequireSecurity`, `PublicSecurity` helpers available.
- [x] `Operation.Security` is `*[]SecurityRequirement`; nil = inherit global;
      empty slice = explicit public override; survives `omitempty` correctly.
- [x] All new symbols have godoc.
- [x] Round-trip marshal/unmarshal test passes; nil-doc guard tests pass.
- [x] `pkg/openapi` change is purely additive; zero byte-level change when
      security fields are absent (omitempty).
- [x] `API_CONTRACT_INVENTORY.md` and `CHANGELOG.md` updated on nucleus side.
- [x] fleetdesk `go.mod` pins nucleus at or after `0d3d875`.
- [x] Live `/openapi.json` carries `securitySchemes.bearerAuth` and global
      security requirement.
- [x] `POST /api/token` carries `"security": []` (public override).
- [x] FINDINGS.md finding #33 marked FIXED on both sides.
- [x] nucleus CI green (PR #138).
- [x] fleetdesk E2E smoke 12/12 green.

---

## Relationship to other work

| Artefact | Role |
|----------|------|
| nucleus PR #134 (`efddf6c`) | Prior iteration: `Runtime.JWT()` accessor |
| nucleus PR #135 (`b33eee8`) | CI unblock: govulncheck pinned @v1.3.0 |
| nucleus PR #138 (`0d3d875`) | This iteration: OpenAPI security-scheme surface |
| fleetdesk commit `3567dac` | Prior iteration: apiauth refactor (finding #32 consumer side) |
| fleetdesk commit `8686574` | This iteration: bearer auth declaration (finding #33 consumer side) |
| fleetdesk `FINDINGS.md` #32 | FIXED (prior iteration) |
| fleetdesk `FINDINGS.md` #33 | FIXED (this iteration) |
| `docs/iterations/2026-06-18-runtime-jwt-accessor.md` | Prior nucleus-side iteration |
| `docs/iterations/2026-06-18-fleetdesk-repin-rt-jwt.md` | Prior fleetdesk-side iteration |

---

## Notes

- `pkg/openapi` is experimental. No contract baseline change was made.
  When `pkg/openapi` graduates to stable, `BearerAuthScheme`, `APIKeyScheme`,
  and the `SecurityRequirement` types must be added to the freeze baseline.
- govulncheck in nucleus `ci.yml` remains pinned at `@v1.3.0`. Do NOT
  upgrade to `@latest` until `golang.org/x/tools` fixes the `TypeParam`
  panic under Go 1.26.4 generics.
- v0.9.0 is the current published nucleus tag (2026-06-09, commit `929234e`).
  All changes since — including PR #138 — live on nucleus `main`, unreleased.
- fleetdesk is local-only (no remote). Commit `8686574` exists only on the
  local `main` branch as of 2026-06-18.
- Findings #32 and #33 are now both fully closed (nucleus + fleetdesk sides).
