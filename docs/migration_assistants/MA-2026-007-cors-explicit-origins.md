# Migration Assistant: implicit allow-all CORS → explicit `cors_origins`

- ID: `MA-2026-007`
- Pairs with: `docs/deprecations/DEP-2026-007-cors-allowall-default-flip.md`
- Severity: `low` before v1.0.0 (WARN only, no behavior change);
  `medium` at v1.0.0 for deployments with cross-origin browser clients that
  never set `cors_origins` — their CORS requests start failing until the
  key is set.
- Status: `current`

---

## Scope

Applications that rely on the **implicit** allow-all CORS default — i.e.
they serve cross-origin browser clients and have no `cors_origins` key in
their config. At v1.0.0 the empty default flips to deny cross-origin
(DEP-2026-007, honoring ADR-013 R4).

Out of scope: applications that already set `cors_origins` (any value), and
applications with no cross-origin browser clients (the flip removes headers
they never needed; nothing observable changes for same-origin traffic).

## Detection

**Logs — the announcement WARN (v0.11.0+):**

```
cors_origins is empty: the implicit allow-all CORS default is deprecated
and flips to deny cross-origin at v1.0.0 (DEP-2026-007)
```

**Config file — key absent:**

```bash
# From the consumer repo root; no output means the implicit default is in use.
grep -rn "^cors_origins:" *.yml *.yaml 2>/dev/null
```

**Traffic check:** whether browser clients actually call the API from
another origin (check `Origin` request headers in access logs). If none do,
no action is needed.

## Rewrite

One YAML list, chosen by intent:

```yaml
# Real cross-origin clients — the allow-list (recommended):
cors_origins:
  - "https://app.example.com"

# Public credential-less API that genuinely wants allow-all —
# the explicit wildcard (behaves exactly like today's default):
cors_origins:
  - "*"
```

`cors_allow_credentials: true` additionally requires a non-wildcard-safe
setup: it is only honored with an explicit allow-list, and with `["*"]` the
middleware reflects the request origin instead of emitting `*` (SEC-1 /
FW-6 guards, unchanged by this migration).

## Rollback

- Before v1.0.0: removing the key restores the implicit default (plus the
  WARN).
- After v1.0.0: `cors_origins: ["*"]` reproduces the pre-flip allow-all
  behavior exactly.

## Validation

After setting the key, boot the app and confirm:

1. no `cors_origins is empty` WARN in startup logs;
2. a preflight from an allowed origin gets `Access-Control-Allow-Origin`
   (the origin, or `*` for the wildcard config);
3. a request from a non-listed origin gets no CORS headers (when using a
   specific allow-list).
