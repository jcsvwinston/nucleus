---
sidebar_position: 8
title: FAQ & troubleshooting
covers: []
config_keys: []
---

# FAQ & troubleshooting

Answers to the questions that actually come up, each reflecting the shipped
behavior of the current release.

## My startup log warns about unrecognized db tag directives

The `db:` struct-tag parser recognizes a fixed set of directives, separated
by semicolons: `column:<name>`, `pk`, `fk:<table.column>`, `fk:<k=v,…>`,
`index[:name]`, `unique[:name]`, `not null`, `required`, `readonly`,
`tenant`, and `"-"` (exclude the field). Anything else — a typo, or syntax
borrowed from another ORM such as `db:"id,primary"` — is applied as
**nothing**. The startup `WARN` names the model, the field, and the ignored
tokens so you stop trusting a constraint that does not exist. Fix the tag
rather than silencing the log:

```go
type Article struct {
    ID    int64  `db:"pk"`
    Slug  string `db:"column:slug;unique"`
    Notes string `db:"-"` // not persisted
}
```

## Why is my model's ID still zero after `Create` on Oracle?

Reading back a database-generated key on Oracle needs a `RETURNING … INTO`
output binding that the generic CRUD layer does not use, so on Oracle the
insert succeeds but the entity's ID field keeps its zero value — a declared
limitation, not a silent bug. If you need the generated key immediately,
query it back explicitly. On PostgreSQL and SQL Server the key **is**
backfilled (via `RETURNING` / `OUTPUT INSERTED`) — but only when the model
declares an integer primary key. String/UUID keys are never backfilled on
any engine: generate and set them yourself before `Create`.

## What if my model declares no primary key at all?

Without an explicit `db:"pk"` tag, a field named `ID` is adopted as the
primary key. If there is neither, `Create` still works (plain insert), but
the by-ID operations (`FindByID`, `Update`, `Delete`) fall back to guessing
a column named `id` — and fail at the database if the table has no such
column. Declaring two `pk` fields is an error at metadata extraction.
Declare one primary key explicitly.

## Does `session_store: sql` work on SQL Server or Oracle?

Not reliably. The SQL session store generates dialect-specific SQL for
SQLite, PostgreSQL, and MySQL; a `sqlserver://` or `oracle://` database URL
is not among the recognized dialects, so the store falls back to SQL those
engines can reject at runtime (table setup or session reads/writes may
fail). On those engines use `session_store: redis` (or `memory` for a
single replica).

## Do migrations run automatically when the app starts?

No. Starting the binary starts the HTTP server — nothing touches your
schema. Apply migrations explicitly with `nucleus migrate up` as a deploy
step ([Deployment](./operations/deployment.md#migrations-are-a-deploy-step)).
The programmatic `App.AutoMigrate` creates missing tables for registered
models but never alters existing ones.

## What is the difference between `env: production` and `NUCLEUS_ENV=production`?

They are two different switches. The config key `env: production` changes
*runtime* behavior (HSTS is emitted even behind a TLS-terminating proxy,
and `nucleus health --deploy` stops flagging the environment). The OS
environment variable `NUCLEUS_ENV=production` hardens the *config loader*:
unknown keys in config files become a boot error even if the code
downgraded them to warnings for development. Production deployments should
set both.

## Why does my config file boot fine locally but fail in production with unknown keys?

See the previous answer: your production environment sets
`NUCLEUS_ENV=production`, which forces strict validation, while your
development setup runs with `WithUnknownFields("warn")`. The failure is the
guard working — the listed keys are typos or unregistered keys, and the
error includes a did-you-mean hint. Fix the keys; do not remove the env
var.

## How do I see the SQL my application executes?

Enable the opt-in driver instrumentation:

```yaml
sql_driver_instrumentation: true
```

By default (off), the observability live SQL feed carries only statements
issued through the model layer's CRUD. With the flag on, the `database/sql`
driver is wrapped so direct queries — session stores, outbox dispatch,
migrations, your own raw SQL — reach the feed too, without
double-recording CRUD traffic. The feed is consumed through the
observability event bus (and surfaced in the orbit admin panel's live SQL
view). The expensive sanitize-and-emit work only runs while a subscriber
is attached, so an idle feed costs almost nothing.

## Why does the browser report CORS errors against my API?

Because cross-origin requests are **denied by default**: with no
`cors_origins` configured, Nucleus emits no CORS headers at all. List the
exact origins your frontend uses (or the explicit opt-in `["*"]` for a
deliberately public API):

```yaml
cors_origins:
  - https://app.example.com
```

Note that `cors_allow_credentials: true` only takes effect with a
non-wildcard origin list.

## Why do all my logs and rate limits see the load balancer's IP instead of the client's?

`X-Forwarded-For` / `X-Real-IP` are ignored unless the immediate peer is
listed in `trusted_proxies` — otherwise anyone could spoof their IP to
evade rate limits or poison logs. Set it to your load balancer's ranges:

```yaml
trusted_proxies:
  - 10.0.0.0/8
```

## Why is the session cookie not set when I develop over `http://localhost`?

The session cookie ships with `Secure: true` by default, so browsers drop
it over plain HTTP. For local development, opt out explicitly with
`session_cookie_secure: false` — and leave the default in place anywhere
TLS exists.

## My app fails to boot with "jwt_secret is too short" — why?

`jwt_secret` must be at least 32 bytes (HS256 needs 256 bits of key
material; anything shorter is brute-forceable). Generate a proper secret
with `openssl rand -base64 32`, or move to the multi-key `jwt_keys[]`
setup, which also gives you `kid`-based rotation. Related boot error:
`jwt_secret` is non-nullable — setting it to `null` in a file or exporting
an empty `NUCLEUS_JWT_SECRET=` is rejected rather than silently reverting
to no secret.

## Where did the admin panel go?

The admin panel is not part of the core framework: it lives in the
separate `orbit` Go module and mounts as a regular module, configured
under `modules.orbit.*`. The legacy in-core `admin_*` config keys were
removed in v0.12.0 — the
[Configuration reference](./reference/configuration.md) maps each old key
to its `modules.orbit.*` replacement. See
[Features → Admin](./features/admin.md) for setup.
