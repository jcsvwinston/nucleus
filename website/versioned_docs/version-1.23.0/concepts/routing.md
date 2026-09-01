---
sidebar_position: 3
title: Routing & middleware
covers:
  - pkg/nucleus.New
  - pkg/nucleus.WithTemplatesFS
  - pkg/app.WithTemplatesFS
  - pkg/nucleus.Router
  - pkg/nucleus.Router.With
  - pkg/nucleus.Module
  - pkg/nucleus.Methods
  - pkg/nucleus.Handler
  - pkg/nucleus.Middleware
  - pkg/router.New
  - pkg/router.Context
  - pkg/router.Context.Param
  - pkg/router.Context.Query
  - pkg/router.Context.JSON
  - pkg/router.FromHTTP
  - pkg/router.CORSMiddleware
  - pkg/router.CSRFMiddleware
  - pkg/router.RateLimitMiddleware
  - pkg/router.TelemetryMiddleware
  - pkg/router.Recoverer
  - pkg/router.RequestID
  - pkg/router.BindForm
  - pkg/app.App.MountOpenAPI
config_keys:
  - rate_limit_requests
  - rate_limit_window
  - rate_limit_burst
  - rate_limit_by_route
---

# Routing & middleware

Nucleus has two routing surfaces, and application code should almost always
use the first:

- **`pkg/nucleus`** — the module-facing layer. This is the recommended entry
  point, and what your module's `Routes` function receives.
- **`pkg/router`** — the lower-level implementation. You need it only to
  integrate a third-party HTTP handler, or when building an application
  directly with `pkg/app`.

## Defining routes (module layer — `pkg/nucleus`)

Inside a `Module[C].Routes` callback, the `nucleus.Router` interface is
the only surface you should use. It does not expose any `pkg/router`
types, so modules do not take a hard dependency on the router
implementation.

```go
var articlesModule = nucleus.Module[struct{}]{
    Name:   "articles",
    Prefix: "/api/articles",
    Routes: func(r nucleus.Router, _ struct{}) {
        r.Get("/",     listArticles)
        r.Post("/",    createArticle)
        r.Get("/{id}", showArticle)
        r.Put("/{id}", updateArticle)
        r.Delete("/{id}", deleteArticle)
    },
}
```

`nucleus.Router` supports three coexisting styles:

- **Flat declarative** — `r.Get("/path", handler)` for simple or
  audit-sensitive modules.
- **REST resource** — `r.Resource("/path", controller, nucleus.Methods(...))` for
  CRUD modules. Only the requested verbs are registered; reflection is
  not used.
- **Nested groups** — `r.Group("/prefix", func(g nucleus.Router) { ... })` for
  areas with nested URL hierarchy. Middleware added inside the callback
  is scoped to the group.

```go
Routes: func(r nucleus.Router, _ struct{}) {
    r.Group("/admin", func(g nucleus.Router) {
        g.Get("/stats", adminStats)
        g.Get("/users", listUsers)
    })
},
```

`Middleware` type: `func(http.Handler) http.Handler` — standard
`net/http` middleware. No framework-specific wrapper type is needed.

### Per-route middleware (`Router.With`)

`Router.With(mw ...Middleware) Router` returns a new `Router` whose
middleware applies only to routes registered on it. Routes registered
directly on the parent `r` are not affected — this is the per-route
counterpart to the module-level `Module.Middleware` field, which wraps
every route in the module.

```go
// Guard a single route without affecting sibling routes.
// enforcer is *authz.Enforcer captured from OnStart.
Routes: func(r nucleus.Router, _ struct{}) {
    r.Get("/products", listProducts)                              // no auth
    r.With(enforcer.RequireRole("admin")).Get("/billing", billing) // admin only
},
```

`With` composes additively: chained or nested calls layer middleware
outer-to-inner. Any `func(http.Handler) http.Handler` value works directly
— `Enforcer.RequireRole`, `router.CSRFMiddleware`, or a hand-written guard
— with no adapter needed.

## Lower-level routing (`pkg/router`)

`pkg/router` is used directly only in two cases:

1. You are assembling an app with `pkg/app` (not `pkg/nucleus`).
2. You need to mount an arbitrary `http.Handler` via
   `a.Router.Mount(prefix, handler)`.

In the `pkg/app` context, `a.Router` is a `*router.Router` and handlers
receive `*router.Context`:

```go
// pkg/app-level wiring (not module code)
a.Router.Get("/api/articles", listArticles)
a.Router.Post("/api/articles", createArticle)

a.Router.Mux.Route("/admin/api", func(sub *router.Mux) {
    sub.Use(adminAuthMiddleware)
    sub.Get("/stats", adminStats)
})
```

`router.Handler` is `func(*router.Context) error`; errors bubble up to
the recovery / logging middleware.

`Router.Mount(prefix, handler)` mounts an arbitrary `http.Handler` —
useful for embedding third-party handlers or a second app.

## The `Context` type

Handlers receive a `*router.Context` — or, in fluent mode, a
`*nucleus.Context` that wraps it. The context exposes:

- `Request` / `ResponseWriter`
- path parameters via `c.Param("id")`
- query string helpers (`c.Query`, `c.QueryInt`, …)
- body binding (`c.BindJSON`, `c.BindXML`, `c.BindForm`)
- response helpers (`c.JSON`, `c.XML`, `c.String`, `c.Status`)
- the request-scoped `context.Context`
- the resolved request scope (site, tenant) when multi-site is on

### Body binding

The three binders differ in one important way — whether they validate:

| Binder | Accepts | Runs `validate` tags |
|---|---|---|
| `c.BindJSON` | JSON | Yes — returns a `*DomainError` on failure |
| `c.BindForm` | `application/x-www-form-urlencoded`, `multipart/form-data` | Yes |
| `c.BindXML` | XML | **No** |

`c.BindForm` decodes into a struct pointer and performs typed conversion
before validating. Its rules:

- **Field resolution order** — a `form:"name"` tag wins, then `json:"name"`,
  then the case-insensitive field name. `form:"-"` skips a field.
- **Supported types** — string, bool (an HTML checkbox value of `"on"` binds
  as true), signed and unsigned integers, floats, `time.Time` (RFC 3339,
  `2006-01-02T15:04`, or `2006-01-02`), and pointers to any of those.
- **Embedded exported structs** are flattened.
- **Present-but-empty values** leave the field at its zero value, and unknown
  keys are ignored.

## Built-in middleware

The default middleware chain (full-stack mode) installs:

| Middleware            | Purpose                                            |
| --------------------- | -------------------------------------------------- |
| Recovery              | Recovers from panics, logs with stack trace.       |
| Request ID            | Generates / propagates an X-Request-ID.            |
| Structured logging    | Emits one `slog` line per request with timing.    |
| OpenTelemetry         | Wraps the handler in an OTel span (when enabled). |
| CORS                  | Configured from `cors_origins` / `cors_allow_credentials`; empty `cors_origins` denies cross-origin (v1.0.0 default). |
| CSRF                  | **Opt-in — off by default.** Set `csrf_enabled: true` to mount it on the default stack, or mount `router.CSRFMiddleware(opts)` / `router.WithCSRF` per module. |
| Rate limiting         | Configured from `rate_limit_*` keys.               |
| Request scope         | Resolves multi-site / multi-tenant context.        |

Every auto-mounted middleware can be turned off from configuration, and none
of them rely on hidden state. CSRF is the exception in the other direction:
it is off by default and you turn it on, either with `csrf_enabled: true` or
per module (see [Auth & sessions](../features/auth/index.md) for the module-scoped
pattern).

The order of the auto-mounted middleware is fixed. Handlers can rely on the
request already carrying a logger, a request ID and a span by the time they
run.

## Custom middleware

```go
func auditMiddleware(next router.Handler) router.Handler {
    return router.Handler(func(c *router.Context) error {
        start := time.Now()
        err := next(c)
        slog.InfoContext(c.Request.Context(),
            "audit",
            "method", c.Request.Method,
            "path",   c.Request.URL.Path,
            "took",   time.Since(start),
        )
        return err
    })
}

r.Use(auditMiddleware)
```

`router.Handler` is a thin wrapper over `http.Handler` that returns an
`error`. Errors bubble up to the recovery / logging middleware where they
are translated into a JSON or HTML response according to the request
`Accept` header.

## Interceptors declared in configuration

`r.Use` and a module's `Middleware` field both need the person **assembling
the application** to write the code, in the right place, in the right
order. That is fine for middleware you wrote for your own app, and no use
at all for middleware somebody wants to distribute: it cannot be shipped
as a package, only pasted into a bootstrap.

An interceptor is a package that registers itself, exactly like a storage
provider or an authentication backend:

```go
package audit

import "github.com/jcsvwinston/nucleus/pkg/router/interceptor"

func init() {
    interceptor.Register("audit", New)
}

func New(cfg interceptor.Config) (interceptor.Interceptor, error) {
    var settings struct {
        Sink string `koanf:"sink"`
    }
    if err := cfg.Bind(&settings); err != nil {
        return nil, err
    }
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // ...
            next.ServeHTTP(w, r)
        })
    }, nil
}
```

The application imports it for the side effect and the operator places it:

```go
import _ "example.com/audit"
```

```yaml
http_interceptors: [audit, tenant-guard]
interceptors:
  audit:
    sink: stdout
  tenant-guard:
    header: X-Tenant
```

**The list is ordered and the order is the behaviour.** First is
outermost: it sees the request first and the response last.
Authentication before rate limiting and rate limiting before
authentication are different systems, so an interceptor is not merely
switched on here, it is *placed* — the same way `auth_backends` places a
backend. Settings live under `interceptors.<name>.*`, mirroring how
`auth_backends` pairs with `auth.<name>.*`.

A name nobody registered **fails at boot**, naming what is registered, and
so does a factory that cannot configure itself. A typo in a list of
request interceptors must not resolve to one fewer protection, quietly.

Interceptors are mounted **inside** the framework's own middleware — after
the request ID, the session and the observability hook. An interceptor
that displaced those would break everything downstream, including its own
logging. The order among interceptors is yours; the order relative to the
framework is not.

## Server-rendered templates

`app.New` loads every `.html` under `templates_dir` (default
`internal/web/templates`) **recursively** at startup and wires the engine
into the router, so handlers render with the template variant of `HTML`:

```go
// In a module handler (nucleus.Context): the engine-backed render lives on
// the embedded router context — nucleus.Context.HTML(code, raw) writes a
// raw string instead.
return c.Context.HTML(http.StatusOK, "fieldservice/index.html", data)
```

**Naming rule:** each file registers under its path relative to
`templates_dir`, with forward slashes. So
`internal/web/templates/fieldservice/index.html` registers as
`"fieldservice/index.html"`.

Two consequences: files at the root keep their flat name (`"base.html"`), and
`{{define "name"}}` blocks register under their declared names as always.
This is the same layout `nucleus startapp` scaffolds, and the scaffolded page
route renders through it.

The startup log reports `templates loaded` with the directory and the count.
A configured directory that exists but contains no `.html` logs a WARN, so a
misconfigured path is visible rather than surfacing later as
"template engine is not configured" on every render.

### Template functions and prebuilt bases

Presentation logic belongs in templates. `app.WithTemplateFuncs` registers a
`template.FuncMap` available to every template the loader parses, and
`app.WithTemplates` injects a prebuilt `*template.Template` as the base the
directory parses into (its templates and `{{define}}` blocks stay
available). Order at startup: registered functions → recursive parse of
`templates_dir` → the engine is wired into the router.

```go
a, err := app.New(cfg, app.WithTemplateFuncs(template.FuncMap{
    "fecha": func(t time.Time) string { return t.Format("02/01/2006") },
}))
```

The fluent builder exposes the same options directly:
`nucleus.New().WithTemplateFuncs(...)` and `.WithTemplates(...)`. Every public
application option has a builder counterpart, and a parity test enforces it.

### Embedded template sources (`WithTemplatesFS` and `Module.Templates`)

`app.WithTemplatesFS(prefix, fsys)` parses every `.html` file of an
`fs.FS` — typically an `embed.FS` — into the engine under
`<prefix>/<path>` names. Unlike `WithTemplates` it accumulates: each call
adds a source. Load order fixes the collision rule: the `WithTemplates`
base first, then every FS source, then `templates_dir` — so the host's
on-disk files always override an embedded source's.

A module usually does not call it directly: declaring `Module.Templates`
registers the module's embedded templates automatically under the
module's name, and a handler renders them with
`c.Context.HTML(status, "<module-name>/<path>", data)`:

```go
//go:embed templates/*.html
var templatesDir embed.FS

func Module() nucleus.ModuleSpec {
    templates, _ := fs.Sub(templatesDir, "templates")
    return nucleus.Module[struct{}]{
        Name:      "shop",
        Templates: templates, // renders as "shop/index.html", …
        Routes: func(r nucleus.Router, _ struct{}) {
            r.Get("/shop", func(c *nucleus.Context) error {
                return c.Context.HTML(http.StatusOK, "shop/index.html", nil)
            })
        },
    }.Build()
}
```

## Mounting an OpenAPI document

The runtime ships an explicit OpenAPI mount:

```go
import "github.com/jcsvwinston/nucleus/pkg/openapi"

// MountOpenAPI takes an openapi.DocumentProvider — a func() *openapi.Document.
a.MountOpenAPI("/api/openapi.json", func() *openapi.Document { return myDoc })
```

There is no auto-generation of the document from handler reflection —
that path was deliberately not taken. The contract you ship is the one
you wrote.
