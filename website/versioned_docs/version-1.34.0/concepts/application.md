---
sidebar_position: 1
title: Application container
covers:
  - pkg/app.New
  - pkg/app.App
  - pkg/app.LoadConfig
  - pkg/app.WithoutDefaults
  - pkg/app.WithStorage
  - pkg/app.WithMail
  - pkg/app.WithRateLimit
  - pkg/app.WithAuthz
  - pkg/app.WithExtensions
  - pkg/app.Extension
  - pkg/app.Extension.Attach
  - pkg/app.Extension.Shutdown
  - pkg/app.App.Run
  - pkg/app.App.Shutdown
  - pkg/app.App.Database
  - pkg/app.App.DatabaseForRequest
config_keys:
  - databases.default
  - database_default
---

# Application container

`pkg/app` is where a Nucleus application is assembled. One call wires every
subsystem — configuration, logging, databases, sessions, mail, routing — and
returns a validated application ready to run.

Read this page when you need programmatic control over startup: building an
app inside a test, embedding one in a larger binary, or replacing the default
subsystems with your own.

```go
import "github.com/jcsvwinston/nucleus/pkg/app"

cfg, err := app.LoadConfig("nucleus.yml")
if err != nil {
    return err
}

a, err := app.New(cfg)
if err != nil {
    return err
}
defer a.Shutdown(ctx)

return a.Run(ctx)
```

## What `app.New` wires

Called with no options, `app.New(cfg)` initialises:

- the canonical configuration view
- the `slog` logger (`pkg/observe`)
- the SQL database map by alias — `database_default` plus
  `databases.<alias>`
- the mail sender (`pkg/mail`)
- the session manager (`pkg/auth`), backed by the configured store
  (`memory`, `sql` or `redis`)
- the HTTP router and middleware chain (`pkg/router`)
- the request scope resolver, for multi-site and multi-tenant setups
- the model registry (`pkg/model`)

This is the default, full-stack mode, and it matches what the `mvc` scaffold
template produces. The admin panel (orbit) is a separate module you mount
with `.Mount(orbit.Module(...))`; it is not part of the default wiring.

## Core-only mode

Pass `app.WithoutDefaults()` to opt out of the default subsystems —
storage, mail, the rate limiter, and authorization (the RBAC enforcer and
its default-deny middleware) — and wire only what you need:

```go
a, err := app.New(cfg, app.WithoutDefaults())
```

This is the path the `api` template uses. From here you attach the
subsystems you actually want using extensions.

Storage has an option of its own. `app.WithStorage()` builds the storage
subsystem the configuration declares — a `storage:` block in the file, or
`NUCLEUS_STORAGE__*` variables — with the same tenant scoping, cleaner and
public routes the default path gives it, and builds nothing while the
configuration declares none:

```go
a, err := app.New(cfg, app.WithoutDefaults(), app.WithStorage())
```

Without it, a core-only application whose configuration declares storage
still starts with the block ignored, as it always did, but no longer
silently: the boot log carries one ERROR line naming the option, and
`nucleus doctor` reports it. From v2.0.0 that configuration refuses to
start (DEP-2026-013). The loaders record whether storage was declared in
`Config.StorageDeclared`; a `Config` built in Go sets that field to ask for
storage.

Mail is the same shape. `app.WithMail()` builds the mail sender the
configuration declares (`mail_driver`, the `smtp_*` keys); without it, a
core-only application whose configuration writes a `mail_driver` other than
`noop` starts with the driver ignored and one ERROR line at boot naming the
option, and from v2.0.0 that configuration refuses to start
(DEP-2026-015). The loaders record whether `mail_driver` was written in
`Config.MailDeclared`.

So is the rate limiter. `app.WithRateLimit()` mounts the limiter the
`rate_limit_*` keys describe, where the default stack mounts it — after the
API-key read, before the request interceptors — and nothing while
`rate_limit_requests` is 0. A core-only application decodes no bearer token
ahead of it unless it carries `WithAuthz()` too, so without that option it
keys a request by its API key's owner and tenant, and by its client IP
otherwise. Without `WithRateLimit()`, a `rate_limit_requests` above 0 is not
enforced, and the boot log says so in one ERROR line naming the option; from
v2.0.0 that configuration refuses to start (DEP-2026-016).

And so is authorization. `app.WithAuthz()` builds the default stack's
authorization and nothing else of it — the RBAC enforcer with
`rbac_policy_file` and the bootstrap allow-list, the bearer decode ahead of
the API-key read, the limiter and the interceptors, and the default-deny
middleware after them:

```go
a, err := app.New(cfg, app.WithoutDefaults(), app.WithAuthz())
```

Default-deny means what it says: with no policy file and no rows, every
registered route outside the bootstrap allow-list (`/healthz`, `/livez`,
`/readyz`, `/login`, `/.well-known/jwks.json`, `/static/*`, `/metrics`
unless `metrics_public: false`) answers an anonymous request 403, exactly as
on the default stack, and the boot log says `authz: default-deny with 0
policy rows`. The rows mounted modules declare in `Policies` load into the
enforcer (through `pkg/nucleus`), an API key's scopes are subjects of it,
and `/debug/pprof` and the realtime channels sit behind it. `WithOpenAuthz()`
switches the middleware off here as on the default stack.

Without `WithAuthz()` a core-only application has no enforcer, and the
framework authorizes no route. A configuration that asks for one anyway —
`rbac_policy_file`, or `metrics_public: false` while the metrics path is
served — starts with the keys ignored and one ERROR line at boot naming
them and the option; from v2.0.0 it refuses to start without `WithAuthz()`
(DEP-2026-017). The same holds for what the application itself asks to have
guarded. The rows mounted modules declare in `Policies` are discarded, so a
route they keep from anonymous callers answers anyone; when that leaves a
route open, or a deny row unenforced, one ERROR line at boot names the
modules and the routes (DEP-2026-017). And `profiling_enabled: true` serves
`/debug/pprof` — heap and goroutine dumps — to anyone who reaches the port,
which one ERROR line at boot says in place of the default stack's WARN; from
v2.0.0 it refuses to start unless `WithAuthz()` guards the profiler
(DEP-2026-018).

## Extensions

Extensions are first-class pluggable subsystems:

```go
type Extension interface {
    Name() string
    Attach(a *App) error
    Shutdown(ctx context.Context) error
}
```

```go
a, err := app.New(cfg,
    app.WithoutDefaults(),
    app.WithExtensions(myExtension),
)
```

`Attach` runs at startup and can register routes, middleware, models, and
shutdown hooks on the application. `Shutdown` runs during graceful shutdown,
in reverse attach order.

## What `App` exposes

| Member                            | Purpose                                       |
| --------------------------------- | --------------------------------------------- |
| `App.DB`                          | The default database handle.                  |
| `App.DBs`                         | All opened databases keyed by alias.          |
| `App.Database(alias)`             | Look up a specific database.                  |
| `App.DatabaseForRequest(r)`       | Resolve the database for the current request scope (multi-site / multi-tenant). |
| `App.Router`                      | The mounted router.                           |
| `App.Models`                      | The model registry.                           |
| `App.Run(ctx)` / `App.Shutdown(ctx)` | Lifecycle entry points.                    |

## Lifecycle

`App.Run` blocks. It listens, serves traffic, and — when the context is
cancelled — runs each registered shutdown hook in reverse attach order before
returning. `server.shutdown_timeout` in `nucleus.yml` bounds the graceful
timeout.

There are no hidden globals. `App` registers no singleton, so each `app.New`
call produces an independent application. That is what makes end-to-end
testing straightforward: run a real `App` on a random port and tear it down
when the test finishes.
