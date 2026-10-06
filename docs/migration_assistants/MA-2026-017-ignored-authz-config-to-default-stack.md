# Migration Assistant: authorization configuration and module policy rows ignored by a WithoutDefaults() application → WithAuthz(), the default stack, or no keys

- ID: `MA-2026-017`
- Pairs with: `docs/deprecations/DEP-2026-017-ignored-authz-config-without-defaults.md`
- Severity: `low` before v2.0.0 (an ERROR log line; the application behaves
  as before); `high` at v2.0.0 for applications that still carry the
  combination — they stop starting until it is resolved.
- Status: `current`

---

## Scope

Applications built `WithoutDefaults()` without `WithAuthz()` whose
configuration sets `rbac_policy_file`, or sets `metrics_public: false` while
the metrics path is served (the Prometheus exporter linked). Such an
application builds no RBAC enforcer: the policy file has never been read,
and the metrics path answers anyone. From v2.0.0 the combination refuses to
start without `WithAuthz()` (DEP-2026-017).

The same applications when they mount a module whose `Policies` declare a
deny row or leave a route the module serves closed to anonymous callers:
there is no enforcer to load the rows into, so those routes answer anyone
(NU-126).

Out of scope: applications built with the defaults, applications built
`WithoutDefaults()` with `WithAuthz()`, and applications built
`WithoutDefaults()` that set neither key and mount no module whose rows
refuse anything.

## Detection

**Logs — one ERROR line at boot (this release onward):**

```
level=ERROR msg="authz configuration IGNORED: the configuration asks for authorization and this application is built WithoutDefaults() without WithAuthz(), which builds no RBAC enforcer, so none of it is enforced" keys="rbac_policy_file, metrics_public" … deprecation="DEP-2026-017: from v2.0.0 this configuration refuses to start without WithAuthz()"
```

```
level=ERROR msg="module policies DISCARDED: mounted modules declare policy rows and this application is built WithoutDefaults() without WithAuthz(), which builds no RBAC enforcer, so none of them is enforced — …" modules="notes (3 rows, 0 deny)" rows=3 deny_rows=0 unguarded="POST /notes, PUT /notes/{id}, DELETE /notes/{id}" … deprecation="DEP-2026-017: from v2.0.0 this configuration refuses to start without WithAuthz()"
```

**CLI — from the project directory:**

```bash
nucleus doctor --check rbac   # warning: authz configuration IGNORED …
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

- Before v2.0.0: removing `WithAuthz()`, or putting `WithoutDefaults()`
  back, returns the application to the ignored keys plus the log line.
- After v2.0.0: there is no ignored state to return to; the choice is
  `WithAuthz()`, the default stack, or no keys.

## Validation

After the rewrite, boot the application and confirm:

1. no `authz configuration IGNORED` and no `module policies DISCARDED`
   line in the boot log;
2. with `WithAuthz()` or on the default stack, `authz: default-deny with N
   policy rows from <path>` (or, with no file, `… with 0 policy rows`), and
   an unpoliced route answering 403;
3. `nucleus doctor --check rbac` reports the policy file found, not ignored.
