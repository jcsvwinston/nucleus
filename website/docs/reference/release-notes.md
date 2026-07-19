---
sidebar_position: 3
title: Release notes
covers: []
config_keys: []
---

# Release notes

The current release is **v1.3.2**. {/* x-release-please-version */}

Nucleus is on the stable `v1.x` line (`v1.0.0` tagged 2026-07-10): stable
surfaces are frozen by contract tests, and every `v1.x` upgrade is designed
to be drop-in for code that uses them — see
[Support & compatibility](../architecture/compatibility.md) and the
[upgrade guide](../operations/upgrade.md). Commit-level detail for every
release, including the pre-1.0 history, lives on
[GitHub Releases](https://github.com/jcsvwinston/nucleus/releases).

## v1.3.2 (2026-07-19)

A correctness patch focused on the model layer's `db:` tags and on
`Create` across database engines. Drop-in.

### Fixed

- **Unknown `db:` tag directives now warn at startup.** A directive the
  parser does not recognize was — and still is — applied as nothing; the
  difference is that the app now logs one startup `WARN` per affected
  field, naming the unrecognized tokens and the supported syntax, instead
  of leaving you trusting a constraint that never existed. `db:"-"` now
  excludes a field from persistence.
- **`Create` only reads back the generated key when it actually can.**
  The `RETURNING` / `OUTPUT INSERTED` read-back is now emitted only for
  models that declare a real, integer primary-key field. Models with
  string/UUID keys or without a declared primary key previously got a
  read-back query that could fail (for example against tables with no
  `id` column); they now take the plain insert path, matching
  SQLite/MySQL behavior.
- **List pagination on SQL Server emits valid T-SQL.** Paginated list
  queries used a `LIMIT` clause SQL Server does not accept; they now use
  the `OFFSET … FETCH` form. The whole CRUD surface is exercised against a
  real SQL Server (and Oracle) in release validation.
- **The version pinned by `nucleus new` can no longer go stale.** The
  framework version written into generated `go.mod` files is maintained by
  the release tooling and cross-checked in CI on every build.

### Upgrade notes

Nothing to change. If your startup logs show new `WARN` lines about `db:`
tags, those tags were already being ignored — fix the tag syntax, don't
silence the log. See
[the FAQ](../faq.md#my-startup-log-warns-about-unrecognized-db-tag-directives)
for the supported directives.

## v1.3.1 (2026-07-15)

A one-fix patch. Upgrade if `Create` should hand you the generated primary
key on PostgreSQL or SQL Server.

### Fixed

- **`Create` backfills the generated primary key on PostgreSQL and SQL
  Server.** Those drivers do not implement `LastInsertId`, so the entity's
  ID field silently stayed at zero after a successful insert. `Create` now
  uses `RETURNING` (PostgreSQL) / `OUTPUT INSERTED` (SQL Server) to
  populate it. Oracle remains a declared gap — see
  [Support & compatibility](../architecture/compatibility.md#databases).

## v1.3.0 (2026-07-13)

A minor release that completes the v1.2.0 security hardening pass and
rounds out observability.

### New

- **Opt-in driver-level SQL instrumentation** (`sql_driver_instrumentation`).
  Off by default (zero hot-path cost); when enabled, direct
  `QueryContext`/`ExecContext` statements that bypass the model layer —
  session stores, outbox dispatch, migrations, raw SQL — also reach the
  observability live SQL feed, without double-recording CRUD statements.
- **The observability package and its hooks are now stable**, covered by
  the same compatibility promise as the rest of the framework.

### Security

- **CSRF protection as a config switch.** `csrf_enabled: true` mounts
  origin verification (`Sec-Fetch-Site`) with a double-submit token
  fallback; `csrf_exempt_paths` excludes Bearer-only subtrees. The `mvc`
  scaffold enables it by default.
- **`metrics_public: false`** takes `/metrics` out of the anonymous
  allow-list and puts it behind the default-deny RBAC enforcer.

### Upgrade notes

Both new switches default to the previous behavior (`csrf_enabled: false`,
`metrics_public: true`); nothing changes until you opt in.

## v1.2.0 (2026-07-12)

A security-hardening minor. **Existing deployments can notice these changes
at upgrade time** — read the notes below.

### Security

- **`jwt_secret` must be at least 32 bytes.** Any non-empty value used to
  be accepted; a shorter secret is now a boot error. Generate a proper one
  (`openssl rand -base64 32`) or move to `jwt_keys[]`.
- **Proxy headers are no longer trusted by default.** `X-Forwarded-For` /
  `X-Real-IP` are ignored unless the immediate peer is listed in the new
  `trusted_proxies` key; otherwise the TCP peer address is the client IP
  for rate limiting and logs.
- **HSTS is emitted only over TLS or when explicitly forced**
  (`env: production`) — plain-HTTP development runs are no longer pinned
  to HTTPS by a stray header.

### Upgrade notes

- Short `jwt_secret` values fail the boot — rotate the secret before
  upgrading.
- If Nucleus runs behind a load balancer, set `trusted_proxies` to its
  address ranges or rate limiting will see every request as coming from
  the balancer.

## v1.1.0 (2026-07-11)

### New

- **SQL events report rows affected.** The observability feed's SQL events
  carry the driver-reported `RowsAffected`. Additive; drop-in.

## v1.0.0 (2026-07-10)

The first stable release. The compatibility promise starts here: stable
surfaces are pinned by contract freeze tests and change only through the
documented deprecation policy.

### Breaking

- **Cross-origin requests are denied by default.** The implicit allow-all
  CORS default is gone: an empty `cors_origins` now emits no CORS headers
  at all. Deployments that relied on allow-all must opt in explicitly —
  a real origin allow-list, or `cors_origins: ["*"]` to keep the old
  behavior.

### Upgrade notes

If browsers suddenly report CORS errors after this upgrade, set
`cors_origins` to the exact origins your frontend uses. Everything else in
v1.0.0 is the certification of surfaces that already existed in v0.12.x.
