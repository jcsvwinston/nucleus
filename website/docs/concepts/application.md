---
sidebar_position: 1
title: Application container
covers:
  - pkg/app.New
  - pkg/app.App
  - pkg/app.LoadConfig
  - pkg/app.WithoutDefaults
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

Pass `app.WithoutDefaults()` to opt out of the default subsystems and wire
only what you need:

```go
a, err := app.New(cfg, app.WithoutDefaults())
```

This is the path the `api` template uses. From here you attach the
subsystems you actually want using extensions.

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
