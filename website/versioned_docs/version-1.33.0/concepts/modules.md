---
sidebar_position: 4
title: Modules — wiring and lifecycle
covers:
  - pkg/nucleus.Module
  - pkg/nucleus.Module.DependsOn
  - pkg/nucleus.Module.OnStart
  - pkg/nucleus.Module.OnShutdown
  - pkg/nucleus.Runtime
  - pkg/nucleus.Provide
  - pkg/nucleus.Resolve
  - pkg/nucleus.ErrNotProvided
  - pkg/nucleus.ErrAlreadyProvided
  - pkg/nucleus.ErrModuleDependency
  - pkg/nucleus.Key
  - pkg/nucleus.NewKey
  - pkg/nucleus.Key.WithValue
  - pkg/nucleus.Key.FromContext
  - pkg/nucleus.SetValue
  - pkg/nucleus.Value
  - pkg/nucleus.LifecycleHooks
---

# Modules — wiring and lifecycle

An application is a set of modules. This page is about how they meet: in
what order they start and stop, how one module hands another a value, how a
value travels with one request, and what happens when a module cannot start.

## Start order: `DependsOn`

A module that needs another one started first says so:

```go
var Reports = nucleus.Module[ReportsConfig]{
    Name:      "reports",
    Prefix:    "/reports",
    DependsOn: []string{"billing"},
    // ...
}.Build()
```

The application starts its modules in an order that puts every module after
the modules it names in `DependsOn`. Modules that declare nothing, and ties
among the rest, start in name order — the order every application had
before the field existed, so mounting a module that declares nothing changes
nothing. Shutdown runs in the reverse order: a module stops before the
modules it depends on.

A name in `DependsOn` that is not mounted, or a cycle — two modules that
depend on each other, through any chain — stops the boot before anything
starts, with an error that wraps `nucleus.ErrModuleDependency` and names the
modules:

```text
nucleus: invalid module dependency: module dependency cycle: "billing" -> "reports" -> "billing"
```

`DependsOn` names modules. `Requires`, the field next to it, names
**database aliases** the module needs configured.

## One module provides, another resolves

A module offers a value to the rest of the application in its `OnStart`,
under a Go type, and another module asks for it by that type:

```go
// The type consumers ask for. An interface keeps them off the implementation.
type Invoicer interface {
    Invoice(ctx context.Context, accountID string) (Invoice, error)
}

var Billing = nucleus.Module[struct{}]{
    Name: "billing",
    OnStart: func(ctx context.Context, rt nucleus.Runtime, _ struct{}) error {
        return nucleus.Provide[Invoicer](rt, &invoicer{db: rt.DB()})
    },
}.Build()

var Reports = nucleus.Module[struct{}]{
    Name:      "reports",
    Prefix:    "/reports",
    DependsOn: []string{"billing"},
    OnStart: func(ctx context.Context, rt nucleus.Runtime, _ struct{}) error {
        inv, err := nucleus.Resolve[Invoicer](rt)
        if err != nil {
            return err
        }
        reports.invoicer = inv // the Routes closure below runs after OnStart
        return nil
    },
    Routes: func(r nucleus.Router, _ struct{}) {
        r.Get("/monthly", reports.monthly)
    },
}.Build()
```

The rules that keep this predictable:

- **The type is the key.** `Provide[Invoicer](rt, impl)` provides an
  `Invoicer`; `Provide(rt, impl)` without the type argument provides
  whatever `impl`'s own type is, and `Resolve[Invoicer]` would not find it.
  When that happens the error says so and names the type that was provided.
- **One value per type.** A second `Provide` of the same type is an error,
  wrapping `nucleus.ErrAlreadyProvided`, that names both modules. To offer
  two values of one underlying type, give each its own named type.
- **Provide in `OnStart`.** Once every module has started, `Provide` is
  refused: what was provided is what there is. A nil value is refused too.
- **Declare what you resolve.** A module that resolves in its `OnStart`
  declares the provider in `DependsOn`, so the provider has started. If it
  does not and the provider has not started yet, `Resolve` returns an error
  wrapping `nucleus.ErrNotProvided` that says to declare it. If it does not
  and the provider happens to start first by name, the value is returned and
  the log carries a warning: the next rename would break it.

`Resolve` also works after startup, from any `Runtime` the application
handed out — a test reaches the application's values through the test
server's `Runtime()`.

## Values that live for one request

A value that belongs to one request — the tenant a middleware resolved, the
account a handler loaded — travels under a typed key. Create the key once,
at package level, and share the variable between the code that sets the value
and the code that reads it:

```go
var CurrentTenant = nucleus.NewKey[Tenant]("tenant")
```

A middleware stores the value on the request's context:

```go
func ResolveTenant(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        t := lookupTenant(r)
        next.ServeHTTP(w, r.WithContext(CurrentTenant.WithValue(r.Context(), t)))
    })
}
```

and a handler reads it back, typed — no assertion, no string key that two
packages could both pick:

```go
func (h *handlers) dashboard(c *nucleus.Context) error {
    t, ok := nucleus.Value(c, CurrentTenant)
    if !ok {
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "no tenant"})
    }
    return c.JSON(http.StatusOK, h.summary(t))
}
```

Inside a handler chain, `nucleus.SetValue(c, key, v)` stores a value the
rest of the chain reads with `nucleus.Value`. Two keys are the same key only
if they are the same variable: two `NewKey` calls with the same name are two
keys.

`Context.Get` and `Context.Set`, the string-keyed form, keep working and
are deprecated, with removal in the 2.0 major. `Set` also added data to the
template `Render` executes; for that, pass the data to `Render` itself, or
call `BindData` before rendering.

## When a module cannot start

`OnStart` runs for each module in start order, before any route is
registered. When one returns an error the application does not start, and it
does not leave the others running: every module whose `OnStart` succeeded
gets its `OnShutdown`, in reverse order, then the framework closes its own
resources (database pools, the session store) and, if it ran, the
application-level `LifecycleHooks.OnShutdown` runs. The module whose
`OnStart` failed gets no `OnShutdown` — it cleans up what it opened before
returning its error.

The error `Run` returns is the start error, joined with any error those
`OnShutdown` hooks returned, so `errors.Is` finds each of them:

```text
nucleus: module "reports" OnStart: open the export bucket: access denied
app.Shutdown hook[4]: nucleus: module "billing" OnShutdown: flush pending invoices: ...
```

The same teardown runs for any failure after the application container
exists: a broken job or webhook registration, two modules claiming one
route, a jobs runtime or an outbox that cannot start.
