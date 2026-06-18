# Iteration Archive — Router.With() per-route middleware (finding #24)

> Archived: 2026-06-18.
> Status: COMPLETE (nucleus PR #140 merged; fleetdesk consumer side commit c5d969f).
> Finding #24 closed end-to-end.

## Goal

Add `With(mw ...Middleware) Router` to the stable `nucleus.Router` interface so
callers can attach per-route middleware without a global registration, covering
the fluent router and its `Resource` helper, while keeping the additive
contract non-breaking.

## Nucleus side — PR #140 (`05fb701` merged → main 2026-06-18)

**Commit subject:** `feat(nucleus): Router.With() — per-route middleware on the
fluent router (#24)`

### What was added

- `With(mw ...Middleware) Router` method on the `nucleus.Router` interface
  (`pkg/nucleus/router.go`).
- Implementation on `routerAdapter`: delegates to `router.Mux.With`, which
  returns an inline sub-Mux. The sub-Mux applies the supplied middleware chain
  to every route registered on the returned `Router` only — sibling and parent
  routes are not affected.
- `nucleus.Middleware` and `router.Middleware` are the same
  `func(http.Handler) http.Handler` alias; no new type was introduced.
- `pkg/nucleus/router_with_test.go`: regression suite covering isolation,
  `Resource` propagation, additive composition across nested `With`/`Group`,
  prefix preservation, and — critically — a test that disproved the
  security-auditor's "does `With` bypass `Resource` guards?" concern (it does
  not; the middleware runs for every `Resource`-generated sub-route).
- `contracts/baseline/`: additive diff of exactly one entry:
  `iface-method:Router.With`. `Router` is framework-implemented and
  module-consumed, so adding a method is non-breaking for existing callers.
- `docs/adrs/ADR-010`: per-route-middleware amendment + status updated.
- `docs/reference/API_CONTRACT_INVENTORY.md`: `Router.With` entry added.
- `CHANGELOG.md`: Unreleased entry for the feature.
- `website/docs/concepts/routing.md`: `With` usage documented.

### Iteration loop outcomes

| Step | Verdict | Notes |
|---|---|---|
| architect-reviewer | PASS | Sub-Mux delegation consistent with SPEC §2 layering; no new global state. |
| contract-guardian | PASS | Additive +1 iface-method; non-breaking for consumers; baseline updated. |
| security-auditor | PASS (after regression test) | Initial concern: does `With` skip `Resource` sub-routes? Regression test in `router_with_test.go` proves middleware runs for all `Resource` endpoints. Concern disproved. |
| code-reviewer | NITS addressed | Added impl godoc, shared-ServeMux note, prefix assertion in prefix test, nested `Group`+`With` test. |
| test-runner | green | `-race`, freeze, firewall, `gofmt` all passed. |
| examples-maintainer | no-op | No `examples/mvc_api` surface touched. |
| doc-updater | UPDATED | `API_CONTRACT_INVENTORY`, `ADR-010`, `CHANGELOG`. |
| website-curator | UPDATED | `website/docs/concepts/routing.md`. |

## Fleetdesk consumer side — commit `c5d969f` (local-only, 2026-06-18)

**Commit subject:** `refactor(webui): drop the nopResponseWriter role-guard hack
— close finding #24`

### What changed

- Re-pinned nucleus to `v0.9.1-0.20260618162258-05fb70103082` (commit `05fb701`).
- Deleted the `nopResponseWriter` adapter: the previous workaround intercepted
  `http.ResponseWriter` writes to suppress duplicate headers when a role guard
  fired inside an SSR handler. It was fragile and obscure.
- `requireRole` now checks role membership directly via
  `Enforcer.RequireRole`. This is symmetric with `requirePerm`. Because
  fleetdesk's `rbac_policy.csv` carries no `g` (role-hierarchy) lines, the
  behaviour is equivalent to the old path and the styled 403 is rendered via
  `nucleus.Context` as expected.
- E2E smoke: 12/12 green.
- `FINDINGS.md`: finding #24 marked FIXED.

### Honest nuance — SSR guards do NOT yet use `With`

Fleetdesk's SSR route guards remain `Handler`-based rather than adopting
`With(Enforcer.RequireRole(...))`. Two gaps block the full migration:

1. **Finding #26 (OPEN):** `RequireRole` returns a JSON 403 response. A module
   middleware that fires before the handler cannot reach the template engine to
   render an SSR-appropriate styled denial page.
2. **Module middleware / session injection order:** a module middleware applied
   via `With` runs after session injection has been set up by the framework's
   global gate; however, the styled-denial path still requires the template
   engine context that only the handler layer has today.

`With` is therefore validated by nucleus's own test suite and serves the
JSON-API per-route guard case correctly. Fleetdesk's full adoption of `With`
for SSR guards is deferred until finding #26 is resolved.

## Closed finding

- **#24** — no per-route http-middleware in fluent Router — FIXED end-to-end
  (nucleus PR #140 + fleetdesk commit c5d969f, 2026-06-18).

## Files of interest

- `pkg/nucleus/router.go` (`With` interface + `routerAdapter` impl)
- `pkg/nucleus/router_with_test.go` (full regression suite)
- `contracts/baseline/api_exported_symbols.txt` (+1 `iface-method:Router.With`)
- `docs/adrs/ADR-010` (per-route-middleware amendment)
- `docs/reference/API_CONTRACT_INVENTORY.md`
- `CHANGELOG.md`
- `website/docs/concepts/routing.md`
- `~/GolandProjects/fleetdesk/internal/webui/` (nopResponseWriter removed)
- `~/GolandProjects/fleetdesk/FINDINGS.md` (#24 FIXED)
- `~/GolandProjects/fleetdesk/go.mod` (pinned 05fb701 pseudoversion)
