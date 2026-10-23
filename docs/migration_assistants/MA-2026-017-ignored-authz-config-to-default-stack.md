# Migration Assistant: authorization configuration and module policy rows ignored by a WithoutDefaults() application → WithAuthz(), the default stack, or no keys

- ID: `MA-2026-017`
- Pairs with: `docs/deprecations/DEP-2026-017-ignored-authz-config-without-defaults.md`
- Severity: `high` — an application that carries the combination does not
  start until it is resolved (before v2.0.0 it started, with an ERROR log
  line).
- Status: `current`

---

## Scope

Applications built `WithoutDefaults()` without `WithAuthz()` whose
configuration sets `rbac_policy_file`, or sets `metrics_public: false` while
the metrics path is served (the Prometheus exporter linked). Such an
application builds no RBAC enforcer: the policy file would never be read,
and the metrics path would answer anyone. The combination refuses to start
without `WithAuthz()` (DEP-2026-017).

The same applications when they mount a module whose `Policies` declare a
deny row or leave a route the module serves closed to anonymous callers:
there is no enforcer to load the rows into, so those routes would answer
anyone, and the boot fails once the modules have registered their routes
(NU-126). The `api` starter does not carry `WithAuthz()` — its contract is
"no authz" — so `nucleus generate module` writes its rows commented out
there, with the option as the hint, and the pair boots (decision D9 of the
2.0 dry run).

Out of scope: applications built with the defaults, applications built
`WithoutDefaults()` with `WithAuthz()`, and applications built
`WithoutDefaults()` that set neither key and mount no module whose rows
refuse anything.

## Detection

**Boot — the application refuses to start, and the error says why:**

```
app.New: New authz: authz configuration IGNORED: the configuration asks for authorization and this application is built WithoutDefaults() without WithAuthz(), which builds no RBAC enforcer, so none of it is enforced (keys=rbac_policy_file, metrics_public; …) — add WithAuthz() beside WithoutDefaults() … (DEP-2026-017: refused since v2.0.0 without WithAuthz())
```

```
nucleus: module policies DISCARDED: mounted modules declare policy rows and this application is built WithoutDefaults() without WithAuthz(), which builds no RBAC enforcer, so none of them is enforced — … (modules="notes (3 rows, 0 deny)", rows=3, deny_rows=0, unguarded="POST /notes, PUT /notes/{id}, DELETE /notes/{id}") — add WithAuthz() beside WithoutDefaults() … (DEP-2026-017: refused since v2.0.0 without WithAuthz())
```

**CLI — from the project directory:**

```bash
nucleus doctor --check rbac   # error: authz configuration IGNORED …
```

**Source:**

```bash
grep -nE "WithoutDefaults\(\)|WithAuthz\(\)" main.go cmd/*/main.go 2>/dev/null
grep -nE "rbac_policy_file|metrics_public" nucleus.yml 2>/dev/null
```

## Rewrite

| Before | After | Kind |
|---|---|---|
| `WithoutDefaults()` + `rbac_policy_file` | `WithoutDefaults().WithAuthz()` — the default-deny enforcer loads the policy; or no `WithoutDefaults()` | manual |
| `WithoutDefaults()` + `rbac_policy_file`, authorization done in handlers | `rbac_policy_file` removed | manual |
| `WithoutDefaults()` + `metrics_public: false` | `WithoutDefaults().WithAuthz()` — or the key removed and the metrics path private at the network layer | manual |
| `WithoutDefaults()` + a module of your own with `Policies` | `WithoutDefaults().WithAuthz()` — or the rows removed and the callers refused in the module's middleware or handlers | manual |
| `WithoutDefaults()` + a third-party module with `Policies` | `WithoutDefaults().WithAuthz()` — or no `WithoutDefaults()` | manual |

Chosen by intent:

```go
// The application should authorize every route against its policy file and
// the rows its modules declare, and stay core-only: the default stack's
// authorization (default-deny, ADR-004) and nothing else of that stack.
nucleus.New().
    FromConfigFile("nucleus.yml").
    WithoutDefaults().
    WithAuthz().
    Start()
```

```go
// The same, with the rest of the default stack: no WithoutDefaults().
nucleus.New().
    FromConfigFile("nucleus.yml").
    Start()
```

```yaml
# The application authorizes in its handlers (or serves only public routes):
# remove what asks for an enforcer it does not build.
rbac_policy_file: rbac_policy.csv   # ← delete
metrics_public: false               # ← delete; firewall metrics_path instead
```

With `WithAuthz()`, as on the default stack, every route then answers only
to the callers a policy grants — a route nobody granted answers 403, and
with no policy file and no module rows that is every registered route but
the bootstrap allow-list (`/healthz`, `/livez`, `/readyz`, `/login`,
`/.well-known/jwks.json`, `/static/*`, `/metrics` unless
`metrics_public: false`). Review the policy file against the routes the
application serves before you deploy it. `WithAuthz()` also decodes the
bearer ahead of the API-key read, the rate limiter and the interceptors, so
`WithRateLimit()` keys a token's requests by its user from then on.

Moving to the default stack instead changes more than authorization: it
also builds the mail sender (`noop` unless a driver is declared), the local
storage default and the rate limiter. Keep `.WithStorage()` / `.WithMail()`
/ `.WithRateLimit()` / `.WithAuthz()` out of the chain there: on the default
stack they change nothing.

## Rollback

There is no ignored state to return to: the choice is `WithAuthz()`, the
default stack, or no keys and no refusing rows (before v2.0.0, removing
`WithAuthz()` returned the application to the ignored keys plus the log
line).

## Validation

After the rewrite, boot the application and confirm:

1. the application starts — no `authz configuration IGNORED` and no
   `module policies DISCARDED` refusal;
2. with `WithAuthz()` or on the default stack, `authz: default-deny with N
   policy rows from <path>` (or, with no file, `… with 0 policy rows`), and
   an unpoliced route answering 403;
3. `nucleus doctor --check rbac` reports the policy file found, not ignored.
