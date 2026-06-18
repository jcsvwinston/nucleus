# Iteration Archive — CSRF session-key fix + fleetdesk adoption (finding #27)

> Archived: 2026-06-18.
> Status: COMPLETE (nucleus PR #142 merged @ a6beffc; fleetdesk consumer side
> commit 7e9666b). Finding #27 closed end-to-end.

## Goal

Make `router.CSRFMiddleware` fully usable from a module-middleware position
(`Module.Middleware`) with any configured `SessionKey`, and surface the resolved
token reliably via `router.CSRFToken(r)` — including on same-origin GET requests
that hit the Layer-1 origin-check shortcut.

## Nucleus side — PR #142 (`a6beffc` merged → main 2026-06-18)

**Commit subject:** `fix(router): CSRFToken honors any session key; token available
on origin shortcut (#27) (#142)`

### What was fixed

- `router.CSRFToken(r)` previously hard-coded the `"csrf_token"` session key
  when resolving the token. A custom `CSRFOptions.SessionKey` made it return `""`.
  It now reads the token from a new `csrfTokenKey` request-context value that the
  middleware itself injects during token resolution — regardless of what key the
  session uses.
- Token resolution + context injection were reordered to run BEFORE the Layer-1
  same-origin (`Sec-Fetch-Site`) shortcut in `CSRFMiddleware`. Previously a
  same-origin GET bypassed token resolution entirely, so a form-rendering handler
  calling `CSRFToken(r)` on a same-origin request received `""` and produced a
  broken form.
- `pkg/router/csrf_session_test.go`: 3 new regression tests:
  1. Session-mode CSRF injected via `Group` middleware — token present in handler.
  2. `CSRFToken` honours a custom `SessionKey` (non-default `"fd_csrf"`).
  3. Token available on the same-origin shortcut path (GET with
     `Sec-Fetch-Site: same-origin`).

### Corrected diagnosis (important)

The original root-cause framing for finding #27 was: "module middleware runs
after session injection / the global auth gate, so the session is not yet in
context when `Module.Middleware` runs." **This was disproved.** `injectDependencies`
wraps the *group* middleware chain as a whole, so the session IS present in context
when `Module.Middleware` executes. The actual bugs were:

1. `CSRFToken` ignored `SessionKey` (always read `"csrf_token"`).
2. Token injection skipped the same-origin shortcut path.

The fix is surgical — no changes to session injection order or dependency wiring.

### Iteration loop outcomes

| Step | Verdict | Notes |
|---|---|---|
| architect-reviewer | PASS | Context-key injection is consistent with stdlib `context` conventions; no global state added. |
| code-reviewer | PASS | Token injection point (before shortcut) is correct; context key is unexported to prevent external coupling. |
| security-auditor | PASS | Injecting a resolved token into context is safe; the CSRF enforcement itself is unchanged. |
| contract-guardian | PASS | `router.CSRFToken` signature unchanged; purely a behaviour fix. No baseline delta. |
| test-runner | green | 3 new regression tests pass; `-race` clean; freeze tests unchanged. |
| examples-maintainer | no-op | No `examples/mvc_api` surface touched. |
| doc-updater | UPDATED | `CHANGELOG.md` Unreleased "Fixed" entry. |

## Fleetdesk consumer side — commit `7e9666b` (local-only, 2026-06-18)

**Commit subject:** `refactor(webui): adopt framework session-mode CSRF — close
finding #27`

**Nucleus re-pin pseudo-version:** `v0.9.1-0.20260618174317-a6beffc15524`

### What changed

- Deleted the hand-rolled CSRF implementation: `platform.EnsureCSRFToken` helper
  and the ad-hoc module `csrf` middleware. These were a workaround for the nucleus
  gap and are now obsolete.
- `internal/webui/csrf.go` — new helper `frameworkCSRF()` that returns a
  pre-configured `router.CSRFMiddleware(CSRFOptions{...})`:
  - `UseSessionToken: true`
  - `EnableOriginCheck: true`
  - `SessionKey: platform.CSRFSessionKey` (`"fd_csrf"`)
  - `FormField: "_csrf_token"`
  - `ExemptPaths: []string{"/assets/"}`
- `platform.CSRFSessionKey = "fd_csrf"` is now exported so csrf.go and any
  future callers share a single constant.
- `platform.SaveUser` rotates the CSRF session key on login (OWASP
  privilege-change rule): the old `"fd_csrf"` value is deleted before the new
  session is written.
- `internal/webui/chrome.go`: form-rendering sources the CSRF token via
  `router.CSRFToken(c.Request)` instead of reading `platform.EnsureCSRFToken`.
- Login-without-CSRF now answers **419** (framework token-mismatch) where the
  hand-rolled guard previously answered 403; the e2e smoke test was updated to
  assert 419.
- `FINDINGS.md`: finding #27 marked FIXED with the corrected diagnosis.
- E2E smoke: **12/12 green.**

## Closed finding

- **#27** — CSRFMiddleware unusable from module-middleware position; CSRFToken
  ignores SessionKey; token absent on same-origin shortcut — FIXED end-to-end
  (nucleus PR #142 + fleetdesk commit 7e9666b, 2026-06-18).

## Findings ledger after this close

- **FIXED: 20** (including #32, #33, #24, #27)
- **OPEN: 13**

## Files of interest

- `pkg/router/csrf.go` (token resolution reordered; `csrfTokenKey` injected)
- `pkg/router/csrf_session_test.go` (3 new regression tests)
- `CHANGELOG.md` (Unreleased "Fixed" entry)
- `~/GolandProjects/fleetdesk/internal/webui/csrf.go` (new `frameworkCSRF()`)
- `~/GolandProjects/fleetdesk/internal/webui/chrome.go` (`router.CSRFToken` call)
- `~/GolandProjects/fleetdesk/pkg/platform/platform.go` (`CSRFSessionKey` export; `SaveUser` rotation)
- `~/GolandProjects/fleetdesk/FINDINGS.md` (#27 FIXED; 20 FIXED / 13 OPEN)
- `~/GolandProjects/fleetdesk/go.mod` (pinned v0.9.1-0.20260618174317-a6beffc15524)
