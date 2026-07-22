---
sidebar_position: 3
title: Release notes
covers: []
config_keys: []
---

# Release notes

The current release is **v1.6.0**. {/* x-release-please-version */}

Nucleus is on the stable `v1.x` line (`v1.0.0` tagged 2026-07-10): stable
surfaces are frozen by contract tests, and every `v1.x` upgrade is designed
to be drop-in for code that uses them — see
[Support & compatibility](../architecture/compatibility.md) and the
[upgrade guide](../operations/upgrade.md). Commit-level detail for every
release, including the pre-1.0 history, lives on
[GitHub Releases](https://github.com/jcsvwinston/nucleus/releases).

## v1.6.0 (2026-07-22)

Defense-in-depth hardening of the webhook surface, from the first directed
security review of the continuous-audit regime. No behaviour changes for
correctly-configured apps; the wire is unchanged.

### Added

- **Webhook registration rejects non-canonical mounts.** A module webhook
  registered with `path == "/"` (which would mount a catch-all subtree) or a
  module name containing `..`/`/` (which would shift the mount point) now
  fails boot instead of mounting something surprising. Canonical paths and
  names are unaffected.
- **Outbox payload-encoding header is informational by design.** The bridge
  signs the body only — byte-for-byte the module-webhook scheme, so one
  verifier covers both surfaces — and `X-Outbox-Payload-Encoding` is now
  documented as unsigned/informational. New consumer helper
  `outbox.CheckPayloadEncoding` decodes by the encoding a consumer expects
  and rejects a mismatch (`ErrPayloadEncodingMismatch`), rather than trusting
  the request header. The signed wire is unchanged from v1.5.0.

### Upgrade notes

Drop-in. If a module registered a webhook at `/` or with a slash in its
name, that was already broken (unreachable or mis-mounted) and now fails
loudly at boot — give it a real path. Outbox consumers should verify the
body-only signature with the module-webhook verifier and check the payload
encoding against their own config (see `CheckPayloadEncoding`), not the
request header.

## v1.5.0 (2026-07-22)

Signs and versions the outbox webhook contract, hardens module webhooks
(canonical paths, opt-in anti-replay), and fixes Oracle pagination and S3/GCS
not-found detection. Drop-in: the outbox wire is unchanged by default and the
new webhook behaviour is opt-in.

### Added

- **The outbox bridge webhook has a signed, versioned contract.** With
  `outbox.bridges.<n>.config.secret`, every delivery carries an HMAC-SHA256
  signature over the body in `X-Nucleus-Signature` (`sha256=<hex>`) — the same
  scheme module webhooks verify, so one verifier covers both. Every delivery
  also declares its payload shape in `X-Outbox-Payload-Encoding: json|base64`,
  so a consumer never guesses. The wire is **byte-for-byte the v1.4.0 default
  (base64)**; `payload_encoding: json` opts into embedding the payload as
  JSON. A body-level contract test compares the emitted webhook byte for byte
  per variant — the gap the symbol-only freeze cannot see. Without a secret,
  deliveries are unsigned and a boot WARN says so. See
  [Storage & background tasks](../features/storage-and-tasks.md).
- **Webhook anti-replay (opt-in).** `WebhookSpec.TimestampTolerance > 0`
  requires an `X-Nucleus-Timestamp` header inside the signed material
  (`SignWebhookBodyWithTimestamp`), rejecting stale or tampered timestamps.
  The default (tolerance 0) keeps the body-only scheme unchanged. The absence
  of anti-replay in the default scheme is now documented as a limit, with
  event-ID dedup as the recommended pattern.

### Fixed

- **Oracle pagination emits valid SQL.** `FindAll` and `FindByID` in
  `pkg/model` used `LIMIT` on Oracle (ORA-00933); they now use
  `OFFSET … FETCH NEXT … ROWS ONLY` / `FETCH FIRST 1 ROWS ONLY`, the twin of
  the earlier MSSQL fix. The admin-user CLI lookup is fixed the same way.
  Exercised against a real Oracle in CI.
- **Webhook paths must be canonical.** A module webhook registered with a
  non-canonical path (`..`, `.`, doubled or trailing slash) now fails boot
  instead of mounting an unreachable route.
- **S3/GCS not-found by SDK type, not error text.** `Get`/`Exists` of a
  missing key now map to `storage.ErrNotFound` against real endpoints
  (previously matched on the error string, which a real S3 endpoint does not
  produce). A real-MinIO CI lane covers it.
- **Security:** `golang.org/x/text` bumped to v0.39.0 (GO-2026-5970).

### Upgrade notes

Nothing to change. If a bridge was relying on the base64 payload wire, it is
unchanged; opt into `payload_encoding: json` when your consumer is ready.
Configure `outbox.bridges.<n>.config.secret` to start signing deliveries.

## v1.4.0 (2026-07-20)

Module jobs and webhooks are now executed, not just declared: the `Jobs`
and `Webhooks` closures a module registers run for real, backed by the
existing task runtime and the application router. Also fixes a stop-path
bug in the Asynq task provider and rejects, on request, primary keys
assigned by HTTP clients. Drop-in upgrade; the new surfaces are opt-in.

### Added

- **Module jobs run on a real scheduler.** `JobRegistry.Register(name,
  spec)` schedules background work declared by a module: `Every` for
  fixed intervals or `Cron` for 5-field cron expressions and descriptors
  (`@hourly`, `@every 90s`), validated at boot and identical on every
  provider; per-run `Timeout`; and `Singleton` to skip a tick while the
  previous run is still executing. The `jobs_provider` key selects the
  runtime — `memory` (default, in-process) or `asynq` (Redis-backed,
  durable, with `jobs_redis_url` and `jobs_concurrency`). A broken
  registration (duplicate name, invalid cron, missing handler) fails boot
  instead of silently never running. See
  [Module jobs and webhooks](../features/storage-and-tasks.md#module-jobs-and-webhooks).
- **Module webhooks mount real routes.** `WebhookRegistry.Register(path,
  spec)` mounts an inbound receiver at `<webhooks_prefix>/<module><path>`
  behind a method allow-list (405), a body cap (413, default 1 MiB) and —
  when `Secret` is set — constant-time HMAC-SHA256 verification of the
  `X-Nucleus-Signature` header, rejecting unsigned or mis-signed requests
  with 401 before your handler runs. `nucleus.SignWebhookBody` produces
  the signature for senders and tests. With `csrf_enabled: true` the
  webhook prefix is exempted automatically — webhooks authenticate by
  signature, not CSRF token. A webhook registered without a `Secret` is
  flagged at boot.
- **`RejectClientPK`.** A per-model opt-in that rejects entities arriving
  through `Create` with a client-assigned primary key
  (`model.ErrClientAssignedPK`), for apps that bind request bodies
  straight into models. The check runs before hooks, so server-side key
  assignment in `BeforeCreate` keeps working.

### Fixed

- **The Asynq task worker stops when you stop it.** `Manager.Run` waited
  on OS signals internally, so cancelling its context (or calling
  `Close`) shut the server down but never unblocked `Run` — an embedded
  worker could not be stopped through the API. `Run` now returns promptly
  on context cancellation and on `Close`.
- **Boot no longer warns about declared jobs and webhooks.** The
  "background execution is not yet wired" readiness warning is gone —
  both surfaces execute. The warning for embedded migrations stays:
  Nucleus is SQL-first and never auto-applies them.

### Upgrade notes

Nothing to change in existing apps. If a module already declared `Jobs`
or `Webhooks` closures (previously inert), they now execute on the next
boot: review those closures before upgrading, set `jobs_provider` if you
want durability over the in-process default, and note that invalid
registrations that were silently ignored before now fail startup — which
is the point.

## v1.3.3 (2026-07-19)

A correctness patch: client-assigned primary keys work through `Create`,
unsupported engines fail at startup instead of at runtime, and two more
surfaces emit valid T-SQL. Drop-in for most apps — read the upgrade notes
if you point the `sql` session store or the outbox at SQL Server or Oracle.

### Fixed

- **A pre-assigned primary key now travels in the `INSERT`.**
  Client-generated keys (UUIDs, natural keys) were silently dropped from
  the insert: SQLite stored a row with a `NULL` primary key without any
  error, and PostgreSQL/SQL Server failed with a `NOT NULL` violation.
  A non-zero key is now included in the statement, and the read-back /
  back-fill machinery is skipped, so the entity keeps exactly the key you
  set. A zero-value key keeps the previous behavior: the column stays out
  of the `INSERT` and the database generates the key. See
  [Models & database](../concepts/models-and-database.md#how-create-treats-the-primary-key)
  — including the security note on accepting keys from HTTP clients.
- **The SQL session store and the outbox refuse unsupported engines at
  startup.** Both subsystems speak SQLite, PostgreSQL and MySQL only, but
  an MSSQL or Oracle database URL used to be silently treated as SQLite —
  the failure surfaced later, mid-request, as invalid SQL. Construction
  now fails at startup with an error naming the supported engines.
- **`not null` is matched exactly in `db:` tags.** `db:"not null unique"`
  (a space where a `;` was intended) used to mark the field required and
  silently lose the `unique`; the malformed directive now falls through to
  the startup `WARN` introduced in v1.3.2 instead of half-applying.
- **By-id operations reject models without a primary key.** `FindByID`,
  `Update` and `Delete` on a model that declares no primary key return an
  explicit "model has no primary key" error (check with `errors.Is`)
  instead of guessing a phantom `id` column, and the default list ordering
  falls back to a real column of the model.
- **`nucleus createuser` and `nucleus changepassword` emit valid T-SQL.**
  Their admin-user lookups used a `LIMIT` clause SQL Server does not
  accept; on MSSQL they now use `SELECT TOP 1`.

### Upgrade notes

If your configuration points the `sql` session store or the outbox at an
MSSQL or Oracle database, the app now stops at startup with a clear error
instead of failing later with invalid SQL. That configuration never worked
— it silently ran SQLite-flavored SQL against the wrong engine — but a
deployment that "started fine" before the upgrade will now refuse to boot
until those subsystems point at a supported engine (SQLite, PostgreSQL,
MySQL).

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
