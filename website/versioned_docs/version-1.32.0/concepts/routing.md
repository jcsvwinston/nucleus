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
  - pkg/router.Matched
  - pkg/router.WhenMatched
  - pkg/router.Context
  - pkg/router.Context.Param
  - pkg/router.Context.Query
  - pkg/router.Context.JSON
  - pkg/router.FromHTTP
  - pkg/router.CORSMiddleware
  - pkg/router.CSRFMiddleware
  - pkg/router.NewCSRFMiddleware
  - pkg/router.RateLimitMiddleware
  - pkg/router.TelemetryMiddleware
  - pkg/router.Recoverer
  - pkg/router.RequestID
  - pkg/router.BindForm
  - pkg/router.BindQuery
  - pkg/router.BindPath
  - pkg/router.BindHeaders
  - pkg/router.BindRequest
  - pkg/router.Negotiate
  - pkg/router.Timeout
  - pkg/router.WithProblemDetails
  - pkg/router.APIVersion
  - pkg/router.Mux.Version
  - pkg/nucleus.Context.BindQuery
  - pkg/nucleus.Context.BindPath
  - pkg/nucleus.Context.BindHeaders
  - pkg/nucleus.Context.BindRequest
  - pkg/nucleus.Context.Negotiate
  - pkg/nucleus.Context.RawHTML
  - pkg/nucleus.Context.Render
  - pkg/nucleus.APIVersion
  - pkg/nucleus.Versioned
  - pkg/nucleus.Timeout
  - pkg/nucleus.WithProblemDetails
  - pkg/nucleus.AppBuilder.WithProblemDetails
  - pkg/app.WithProblemDetails
  - pkg/errors.Problem
  - pkg/errors.NewProblem
  - pkg/errors.WriteProblem
  - pkg/app.App.MountOpenAPI
  - pkg/nucleus.AppBuilder.WithOpenAPIDocument
  - pkg/nucleus.APIDocumentSpec
  - pkg/nucleus.AppBuilder.WithOpenAPIValidation
  - pkg/nucleus.Handle
  - pkg/nucleus.EndpointOption
  - pkg/nucleus.Status
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
— `Enforcer.RequireRole`, the middleware `router.NewCSRFMiddleware` builds, or a hand-written guard
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
- path parameters as strings via `c.Param("id")`, query parameters via
  `c.Query("page")`
- typed binding of the query, the path and the headers (`c.BindQuery`,
  `c.BindPath`, `c.BindHeaders`) and of the whole request (`c.BindRequest`)
- body binding (`c.BindJSON`, `c.BindXML`, `c.BindForm`)
- response helpers (`c.JSON`, `c.XML`, `c.String`, `c.RawHTML`, `c.Render`,
  `c.Status`) and `c.Negotiate`, which picks the representation from the
  `Accept` header
- the request-scoped `context.Context`
- the resolved request scope (site, tenant) when multi-site is on

### Body binding

All three body binders decode, then validate the struct by its `validate`
tags, and return a `*DomainError` on failure — a 400 for a body that does
not decode, a 413 past the 1 MiB cap, a 422 `VALIDATION_FAILED` naming each
field that failed:

| Binder | Accepts | Runs `validate` tags |
|---|---|---|
| `c.BindJSON` | JSON | Yes |
| `c.BindForm` | `application/x-www-form-urlencoded`, `multipart/form-data` | Yes |
| `c.BindXML` | XML | Yes |

`c.BindJSON` also binds a body that is a JSON array into a slice — a bulk
endpoint declares `var notes []Note`. Each element is validated by the same
tags, and a failure is named by its index and field, `"[1].title"`, in the
same 422 (a map of structs names the key, `"[alice].title"`). A slice of
non-structs (`[]string`, `[]int`) is decoded and has no tags to check. The
1 MiB cap applies to the whole array; an endpoint that takes larger batches
calls `router.BindMax` with its own cap.

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

### Binding the query, the path and the headers

The rest of the request binds the way the body does: declare one input type
for the endpoint, tag where each field comes from, and validate it with the
same `validate` tags.

```go
type ListNotes struct {
    Org     string   `path:"org"`
    Page    int      `query:"page" validate:"omitempty,min=1"`
    Tags    []string `query:"tag"`        // ?tag=a&tag=b
    TraceID string   `header:"X-Trace-Id"`
}

r.Get("/orgs/{org}/notes", func(c *nucleus.Context) error {
    var in ListNotes
    if err := c.BindRequest(&in); err != nil {
        return err
    }
    // in.Page is an int, in.Tags a []string, in.Org the path value
    ...
})
```

| Method | Reads | Tag |
|---|---|---|
| `c.BindQuery(&v)` | the query string | `query:"name"` |
| `c.BindPath(&v)` | the route's `{name}` wildcards | `path:"name"` |
| `c.BindHeaders(&v)` | the headers, names canonicalised | `header:"X-Name"` |
| `c.BindRequest(&v)` | all three, then a JSON body into the `json`-tagged fields | all of the above |

Each validates the whole struct once it has bound. The rules:

- **Types** — the ones `BindForm` converts (string, bool, integers, floats,
  `time.Time`, pointers to those), any type whose pointer implements
  `encoding.TextUnmarshaler` (a UUID type, `netip.Addr`, your own enum), and
  slices of all of them. A slice takes every value of a repeated query key
  or header.
- **Absent or empty** parameters leave the field as it was. `query:"-"` skips
  a field, and a field with none of the tags a binder reads is left alone.
- **Errors name what the client sent.** A value that does not convert is a
  400 `BAD_REQUEST` whose message and `details` name the parameter
  (`{"page": "must be an integer that fits in 64 bits"}`); a struct that does
  not validate is the 422 `VALIDATION_FAILED` the body binders return, with
  each field named as the query parameter, path parameter or header it came
  from — not as the Go field.
- **`BindRequest` reads the body** only for a method that carries one and a
  JSON `Content-Type` (`application/json` or a `+json` type), with
  `BindJSON`'s cap and errors. The body can set only `json`-tagged fields: a
  field that comes from the path, the query or a header keeps that value,
  so a body cannot overwrite the id the route was called with.

The functions behind the methods — `router.BindQuery`, `router.BindPath`,
`router.BindHeaders`, `router.BindRequest` — take an `*http.Request` for code
that is not a handler.

### Answering in the representation the client asked for

`c.Negotiate(code, v)` reads the `Accept` header and answers `v` as JSON, XML
or plain text — one handler for every client:

```go
return c.Negotiate(http.StatusOK, note)
```

The client's quality values decide; between types it accepts equally the
order is JSON, XML, plain text, so `*/*` or no `Accept` at all gets JSON.
Plain text is offered for a value that has one (a string, a number, an
`error`, a `fmt.Stringer`, an `encoding.TextMarshaler`) and XML for a value
`encoding/xml` can encode; when the first choice cannot encode `v`, the next
acceptable type answers. The response carries `Vary: Accept`. When the client
accepts none of them, `Negotiate` writes nothing and returns a 406
`NOT_ACCEPTABLE` error listing the available types — return it and the
client gets it in the error shape below.

### Raw HTML and templates

`c.Render(code, name, data)` renders a template (see
[Server-rendered templates](#server-rendered-templates)); `c.RawHTML(code,
html)` writes a string you already hold, unescaped, as `text/html`.
`nucleus.Context.HTML(code, html)` did the second under the first's name — the
embedded `router.Context.HTML` renders a template — and is deprecated in
favour of `RawHTML`; it keeps working until the next major.

## Errors: one shape

Every error the framework answers has one shape, whoever raised it: a
handler's `*DomainError`, a binding or validation failure, the router's own
404 for a path nobody serves and 405 for a method a path does not take, the
request timeout, and the refusals of the CSRF middleware, the rate limiter,
the authorizer and the bearer middleware. By default that shape is the
envelope:

```json
{"error": {"code": "NOT_FOUND", "message": "no route serves GET /api/nope"}}
```

A client that prefers [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457)
problem details — an `Accept` header that ranks `application/problem+json`
above `application/json` — gets them instead, as `application/problem+json`:

```json
{
  "type": "about:blank",
  "title": "Unprocessable Entity",
  "status": 422,
  "detail": "validation failed",
  "instance": "/api/notes",
  "code": "VALIDATION_FAILED",
  "details": {"title": "this field is required"}
}
```

`code` and `details` are extension members carrying what the envelope
carries: the framework's machine-readable code and, for a validation
failure, the message per field. `instance` is the request path.

An application that wants problem details for every client opts in once:

```go
nucleus.New().WithProblemDetails() // or nucleus.WithProblemDetails() in App.Options
```

The envelope stays the default until the next major, so no existing client
sees its errors change shape. There is no configuration key for this on
purpose: the shape of an API's errors is part of its contract, and the
choice belongs in code.

The router's own 404 and 405 answer in this shape when the client prefers
JSON to HTML and plain text; a browser, or a client that sends `*/*` or no
`Accept`, keeps Go's plain-text `404 page not found`. The 405 keeps its
`Allow` header and lists the methods in `details.allow`. A handler mounted
opaquely with `Router.Mount` (a file server, another router) answers its
own 404s.

Two older shapes are kept as they were in the envelope mode: a
`router.HTTPError` answers `{"error": "<message>"}`, and an unclassified
handler error answers a 500 `{"error": "internal server error"}`. In the
problem mode both are problem details like everything else.

## API versions

A module declares the API version it serves with `Module.Version`; the
framework mounts it under the version's segment and stamps every response
it gives — its 404s included — with the standard headers once the version
is on its way out:

```go
nucleus.Module[struct{}]{
    Name:   "notes_v1",
    Prefix: "/api",
    Version: nucleus.APIVersion{
        Name:       "v1",                                  // served at /api/v1
        Deprecated: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
        Sunset:     time.Date(2027, 4, 1, 0, 0, 0, 0, time.UTC),
        Successor:  "/api/v2/notes",
    },
    Routes: notesV1Routes,
}
```

| Field | Header |
|---|---|
| `Deprecated` | `Deprecation: @<unix seconds>` ([RFC 9745](https://www.rfc-editor.org/rfc/rfc9745)) — a future date announces it |
| `Sunset` | `Sunset: <HTTP date>` ([RFC 8594](https://www.rfc-editor.org/rfc/rfc8594)) |
| `Successor` | `Link: <url>; rel="successor-version"` |
| `Policy` | `Link: <url>; rel="deprecation"` — a page that explains the move |

A version that sets none of them adds no header: the path segment is the
whole declaration. Two versions of an API are two modules with their own
`Routes`. The module's `Prefix()` reports the full mount point (`/api/v1`),
so its `Policies` and `CSRFExempt` are relative to it. A `Name` that is not
one path segment fails boot. The framework does not stop serving a version
after its `Sunset`; removing the routes is your change to make.

Inside a module, `nucleus.Versioned` does the same for a group of routes:

```go
nucleus.Versioned(r, nucleus.APIVersion{Name: "v2"}, func(g nucleus.Router) {
    g.Get("/notes", listNotes) // served at <prefix>/v2/notes
})
```

On a bare router, `Mux.Version(v, fn)` mounts the version and
`APIVersion.Headers()` returns the middleware that stamps the headers.

## Timeouts per route

`request_timeout` (30s by default) bounds every request; past it the client
gets a 503 `TIMEOUT` in the error shape above and the handler's context is
done with `context.DeadlineExceeded`. A route that needs a different limit
says so:

```go
r.With(nucleus.Timeout(2 * time.Minute)).Get("/export", export) // longer than request_timeout
r.With(nucleus.Timeout(2 * time.Second)).Get("/lookup", lookup) // fires first
```

The duration counts from the moment the request reaches the route, and it
moves the request's deadline in both directions: a longer timeout is really
longer — the server's `write_timeout` for that response is moved with it — and
a shorter one fires first. `nucleus.Timeout` also works in a module's
`Middleware`, for every route of the module. With `request_timeout` disabled
or on a `timeout_exempt_paths` prefix, the route's timeout is the request's
only one. WebSocket upgrades and `text/event-stream` requests are never given
a deadline: the timeout buffers the response, and a stream cannot be
buffered.

## Built-in middleware

The default middleware chain (full-stack mode) installs:

| Middleware            | Purpose                                            |
| --------------------- | -------------------------------------------------- |
| Recovery              | Recovers from panics, logs with stack trace.       |
| Request ID            | Generates / propagates an X-Request-ID.            |
| Structured logging    | Emits one `slog` line per request with timing.    |
| OpenTelemetry         | Wraps the handler in an OTel span (when enabled). |
| CORS                  | Configured from `cors_origins` / `cors_allow_credentials`; empty `cors_origins` denies cross-origin (v1.0.0 default). |
| CSRF                  | **Opt-in — off by default.** Set `csrf_enabled: true` to mount it on the default stack, or mount the middleware `router.NewCSRFMiddleware(opts)` returns (it reports a misconfiguration as an error) / `router.WithCSRF` per module. |
| Rate limiting         | Configured from `rate_limit_*` keys. Mounted by the default stack; an application built `WithoutDefaults()` mounts it only with `WithRateLimit()`, and otherwise reports the ignored keys at boot. |
| Request scope         | Resolves multi-site / multi-tenant context.        |

Every auto-mounted middleware can be turned off from configuration, and none
of them rely on hidden state. CSRF is the exception in the other direction:
it is off by default and you turn it on, either with `csrf_enabled: true` or
per module (see [Auth & sessions](../features/auth/index.md) for the module-scoped
pattern).

The order of the auto-mounted middleware is fixed. Handlers can rely on the
request already carrying a logger, a request ID and a span by the time they
run.

The router takes the routing decision on the request *as each middleware
sees it*: `router.Matched(r)` asks the dispatching router whether a
registered pattern serves the request's method and path at that point of
the chain, so a middleware that rewrites the path changes the answer for
everything after it. The default-deny authorizer and the CSRF middleware
use it to let a path nobody serves fall through to the mux's own 404 (405
for a path registered under other methods) instead of answering a 403 or a
419 for a handler that does not exist. Every other built-in middleware —
request ID, CORS, rate limiting, logging, the security headers, the request
interceptors — runs for unknown paths too. A gate of your own makes the
same choice by wrapping itself in `router.WhenMatched`:

```go
r.Use(router.WhenMatched(func(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // ... enforce for a route that exists; an unmatched request never
        // gets here and the 404 answers
        next.ServeHTTP(w, r)
    })
}))
```

A gate that stepped aside for an unmatched request is not forgotten: if a
middleware mounted after it — a request interceptor, say — rewrites the
path onto a registered route, the gate runs anyway, in its mounted order
and on the path as the gate's own level spells it: a gate at the root
judges the full path even when the rewrite happened inside a module's
`Prefix` mount, with the prefix the mount had stripped put back in front —
policy rows and CSRF exemptions are written against the full path, and
`/secret` inside `/api` is `/api/secret` to them, not the root's
`/secret`. A rewrite can therefore never turn a miss into an unguarded
hit: an unregistered alias onto a registered route answers exactly what
the real path answers, 403 without a policy row and 419 for a
state-changing request without a token, at the root or inside a mount.
The handler receives the request as its own level holds it, with the
context the gates added and the form a gate parsed — the CSRF gate reads
the token from the form field of a classic HTML form — so it reads the
same fields through the alias as through the real path.

The decision sees through mounted sub-routers — a module `Prefix`, a
nested `Group`, a `Resource` — so `GET /api/articles/typo` under the
module above is a 404 at the root gate exactly as `GET /typo` is. Only
`Route`, and `Mount` with a `*Mux`, are seen through. A plain
`http.Handler` mounted with `Router.Mount` is opaque — the router cannot
see its routes — and so is a handler registered under a subtree pattern
with `Handle` or `HandleFunc`, a `*Mux` included: every path under such a
prefix counts as matched, so the gates run there and a typo under it
answers 403, not 404.

Outside a router — wrapped around a plain `http.Handler` — there is no
routing decision, and `Matched` reports true so a gate keeps enforcing.

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

An interceptor sees the status a request was answered with, not why. When
it needs the error behind a 500 — the one a handler returned and the
framework could not classify — it wraps the `http.ResponseWriter` it hands
down in one that implements `interceptor.ErrorReporter`, and the framework
calls `ReportError(r, err)` on it before the 500 is written. That is how the
`sentry` catalog entry works; [Error reporting](../features/error-reporting.md)
has the details and a minimal reporter of your own.

## Server-rendered templates

`app.New` loads every `.html` under `templates_dir` (default
`internal/web/templates`) **recursively** at startup and wires the engine
into the router, so handlers render with `Render`:

```go
// In a module handler (nucleus.Context). c.RawHTML writes a string you
// already hold instead, with no template involved.
return c.Render(http.StatusOK, "fieldservice/index.html", data)
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
`c.Render(status, "<module-name>/<path>", data)`:

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
                return c.Render(http.StatusOK, "shop/index.html", nil)
            })
        },
    }.Build()
}
```

## The OpenAPI document

An application serves the OpenAPI 3.1 document of its API with one line in
the composition root. The document is **derived from the application**, not
written next to it:

```go
nucleus.New().
	FromConfigFile("nucleus.yml").
	WithOpenAPIDocument("/openapi.json").
	Mount(shop.Module(client)).
	Start()
```

What the document says, and where each part comes from:

- **Paths** — every route a module registers through its `nucleus.Router`:
  `Get`/`Post`/…, routes inside `Group` and `With`, and each verb of a
  `Resource`, at the full path the router serves them (module `Prefix`
  applied). The module's name is the operation's tag; the operationId is
  the handler's name (`listArticles` for the method value
  `m.listArticles`; `indexTickets`, `showTickets` for a resource), or the
  method and path for an anonymous function. A subtree mounted with
  `Mount` (an admin panel, a file server) is not described: the framework
  does not route inside it.
- **Path parameters** — from the pattern: `{id}` is a required string
  parameter.
- **Security** — from what the application enforces. When it verifies JWTs
  and the default-deny authorizer is on, the document declares the
  `bearerAuth` scheme as the default, and every operation the policy opens
  to anonymous callers (a module's `Policies` row for `anonymous`, a row in
  `rbac_policy.csv`) carries an explicit empty `security`: the document
  says which calls need a token because the enforcer was asked. Under
  `WithOpenAuthz` it declares no security at all.
- **Responses** — a plain handler (`func(*nucleus.Context) error`) does not
  tell the framework what it writes, and the document says so instead of
  guessing.

The route is readable without credentials even under default-deny (a client
generator or a gateway fetches it first); an operator `deny` row for the
pattern closes it again. `nucleus openapi` exports the same document: it
boots the application without listening, the way `nucleus routes` does, and
reads it back.

### Typed endpoints

A plain handler, `func(*nucleus.Context) error`, says nothing about what it
reads or writes, so the document can only list its path. A typed endpoint
says both in its signature, and the framework binds, validates, writes and
documents from it:

```go
type CreateArticle struct {
	AuthorID int64  `json:"author_id" validate:"required"`
	Title    string `json:"title" validate:"required,max=200"`
}

type ArticleFilter struct {
	AuthorID int64 `query:"author_id"`
}

func (m *module) createArticle(c *nucleus.Context, in CreateArticle) (Article, error) { … }
func (m *module) listArticles(c *nucleus.Context, f ArticleFilter) (ArticleList, error) { … }

Routes: func(r nucleus.Router, _ struct{}) {
	nucleus.Handle(r, http.MethodGet, "/api/articles", m.listArticles)
	nucleus.Handle(r, http.MethodPost, "/api/articles", m.createArticle, nucleus.Status(http.StatusCreated))
},
```

The input is bound with `c.BindRequest` — the `path`, `query` and `header`
tagged fields from those, the rest from the JSON body — and validated before
the function runs; the output is written as JSON with the success status
(`Status`, 200 by default; an output of `struct{}` answers 204). An error goes
where a plain handler's error goes. In the document the operation gets its
parameters, its request body and its response from the two types, and the
operationId from the function's name. `Summary` and `Description` set the
operation's texts. A route that is not typed stays in the document by path,
with a response that says it is not described.

### Schemas from Go structs

`openapi.SchemaOf[T](doc)` writes the schema of a Go type as
`encoding/json` writes the value, registers every named struct it reaches
under `components.schemas` and returns a `$ref`:

```go
type Article struct {
	ID     int64     `json:"id"`
	Title  string    `json:"title" validate:"required,max=200"`
	Body   string    `json:"body,omitempty"`
	Status string    `json:"status" validate:"oneof=draft published"`
	At     time.Time `json:"at"`
}

ref := openapi.SchemaOf[Article](doc) // {"$ref": "#/components/schemas/Article"}
```

The property names are the `json` tags (the Go field name without one);
`json:"-"` and unexported fields are left out and embedded structs are
flattened. A field is required unless it is `omitempty`, a pointer, or
`validate:"omitempty"`; `validate:"required"` makes it required either way.
`validate` rules become constraints (`min`/`max`/`len`, `gt`/`gte`/`lt`/`lte`,
`oneof` as `enum`, `email`, `url`, `uuid`); a `doc:"…"` tag becomes the
description. `time.Time` is a `date-time` string, a pointer is nullable, a
type that implements `json.Marshaler` gets the empty schema (its output is
not derivable from its fields).

### Enforcing the document

`WithOpenAPIValidation()` makes the document a contract the application
enforces. Every request to a module route is checked against the operation
the document declares for it before the handler runs: path, query and header
parameters (converted to their declared types first) and the JSON body. A
request that departs is answered `400` with code `INVALID_REQUEST` and one
entry per departure, each naming its field (`query.page`, `/title`). The
validator is written in Go against the subset of JSON Schema this package
models — types (with `null`), `enum`, `properties`/`required`/
`additionalProperties`, `items`, numeric and length bounds, `pattern`, item
counts, `$ref`, and the formats `SchemaOf` writes.

```go
nucleus.New().
	WithOpenAPIDocument("/openapi.json", contracts.NewDocument()).
	WithOpenAPIValidation()
```

### Keeping the contract

`nucleus openapi --check <baseline.json>` compares the application's document
with a previous export and fails naming every change that breaks a client
written against it: an operation removed; a request that must say more (a
new required parameter or field, a narrower type, a tighter bound, an enum
value no longer accepted); a response that says less (a field removed or no
longer always present, a changed type, a success status removed, a new enum
value); an operation that was public and now asks for credentials. Additions
pass. Commit the export, and run the check in CI:

```bash
nucleus openapi --out api/openapi.baseline.json   # once, and whenever you mean to change the contract
nucleus openapi --check api/openapi.baseline.json  # in CI
```

The comparison is `openapi.BreakingChanges(prev, next)`, for a check of your
own.

### A client from the document

`nucleus openapi --client typescript` writes a TypeScript client from the
document the application serves: one file, no dependency, over `fetch` (a
browser, Node 18+, Deno). It declares a type for every schema and a `Client`
with one method per operation, named after its operationId and typed by its
path parameters, its body, its query and header parameters and its success
response; an answer outside 2xx throws `ApiError`, with the status and the
code read from the envelope or from problem details.

```bash
nucleus openapi --client typescript --out web/src/api.ts          # from the application
nucleus openapi --document openapi.json --client typescript       # from a saved document
```

```ts
import { Client, ApiError } from "./api.ts";

const api = new Client({ baseUrl: "http://localhost:8080", token: () => session.token });
const created = await api.createArticle({ author_id: 1, title: "Hello" }); // Article
try {
  await api.createArticle({ author_id: 1, title: "Hello" });
} catch (err) {
  if (err instanceof ApiError && err.status === 409) { /* taken */ }
}
```

The suite starter's own CI lane generates this client from the starter and
runs a script that uses nothing else against it, with `tsc --strict` first.

### A hand-written contract as the base

A project generated with `nucleus generate resource` keeps a hand-written
contract in `internal/contracts`. Pass it as the base and the application
serves one document:

```go
WithOpenAPIDocument("/openapi.json", contracts.NewDocument())
```

What the base declares is kept as written — an operation, a schema, a
security scheme, the `info` block. The derivation adds the routes the base
does not mention and fills, on the operations it does, what it left out:
the operationId, the tag and the security the policy enforces.

### Mounting a document you build yourself

`WithOpenAPIHandler` (and `app.App.MountOpenAPIHandler`) still mount any
`http.Handler` — `openapi.Handler(provider)` for a document you assemble
entirely by hand. It cannot share a pattern with `WithOpenAPIDocument`; the
boot fails naming the pattern instead of serving one of the two.

```go
import "github.com/jcsvwinston/nucleus/pkg/openapi"

if err := a.MountOpenAPIHandler("/api/openapi.json", openapi.Handler(func() *openapi.Document { return myDoc })); err != nil {
	log.Fatal(err)
}
```
