# ADR-033: A path no route serves answers 404; the gates run after routing

- Status: Accepted
- Date: 2026-09-06
- Amends: [ADR-004](ADR-004-casbin-default-deny-mount.md) (the default-deny
  mount) and, for the CSRF gate, [ADR-006](ADR-006-csrf-hardening.md)
- Related: [ADR-022](ADR-022-vertical-slice-modules.md) (modules carry their
  own policy rows), Track E (`contracts/baseline/security_posture.txt`, the
  observed hardening profile)

## Context

ADR-004 mounts the default-deny authorizer with `Router.Use`, and the router
applies a `Use` middleware around the whole `ServeMux` — before the mux has
looked at the path. The CSRF gate sits in the same pre-routing chain. The
observable result on a fresh `nucleus new` project, measured on 2026-09-05:

- `GET /nope` answers **403** ("you do not have permission"), with an
  `authz denied` log line offering the policy row that would allow it;
- `POST /nope` answers **419** ("CSRF token missing") before the authorizer
  even runs;
- `GET /notes` — a path the scaffold's `rbac_policy.csv` grants but nothing
  serves — answers **404**, because the grant lets the request through to
  the mux.

So the status of an unknown path depended on the policy file, not on the
route table: the same missing route answered 403, 419 or 404 according to
rows that had nothing to do with it. For the person following the
quickstart the three are one wall — the "403 → 404 → 419 ladder" the release
notes of 1.22 already named for a freshly generated resource — and the hint
in the log ("add to rbac_policy.csv: `p, anonymous, /nope, read, allow`")
sent them to edit a policy for a route that did not exist. The umbrella
quickstart went as far as documenting "`/nope` answers 403 and that is
intentional".

The pre-routing 403 had one argument in its favour: it hides whether a route
exists. That argument is weak for this framework. The route table of a
Nucleus binary is not a secret — every module mounts its routes in the open,
the development log prints them one per line, and the scaffold ships its
policy file next to `main.go`. Hiding route existence behind a uniform 403
protected nothing that the binary itself does not state, and it cost the
first-hour reader the one status code that says "you mistyped the URL".

## Decision

1. **The router resolves the route before its middleware chain runs.**
   `Mux.ServeHTTP` asks the `ServeMux` for the handler first and records
   whether a registered pattern serves the request in the request context.
   `router.Matched(r)` reads that decision. The decision sees through
   mounted sub-routers: a module `Prefix`, a nested `Group` and a
   `Resource` all mount a sub-router with `Route`, and at the parent — where
   the gates sit — the mount prefix matching is not a route. The lookup
   continues in the sub-router against the path with the prefix stripped,
   any depth down, until a leaf pattern or a miss, so `GET /api/typo` under
   a module mounted at `/api` is a miss at the root exactly as `GET /typo`
   is. A handler mounted with `Mount` that is not a router (a file server,
   a third-party mux) is opaque: the framework cannot see its routes, and
   everything under its prefix counts as matched. A method-only mismatch
   (the path is registered for other methods) is a miss, and the mux
   answers 405 with its `Allow` header as `net/http` does.
   Outside a `Mux` — a middleware wrapped around a plain handler, or a test
   calling it directly — there is no routing decision, and `Matched`
   reports true so every security layer keeps enforcing.

2. **The default-deny authorizer and the CSRF gate pass an unmatched request
   through**, so the mux's own 404 (or 405) answers. Everything they decide
   for a registered route is unchanged: a route the policy does not grant
   still answers 403, a state-changing request to a registered route without
   a token still answers 419, and a policy row for a path nothing serves does
   not turn the miss into anything but a 404.

3. **What still runs before routing, deliberately:** the request ID, CORS,
   the client-IP resolution, telemetry, the rate limiter, the request
   logger, the security headers, the bearer decode, the session, and the
   request interceptors. An unknown path cannot bypass the limiter or the
   audit interceptor; only the two gates whose answer is meaningless without
   a route step aside.

4. **The posture baseline records it.** `security_posture.txt` gains an
   observed `[unknown-route]` section per environment — a registered route
   with no policy row (403) next to an unregistered path for a safe and a
   state-changing method (404, 404) — so a return to the pre-routing gate,
   or a loosening of the gate on registered routes, is a diff a reviewer has
   to regenerate on purpose.

5. **A clean scaffold boots with zero WARN lines.** The only remaining WARN
   of a fresh project — `jwt: no signing material configured` — is the
   normal state of a new development project that issues no tokens. It is
   logged at INFO outside production and stays a WARN in production, where
   an unset secret is a deployment gap. This is the second half of the
   "quickstart boots clean" item the suite's maturity audit tracked; the
   Prometheus line had already moved to INFO.

This ships as a `fix` in a minor. No signature changes, no configuration
changes; one exported function is added (`router.Matched`). A deployment
that relied on unknown paths answering 403 — a scanner-facing property, not
a contract any documentation promised — now sees 404, which is what the same
deployment already answered for any path its policy file happened to grant.

## Consequences

- The first-hour reader gets 404 for a typo and 403 for a missing policy
  row, and the `authz denied` hint now only ever names a route that exists.
- Route existence is observable from the status code. Accepted: it was
  already observable from the log, the policy file and the binary, and the
  response for a *registered* route without a grant remains a 403 that says
  nothing about what the handler does.
- The route lookup happens twice per request (once to decide, once when the
  mux dispatches; under a mount, once per level for the decision). A
  `ServeMux` lookup is a tree walk over the pattern set; the cost is not
  measurable next to the rest of the default chain.
- The one place a gate at the root still cannot tell a typo from a route is
  under a plain `http.Handler` mounted with `Mount`: it answers 403/419 by
  policy for every path under that prefix, as before.
- Middleware authors who mount their own gates with `Router.Use` can adopt
  the same behaviour with one call to `router.Matched` — and can ignore it
  and keep enforcing pre-routing, which is what an unchanged middleware
  does.
- The CSRF gate no longer issues its cookie on a 404 response. A form is
  always served by a registered route, so nothing that renders a token is
  affected.
