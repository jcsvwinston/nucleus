# ADR-016: Admin API Authentication Enforced at the Router Edge

Reference date: 2026-06-09.
Status: Accepted.
Related: [ADR-004](ADR-004-casbin-default-deny-mount.md) (RBAC default-deny),
[ADR-014](ADR-014-cors-credentials-secure-default.md) (security-default-flip
precedent), [ADR-015](ADR-015-firewall-vn-resolution-and-leak-dispositions.md)
(the F-4 audit that surfaced this).

## Context

The F-4 security audit (ADR-015) flagged the admin panel's route wiring. In
`pkg/admin/panel.go` `mountRoutes`, the ~50 `/api/*` endpoints were registered
flat on the root `*router.Mux`, **outside** the `r.Group` that applied
`authMiddleware`. That group wrapped only the SPA catch-all (`/{path...}`). So:

- When `config.Auth != nil`, the `/api/*` routes had **no router-layer
  authentication**. Their only protection was each handler's own
  `authorizeAction()` call, which re-authenticates via `authenticatedUser` →
  `Auth.Authenticate`. A handler that forgot that call would have been silently
  reachable without authentication — a latent, structural authn-bypass.
- When `config.Auth == nil`, `authorizeAction()` short-circuits to `return nil`,
  so the entire admin API and UI are open. This is the intended local
  development / test posture, but nothing warned an operator who reached it by
  accident.

Mitigating facts confirmed during the fix: every framework entry point that
mounts the panel — `pkg/app` (`app.go`, `Auth: admin.NewDatabaseAdminAuth(...)`)
and `pkg/nucleus` (`config_endpoint.go`) — **always** sets an auth provider, so
`Auth == nil` is not reachable through a normal deployment; and a grep confirmed
that today every `/api/*` handler except `handleLogout` already calls
`authorizeAction()`. The risk is therefore structural/defense-in-depth (LOW),
not an active open door in a framework-wired deployment.

## Decision

1. **Authenticate `/api/*` at the router edge.** When an auth provider is
   configured, `mountRoutes` registers all `/api/*` routes (extracted into a new
   `mountAPIRoutes` method) inside an `r.Group` whose first middleware is
   `authMiddleware`. Authentication is now enforced before any handler runs;
   per-handler `authorizeAction()` continues to perform RBAC **authorization**
   on top. authn at the edge, authz per action.

2. **Preserve the SPA middleware scope.** The SPA fallback keeps its existing
   stack (`tenantContext`, `audit`, `sessionActivity`, `liveTraffic`) by moving
   into a **nested** group that inherits `authMiddleware` from its parent. The
   `/api/*` routes deliberately do **not** inherit those four SPA-observation
   middlewares — preserving their prior router-layer middleware set (which, in
   the authenticated branch, was none) so this change adds *only* authentication
   and does not alter audit/tenant/session/live-traffic behaviour for the API.

3. **Warn, don't fail, when `Auth == nil`.** The no-auth branch keeps serving
   the open API/UI (development/test posture) but logs a prominent `WARN`
   (`warnAdminAuthDisabled`) stating that all `/admin` routes are publicly
   reachable. Production paths always configure auth, so this branch is a
   hand-wired-panel footgun, not a deployment default.

No exported symbol, CLI command, or config key changes.

## Alternatives considered

- **Fail-closed on `Auth == nil`** (deny all, or require an explicit
  `InsecureNoAuth: true` opt-in). Rejected: it breaks the legitimate
  development/test posture and the large body of tests that construct a panel
  without auth, while production is already safe (framework always sets Auth). A
  `WARN` is the proportionate response, consistent with the ADR-014 precedent of
  warning rather than hard-failing a self-neutralising / non-default
  misconfiguration.
- **Move `/api/*` into the existing SPA middleware group** (so they also get
  tenant/audit/session/live-traffic). Rejected for this change: it alters the
  router-layer middleware semantics of every API call (e.g. audit-middleware
  double-logging vs. the handlers' own audit calls, tenant-context injection on
  API paths) — a broad behavioural change out of scope for a focused authn
  hardening. Revisiting whether API routes *should* share those middlewares is a
  separate decision.
- **Per-handler guard lint** (static check that every handler calls
  `authorizeAction`). Rejected as the primary fix: brittle and easy to bypass;
  edge authentication is the structural guarantee. Such a lint could still be
  added as a complementary defense.

## Consequences

- **Defense-in-depth.** A future `/api/*` handler that forgets its
  `authorizeAction()` call is no longer publicly reachable — `authMiddleware`
  rejects unauthenticated requests at the edge (401 for API requests, login
  redirect for browser requests). Pinned by
  `TestAdminAPI_RoutesCarryAuthMiddleware` (structural: every `/api/*` route
  carries router-layer middleware) and `TestAdminAPI_UnauthenticatedRequestRejected`
  (behavioural: sensitive endpoints 401 when unauthenticated).
- **`/api/logout` now sits behind authn.** Previously reachable unauthenticated;
  now requires a valid session. Logout for a logged-in user is unaffected; an
  unauthenticated logout is a no-op 401, which is correct.
- **No change for authenticated callers.** `authMiddleware` sets the user in the
  request context; `authenticatedUser` already prefers that context value, so
  authorized flows behave identically (now authenticated once at the edge
  instead of per-handler). Confirmed by
  `TestAdminAPI_AuthenticatedRequestReachesHandler` and the full admin suite.
- **`Auth == nil` is now loud.** Operators who mount the panel without an auth
  provider get a boot/serve-time `WARN`. The freeze/firewall contracts are
  untouched; no baseline changes.
