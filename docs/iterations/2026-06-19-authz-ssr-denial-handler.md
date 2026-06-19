# Iteration Archive — Authz SSR-friendly denial handler (finding #26)

> Archived: 2026-06-19.
> Status: COMPLETE (nucleus PR #144 merged @ e33d8ae; fleetdesk consumer side
> commit e3923b7). Finding #26 closed end-to-end.

## Goal

Give `pkg/authz` middlewares an opt-in denial handler so that SSR apps can render
a styled page or issue a redirect on a role or permission denial, instead of always
receiving the hardcoded JSON 401/403 envelope. This unblocks the per-route guard
migration deferred in the `Router.With` iteration (#24 close, 2026-06-18), where
fleetdesk's 8 SSR role-only routes could not yet adopt `Router.With(RequireRole(...))`
because `RequireRole` could only write JSON.

## Prior deferral context

When finding #24 (Router.With) closed on 2026-06-18, fleetdesk adopted `With` for
its API module but explicitly deferred the SSR guard migration with the note:
"SSR guard adoption of With deferred until finding #26 resolved." This iteration
completes that deferred work.

## Nucleus side — PR #144 (`e33d8ae` merged → main 2026-06-19)

**Commit subject:** `feat(authz): SSR-friendly denial handler on RequireRole/Middleware
(finding #26) (#144)`

### New stable symbols in `pkg/authz` (additive — 0 removals)

- **`type Denial struct`** — carries the context of a denial event:
  - `Status int` — the HTTP status code the middleware would have written (401 or 403).
  - `Authenticated bool` — true if a subject was present (403 case) vs absent (401 case).
  - `Reason string` — human-readable denial reason for logging / template rendering.
- **`type DenialHandler func(w http.ResponseWriter, r *http.Request, d Denial)`** —
  the callback type callers supply to customise denial behaviour.
- **`type AuthzOptions struct`** — option bag for the new `*WithOptions` variants:
  - `OnDeny DenialHandler` — when non-nil, called instead of the default JSON write.
    When nil the default JSON 401/403 response is produced byte-for-byte unchanged.
- **`func (e *Enforcer) MiddlewareWithOptions(opts AuthzOptions) func(http.Handler) http.Handler`** —
  new variant of `Enforcer.Middleware` that accepts `AuthzOptions`.
- **`func (e *Enforcer) RequireRoleWithOptions(opts AuthzOptions, roles ...string) func(http.Handler) http.Handler`** —
  new variant of `Enforcer.RequireRole` that accepts `AuthzOptions`.

### Backward compatibility

`Enforcer.Middleware()` and `Enforcer.RequireRole()` are unchanged in signature and
continue to delegate internally to the `WithOptions` variants with a zero `AuthzOptions`,
producing identical JSON 401/403 behaviour. There is no fail-open path: when `OnDeny`
is non-nil it is called INSTEAD of the JSON write, so the default-deny invariant is
preserved regardless. The security-auditor confirmed no fail-open risk.

### Contract baseline delta

+9 additive exported symbols (types `Denial`, `DenialHandler`, `AuthzOptions`; methods
`(*Enforcer).MiddlewareWithOptions`, `(*Enforcer).RequireRoleWithOptions`; fields
`Denial.Status`, `Denial.Authenticated`, `Denial.Reason`, `AuthzOptions.OnDeny`).
0 removals. `contract-guardian` verdict: ADDITIVE-OK (minor additive change, no
deprecation path required).

### Iteration loop outcomes

| Step | Verdict | Notes |
|---|---|---|
| architect-reviewer | PASS | Option-bag pattern is consistent with existing authz surface; no hidden globals; extension point is clean. |
| code-reviewer | NITS-addressed | Minor nits addressed before merge; no open items. |
| security-auditor | PASS | No fail-open: OnDeny replaces the write, not the enforcement check. Default JSON path byte-for-byte unchanged. |
| contract-guardian | ADDITIVE-OK/minor | +9 additive symbols; 0 removals; freeze baseline updated. |
| test-runner | green | Fast lane: 33 pkgs pass. Contract freeze: 11 pkgs pass. `pkg/authz` `-race`: 32 tests pass. |
| examples-maintainer | no-op | No `examples/mvc_api` surface touched by this change. |
| doc-updater | UPDATED | `CHANGELOG.md` Unreleased "Added" entry; `docs/reference/API_CONTRACT_INVENTORY.md` updated; `docs/guides/AUTH_GUIDE.md` updated with `WithOptions` usage. |
| website-curator | UPDATED | `website/docs/features/auth.md` updated with `DenialHandler` and `AuthzOptions` examples. |
| docs-content-verifier | PASS | All Go symbols in updated docs verified against contract baseline; all YAML keys verified against CONFIG_KEY_REGISTRY; Go version claim matches go.mod. |
| changelog-writer | Added | `CHANGELOG.md` Unreleased "Added" section entry. |

## Fleetdesk consumer side — commit `e3923b7` (local-only, 2026-06-19)

**Commit subject:** `refactor(webui): adopt framework RequireRole via Router.With —
close finding #26`

**Nucleus re-pin pseudo-version:** `v0.9.1-0.20260619093054-e33d8ae9f9b2`

### What changed

- The 8 role-only SSR routes that previously used a hand-rolled `requireRole` direct
  check now mount the framework guard per-route via `Router.With`:

  ```go
  r.With(m.roleGuard("admin")).Get(...)
  ```

  where `roleGuard` is defined as:

  ```go
  func (m *webUIModule) roleGuard(roles ...string) func(http.Handler) http.Handler {
      return authz.RequireRoleWithOptions(AuthzOptions{OnDeny: m.denyHTTP}, roles...)
  }
  ```

- `denyHTTP` — new denial handler on `webUIModule` that:
  - Redirects anonymous visitors (401 case, `Denial.Authenticated == false`) to
    the login page.
  - Renders the styled `forbidden.html` template (403 case, `Denial.Authenticated == true`)
    for a wrong-role authenticated member.
  - Rebuilds a `router.Context` from `(w, r)` via `router.NewContext` so template
    rendering has access to the standard request context.

- `internal/webui/chrome.go` — gained `chromeForRequest(r)` helper that constructs
  a Chrome render context from a bare `*http.Request`, used by `denyHTTP`.

- Hand-rolled `requireRole` direct-check closure removed from the 8 affected routes.
  `requirePerm` (direct `Enforcer.Can` with its own `actionFor` mapping) is RETAINED
  for state-changing ticket/alert routes — see finding #35 note below.

- `FINDINGS.md`: finding #26 marked FIXED; ledger updated to 21 FIXED / 13 OPEN
  (net +1 FIXED, net OPEN unchanged due to #35 spin-off logged simultaneously).

- E2E smoke: **12/12 green.**

## Completed deferral

The SSR per-route guard migration that was explicitly deferred when `Router.With`
landed (finding #24, 2026-06-18) is now complete. All 8 SSR role-only routes in
fleetdesk use `Router.With(RequireRoleWithOptions(...))`.

## Closed finding

- **#26** — `RequireRole` / `Enforcer.Middleware` write hardcoded JSON 401/403;
  SSR apps cannot render a page or redirect on denial — FIXED end-to-end
  (nucleus PR #144 + fleetdesk commit e3923b7, 2026-06-19).

## Spun-off finding

- **#35 (OPEN)** — `Enforcer.Middleware` / `MiddlewareWithOptions` derive the CRUD
  action from the HTTP method only (every POST → `"create"`). An SSR app that POSTs
  to both update and delete routes cannot enforce a delete deny-override through the
  framework middleware. fleetdesk therefore keeps its hand-rolled `requirePerm`
  (direct `Enforcer.Can` with its own `actionFor` mapping `POST /delete → "delete"`)
  for state-changing ticket/alert routes. Candidate fix: let the authz middleware
  accept a path/method→action resolver function. This is a natural follow-on to
  #26 and completes the full SSR authz story.

## Findings ledger after this close

- **FIXED: 21** (including #32, #33, #24, #27, #26)
- **OPEN: 13** (including new #35; net unchanged because #26 closed and #35 opened)

## Files of interest

- `pkg/authz/middleware.go` (new `MiddlewareWithOptions`, `RequireRoleWithOptions`; types `Denial`, `DenialHandler`, `AuthzOptions`)
- `contracts/baseline/api_exported_symbols.txt` (+9 additive authz symbols)
- `CHANGELOG.md` (Unreleased "Added" entry)
- `docs/reference/API_CONTRACT_INVENTORY.md` (authz surface updated)
- `docs/guides/AUTH_GUIDE.md` (WithOptions usage documented)
- `website/docs/features/auth.md` (DenialHandler + AuthzOptions examples)
- `~/GolandProjects/fleetdesk/internal/webui/authz.go` (roleGuard, denyHTTP)
- `~/GolandProjects/fleetdesk/internal/webui/chrome.go` (chromeForRequest)
- `~/GolandProjects/fleetdesk/FINDINGS.md` (#26 FIXED, #35 OPEN; 21 FIXED / 13 OPEN)
- `~/GolandProjects/fleetdesk/go.mod` (pinned v0.9.1-0.20260619093054-e33d8ae9f9b2)
