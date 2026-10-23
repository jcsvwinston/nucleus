# Deprecation Notices

Store active and historical deprecation notices here.

Naming:

- `DEP-YYYY-NNN-<slug>.md`

Policy:

- `docs/governance/DEPRECATION_TEMPLATE.md`

## Register

One row per notice, in the order they were filed. `Status` is the header
line of the notice; `Since` is what that line records — the release that
closed it, or the branch and date the 2.0 dry run applied it on.

| Notice | Status | Since | Title |
|---|---|---|---|
| [`DEP-2026-001`](DEP-2026-001-legacy-plugin-prefixes.md) | `removed` | — | Legacy plugin discovery prefixes (`goframe-plugin-*`, `goframe-mail-*`) |
| [`DEP-2026-002`](DEP-2026-002-builtin-sendgrid-provider.md) | `removed` | — | Built-in `sendgrid` mail provider |
| [`DEP-2026-003`](DEP-2026-003-casbin-policy-csv-3col-to-4col.md) | `removed` | applied on `dry-run/2.0`, 2026-10-23; the `v2.0.0` tag publishes it | 3-column Casbin policy CSV files |
| [`DEP-2026-004`](DEP-2026-004-admin-rbac-policy-file-rename.md) | `removed` | v0.12.0, 2026-07-09 | `admin_rbac_policy_file` config key renamed to `rbac_policy_file` |
| [`DEP-2026-005`](DEP-2026-005-legacy-flat-storage-keys.md) | `removed` | v0.12.0, 2026-07-09 | legacy flat `storage_driver`/`storage_path` config keys |
| [`DEP-2026-006`](DEP-2026-006-cookiesessionstore-removal.md) | `removed` | v0.12.0, 2026-07-09 | `auth.CookieSessionStore` scheduled for removal |
| [`DEP-2026-007`](DEP-2026-007-cors-allowall-default-flip.md) | `completed` | default flipped at v1.0.0, 2026-07-09 | implicit allow-all CORS default flips to deny at v1.0.0 |
| [`DEP-2026-008`](DEP-2026-008-withopenapi-provider-signature.md) | `removed` | v0.12.0, 2026-07-09 | provider-typed OpenAPI mounts → stdlib `http.Handler` |
| [`DEP-2026-009`](DEP-2026-009-context-html-raw-string.md) | `removed` | applied on `dry-run/2.0`, 2026-10-23; the `v2.0.0` tag publishes it | `nucleus.Context.HTML(code, html)` → `RawHTML` (raw string) and `Render` (template) |
| [`DEP-2026-011`](DEP-2026-011-context-get-set-typed-values.md) | `removed` | applied on `dry-run/2.0`, 2026-10-23; the `v2.0.0` tag publishes it | `Context.Get` / `Context.Set` → typed request values |
| [`DEP-2026-012`](DEP-2026-012-panicking-constructors.md) | `removed` | applied on `dry-run/2.0`, 2026-10-23; the `v2.0.0` tag publishes it | constructors that panic on input → their error-returning forms |
| [`DEP-2026-013`](DEP-2026-013-ignored-storage-block-without-defaults.md) | `completed` | applied on `dry-run/2.0`, 2026-10-23; the `v2.0.0` tag publishes it | a storage block an application built WithoutDefaults() ignores → refused at v2.0.0 |
| [`DEP-2026-014`](DEP-2026-014-unlisted-external-plugins.md) | `completed` | applied on `dry-run/2.0`, 2026-10-23; the `v2.0.0` tag publishes it | external plugins and commands the configuration does not list → refused at v2.0.0 |
| [`DEP-2026-015`](DEP-2026-015-ignored-mail-driver-without-defaults.md) | `completed` | applied on `dry-run/2.0`, 2026-10-23; the `v2.0.0` tag publishes it | a mail driver an application built WithoutDefaults() ignores → refused at v2.0.0 |
| [`DEP-2026-016`](DEP-2026-016-ignored-rate-limit-without-defaults.md) | `completed` | applied on `dry-run/2.0`, 2026-10-23; the `v2.0.0` tag publishes it | a rate limit an application built WithoutDefaults() does not mount → refused at v2.0.0 |
| [`DEP-2026-017`](DEP-2026-017-ignored-authz-config-without-defaults.md) | `completed` | applied on `dry-run/2.0`, 2026-10-23; the `v2.0.0` tag publishes it | authorization configuration and module policy rows an application built WithoutDefaults() without WithAuthz() never enforces → refused at v2.0.0 |
| [`DEP-2026-018`](DEP-2026-018-unguarded-profiler-without-defaults.md) | `completed` | applied on `dry-run/2.0`, 2026-10-23; the `v2.0.0` tag publishes it | the profiler served unguarded by an application built WithoutDefaults() without WithAuthz() → refused at v2.0.0 unless WithAuthz() guards it |
| [`DEP-2026-019`](DEP-2026-019-nucleustest-links-sqlite.md) | `completed` | applied on `dry-run/2.0`, 2026-10-23; the `v2.0.0` tag publishes it | Behaviour-Change Notice: `nucleustest` stops linking SQLite into every test binary at v2.0.0 |
| [`DEP-2026-020`](DEP-2026-020-asynq-provider-to-its-own-module.md) | `completed` | applied on `dry-run/2.0`, 2026-10-23; the `v2.0.0` tag publishes it | Packaging-Change Notice: the asynq jobs provider moves to `providers/tasks-asynq` at v2.0.0 |
| [`DEP-2026-021`](DEP-2026-021-session-idle-timeout-default.md) | `completed` | applied on `dry-run/2.0`, 2026-10-23; the `v2.0.0` tag publishes it | Behaviour-Change Notice: `session_idle_timeout` ships as 30m at v2.0.0 |
| [`DEP-2026-022`](DEP-2026-022-problem-details-default-error-shape.md) | `completed` | applied on `dry-run/2.0`, 2026-10-23; the `v2.0.0` tag publishes it | Behaviour-Change Notice: RFC 9457 problem details become the default error shape at v2.0.0 |
| [`DEP-2026-023`](DEP-2026-023-newwrapresponsewriter-replaced-by-wrap.md) | `removed` | applied on `dry-run/2.0`, 2026-10-23; the `v2.0.0` tag publishes it | `router.NewWrapResponseWriter(w, protoMajor)` → `router.Wrap(w)` |
| [`DEP-2026-024`](DEP-2026-024-pkg-model-leaves-the-public-api.md) | `removed` | applied on `dry-run/2.0`, 2026-10-23; the `v2.0.0` tag publishes it | `pkg/model` leaves the public API at v2.0.0 |
| [`DEP-2026-025`](DEP-2026-025-signals-untyped-door.md) | `removed` | applied on `dry-run/2.0`, 2026-10-23; the `v2.0.0` tag publishes it | the untyped door of `pkg/signals` leaves at v2.0.0 |
| [`DEP-2026-026`](DEP-2026-026-sqlite-time-format.md) | `completed` | applied on `dry-run/2.0`, 2026-10-23; the `v2.0.0` tag publishes it | Behaviour-Change Notice: SQLite stores `time.Time` in a format its date functions read, at v2.0.0 |
