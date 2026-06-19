# Iteration Archive — Authz pluggable subject/action resolvers (finding #35)

> Archived: 2026-06-19.
> Status: COMPLETE (nucleus PR #146 merged @ 32a01a0; fleetdesk consumer side
> commit 3996e18). Finding #35 closed end-to-end.

## Goal

Let `pkg/authz.MiddlewareWithOptions` accept optional resolver functions so
callers can override how the Casbin query derives the subject and the CRUD
action from an incoming request. This unblocks SSR apps (specifically
fleetdesk) that POST to both update and delete routes but need to enforce
distinct permission checks per route — something that was impossible with the
existing HTTP-method-only action derivation (every POST → `"create"`). Completes
the full SSR authz story started in finding #26 (2026-06-19).

## Prior context

When finding #26 (authz SSR-friendly denial handler) closed on 2026-06-19,
fleetdesk adopted `Router.With(RequireRoleWithOptions(...))` for its 8
role-only SSR routes but explicitly retained a hand-rolled `requirePerm`
(direct `Enforcer.Can` with its own `actionFor` mapping
`POST /delete → "delete"`) for the 5 state-changing ticket/alert routes
because the framework middleware could not be given a custom action mapping.
This iteration resolves that remaining gap.

## Nucleus side — PR #146 (`32a01a0` merged → main 2026-06-19)

**Commit subject:** `feat(authz): pluggable subject/action resolvers on
MiddlewareWithOptions (finding #35) (#146)`

### New stable symbols in `pkg/authz` (additive — 0 removals)

- **`type SubjectResolver func(r *http.Request, claims *auth.Claims) string`** —
  optional override for how the middleware derives the Casbin subject from the
  request. When nil (default), subject = `claims.UserID` (original behaviour
  unchanged). A resolver returning `""` is safely denied under default-deny and
  logs a Warn.
- **`type ActionResolver func(r *http.Request) string`** —
  optional override for how the middleware maps a request to a Casbin CRUD
  action string. When nil (default), the HTTP-method mapping is used
  (e.g. `GET → "read"`, `POST → "create"`, etc.) — original behaviour unchanged.
  A resolver returning `""` is safely denied under default-deny and logs a Warn.
- **`AuthzOptions.ResolveSubject SubjectResolver`** — new field on the existing
  `AuthzOptions` struct; zero value is nil (backward compatible).
- **`AuthzOptions.ResolveAction ActionResolver`** — new field on the existing
  `AuthzOptions` struct; zero value is nil (backward compatible).

### Backward compatibility

`Enforcer.Middleware()`, `Enforcer.RequireRole()`, `RequireRoleWithOptions()`,
and the existing `MiddlewareWithOptions()` call sites with no resolver fields
set are **byte-for-byte unchanged** in behaviour. Both resolvers are nil-checked
before use; nil falls through to the prior default logic. No fail-open: a
resolver returning `""` is treated as an unknown subject/action and denied
immediately with a Warn log. The security-auditor confirmed no fail-open risk.

### Contract baseline delta

+4 additive exported symbols (types `SubjectResolver`, `ActionResolver`;
fields `AuthzOptions.ResolveSubject`, `AuthzOptions.ResolveAction`).
0 removals. `contract-guardian` verdict: ADDITIVE-OK/minor (additive change,
no deprecation path required).

### Iteration loop outcomes

| Step | Verdict | Notes |
|---|---|---|
| architect-reviewer | PASS | Resolver pattern is consistent with the option-bag established by #26; composable, no hidden globals; clean extension point. |
| code-reviewer | NITS-addressed | Minor nits addressed before merge; no open items. |
| security-auditor | PASS | No fail-open: empty-string resolver result is denied; enforcement check is unchanged. Default-deny invariant preserved. Warn logged on empty resolve. |
| contract-guardian | ADDITIVE-OK/minor | +4 additive symbols; 0 removals; freeze baseline updated. |
| test-runner | green | Fast lane: 32 pkgs pass. Contract freeze: 11 pkgs pass. `pkg/authz` `-race`: 37 tests pass. |
| examples-maintainer | no-op | No `examples/mvc_api` surface touched by this change. |
| doc-updater | UPDATED | `CHANGELOG.md` Unreleased "Added" entry; `docs/reference/API_CONTRACT_INVENTORY.md` updated; `docs/guides/AUTH_GUIDE.md` updated with resolver usage examples. |
| website-curator | UPDATED | `website/docs/features/auth.md` updated with `SubjectResolver` and `ActionResolver` examples. |
| docs-content-verifier | PASS | All Go symbols in updated docs verified against contract baseline; all YAML keys verified against CONFIG_KEY_REGISTRY; Go version claim matches go.mod. |
| changelog-writer | Added | `CHANGELOG.md` Unreleased "Added" section entry. |

## Fleetdesk consumer side — commit `3996e18` (local-only, 2026-06-19)

**Commit subject:** `refactor(webui): adopt framework permission guard via resolvers
— close finding #35`

**Nucleus re-pin pseudo-version:** `v0.9.1-0.20260619132308-32a01a002e72`

### What changed

- The hand-rolled `requirePerm` handler (and the now-unused `forbidden` helper)
  were removed. The 5 state-changing ticket/alert routes now mount
  `MiddlewareWithOptions` per-route via `Router.With`:

  ```go
  r.With(m.permGuard(actionFor)).Post(...)
  ```

  where `permGuard` is defined as:

  ```go
  func (m *webUIModule) permGuard(actionFor authz.ActionResolver) func(http.Handler) http.Handler {
      return authz.MiddlewareWithOptions(authz.AuthzOptions{
          OnDeny:         m.denyHTTP,
          ResolveSubject: func(r *http.Request, claims *auth.Claims) string { return claims.Role },
          ResolveAction:  actionFor,
      })
  }
  ```

- `actionFor` — the app-owned action mapping (e.g. `POST /delete → "delete"`,
  `POST /update → "update"`, etc.) — is now passed as `ResolveAction` to the
  framework middleware rather than being called inside `requirePerm`. Enforcement
  (the `Can` call plus the operator delete deny-override) runs entirely within
  the framework middleware.

- The hand-rolled `requirePerm` closure and the `forbidden` helper are removed.
  `roleGuard` (from #26) is retained unchanged for the 8 role-only SSR routes.

- `FINDINGS.md`: finding #35 marked FIXED; ledger updated to 22 FIXED / 12 OPEN.

- E2E smoke: **12/12 green** (including the operator delete deny-override route).

## Completes the full SSR authz story

With finding #26 (SSR-friendly denial handler, 2026-06-19) and finding #35
(subject/action resolvers, this iteration) both closed:

- All 8 SSR role-only routes in fleetdesk use `Router.With(RequireRoleWithOptions(...))`.
- All 5 state-changing SSR ticket/alert routes use `Router.With(MiddlewareWithOptions(...))` with custom `ResolveSubject` and `ResolveAction`.
- No hand-rolled `requireRole` or `requirePerm` remains in fleetdesk's webui module.
- Role guards (`roleGuard`) and permission guards (`permGuard`) both run entirely
  through the framework middleware. The full SSR authz story is complete.

## Closed finding

- **#35** — `Enforcer.Middleware` / `MiddlewareWithOptions` derive the CRUD action
  from the HTTP method only; an SSR app that POSTs to both update and delete routes
  cannot enforce a delete deny-override through the framework middleware — FIXED
  end-to-end (nucleus PR #146 + fleetdesk commit 3996e18, 2026-06-19).

## Findings ledger after this close

- **FIXED: 22** (including #32, #33, #24, #27, #26, #35)
- **OPEN: 12** (net -1 OPEN; no new finding spun off this iteration)

## Files of interest

- `pkg/authz/middleware.go` (new `SubjectResolver`, `ActionResolver` types; new fields `AuthzOptions.ResolveSubject`, `AuthzOptions.ResolveAction`)
- `contracts/baseline/api_exported_symbols.txt` (+4 additive authz symbols)
- `CHANGELOG.md` (Unreleased "Added" entry)
- `docs/reference/API_CONTRACT_INVENTORY.md` (authz surface updated)
- `docs/guides/AUTH_GUIDE.md` (resolver usage documented)
- `website/docs/features/auth.md` (SubjectResolver + ActionResolver examples)
- `~/GolandProjects/fleetdesk/internal/webui/authz.go` (permGuard added; requirePerm removed; roleGuard unchanged)
- `~/GolandProjects/fleetdesk/FINDINGS.md` (#35 FIXED; 22 FIXED / 12 OPEN)
- `~/GolandProjects/fleetdesk/go.mod` (pinned v0.9.1-0.20260619132308-32a01a002e72)
