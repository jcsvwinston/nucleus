---
sidebar_position: 4
title: RBAC & the middleware chain
covers:
  - pkg/authz.New
  - pkg/authz.NewFromModel
  - pkg/authz.Enforcer
  - pkg/authz.Enforcer.Can
  - pkg/authz.Enforcer.AddPolicy
  - pkg/authz.Enforcer.Deny
  - pkg/authz.Enforcer.RemovePolicy
  - pkg/authz.Enforcer.RequireRole
  - pkg/authz.Enforcer.AddRole
  - pkg/authz.Enforcer.RemoveRole
  - pkg/authz.Enforcer.GetRoles
  - pkg/authz.Enforcer.GetPolicy
  - pkg/authz.Enforcer.GetGroupingPolicy
  - pkg/authz.Enforcer.GetAllRoles
  - pkg/authz.Enforcer.MiddlewareWithOptions
  - pkg/authz.Enforcer.RequireRoleWithOptions
  - pkg/authz.AuthzOptions
  - pkg/authz.AuthzOptions.ResolveSubject
  - pkg/authz.AuthzOptions.ResolveAction
  - pkg/authz.SubjectResolver
  - pkg/authz.ActionResolver
  - pkg/authz.DenialHandler
  - pkg/authz.Denial
  - pkg/auth.ContextWithClaims
  - pkg/auth.ClaimsFromContext
  - pkg/auth.Claims
  - pkg/nucleus.Runtime.Authorizer
config_keys:
  - rbac_policy_file
---

# RBAC & the middleware chain

`pkg/authz` integrates Casbin behind a wrapped `Enforcer`. This page opens
with the wall everyone hits in their first hour — the 403 from the
default-deny gate — and then covers the policy model in full.

## Your first 403

You added a session-authenticated route, signed in, and the route answers
**403**. Nothing is broken; two facts explain it:

1. **The global gate fires first.** The framework mounts a global
   default-deny authorizer as the last item in the core middleware chain,
   before any module routes are registered. Your module's middleware
   attaches later, inside the module's own sub-router — so the global gate
   always evaluates **before** anything declared in `Module[C].Middleware`.
2. **The gate sees no identity.** When it evaluates the request, no
   `auth.Claims` are in the context yet (your session bridge has not run).
   The enforcer looks for a subject, finds none, treats the request as
   `anonymous`, and denies it — unless a policy row explicitly permits
   anonymous access to that path.

**A session-identity bridge placed in `Module.Middleware` cannot influence
the global gate.** There is no pre-authz identity hook today, and none is
promised for a future version. The fix is not more middleware — it is two
layers doing two different jobs:

### The correct two-layer pattern

1. **Operator grants reachability** — add policy rows in the
   `rbac_policy_file` that permit the `anonymous` subject (or a
   named bootstrap subject) to reach the module's URL prefix. The
   global default-deny gate will then let those requests through.

   ```csv
   # auth/policy.csv — grant anonymous access to the /auth/* paths
   p, anonymous, /auth/login,  create, allow
   p, anonymous, /auth/logout, create, allow
   ```

   For entirely private surfaces where only authenticated users
   should ever reach (e.g. `/api/admin/*`), the operator grants
   access to the specific roles instead:

   ```csv
   p, admin, /api/admin,  *, allow
   p, admin, /api/admin/*, *, allow
   ```

2. **Module enforces identity and roles** — after the global gate passes
   the request, the module's own middleware chain runs. Place a
   session-to-claims bridge first, then a role guard:

   ```go
   // adminModule holds framework handles captured in OnStart.
   type adminModule struct {
       rt nucleus.Runtime
   }

   func (m *adminModule) build() nucleus.ModuleSpec {
       return nucleus.Module[struct{}]{
           Name:   "admin",
           Prefix: "/api/admin",
           // Module.Middleware entries are constructed before OnStart,
           // so they must close over the module struct, not rt directly.
           Middleware: []nucleus.Middleware{
               m.withIdentity,
               m.requireRole("admin"),
           },
           OnStart: func(ctx context.Context, rt nucleus.Runtime, _ struct{}) error {
               m.rt = rt // capture the runtime for per-request use
               return nil
           },
           Routes: func(r nucleus.Router, _ struct{}) {
               r.Get("/stats", adminStats)
           },
       }.Build()
   }

   // withIdentity reads the session-authenticated user ID and role,
   // builds auth.Claims, and injects them so that downstream
   // middleware and handlers can read the subject.
   func (m *adminModule) withIdentity(next http.Handler) http.Handler {
       return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
           sm := m.rt.Session() // *auth.SessionManager
           if sm == nil {
               http.Error(w, "unauthenticated", http.StatusUnauthorized)
               return
           }
           userID := sm.GetString(r.Context(), "user_id")
           if userID == "" {
               http.Error(w, "unauthenticated", http.StatusUnauthorized)
               return
           }
           role := sm.GetString(r.Context(), "role") // stored at login
           ctx := auth.ContextWithClaims(r.Context(), &auth.Claims{
               UserID: userID,
               Role:   role,
           })
           next.ServeHTTP(w, r.WithContext(ctx))
       })
   }

   // requireRole returns middleware that gates the request on the
   // claims injected by withIdentity. It delegates to the runtime
   // enforcer per request so that live policy changes are respected.
   func (m *adminModule) requireRole(roles ...string) nucleus.Middleware {
       return func(next http.Handler) http.Handler {
           return m.rt.Authorizer().RequireRole(roles...)(next)
       }
   }
   ```

   `Module.Middleware` entries are evaluated in registration order
   within the module's sub-mux. `withIdentity` must appear before
   `requireRole` so that `RequireRole` finds claims in the context.

`auth.ContextWithClaims` also propagates the user ID for log
attribution (`observe.CtxWithUserID` is called internally), so
structured logs for the request automatically carry the subject
without extra instrumentation.

The login handler that writes `user_id` and `role` into the session is the
other half of this pattern — see
[Your first login](./your-first-login.md).

## Module-declared policy rows (`Module.Policies`)

A module can carry the policy rows its own routes need, so mounting it
works without the operator editing `rbac_policy.file` by hand:

```go
nucleus.Module[struct{}]{
    Name: "articles",
    Policies: []nucleus.PolicyRule{
        // Same shape as a rbac_policy.csv row: subject, object, action, effect.
        {Subject: "anonymous", Object: "/articles", Action: "read"},
        {Subject: "anonymous", Object: "/articles", Action: "create"},
        {Subject: "anonymous", Object: "/articles/*", Action: "read"},
    },
    // JSON API paths the module needs exempted from CSRF protection.
    // Same raw-prefix matching as csrf_exempt_paths.
    CSRFExempt: []string{"/articles"},
    Routes: func(r nucleus.Router, _ struct{}) { /* … */ },
}
```

The contract, precisely:

- **Objects are relative to the module's `Prefix`** (a module without a
  prefix declares full paths); keyMatch wildcards work (`/articles/*`).
- **To declare the module's own root**, use `Object: "/"` — it means the
  root *and* the subtree, and emits both rows. Use `Object: ""` for the
  root alone, when the landing page is public but what hangs off it is
  not. The distinction exists because the enforcer matches with keyMatch,
  where `/articles` and `/articles/` are different paths and neither
  implies the other.
- **`Action` is a CRUD verb** (`read`|`create`|`update`|`delete`) or
  `*` — not an HTTP method. `Effect` is `allow` (the default when
  empty) or `deny`.
- **The operator always wins.** Module rows join the live in-memory
  ruleset only — the policy file is never written — and the Casbin
  policy effect (`some(allow) && !some(deny)`) means a `deny` row in
  the host's CSV overrides any module `allow`.
- **Malformed declarations fail boot** with `ErrInvalidModulePolicy`, naming
  the module and the entry. A row that was silently skipped would leave its
  route answering a mute 403.
- `CSRFExempt` entries are declarative (not a closure) because the
  exemption list is frozen inside the middleware stack at `app.New`,
  before any module closure runs — the same constraint behind the
  automatic webhook-prefix exemption. `"/"` (or `""`) means the module's
  own surface and resolves to the bare prefix, which the raw-prefix
  matcher extends over everything below it — including a POST to the
  collection path itself.
- **A module cannot exempt the whole application.** An exemption that
  resolves to `"/"` — which is what a module *without* a `Prefix`
  declaring `"/"` would mean — fails boot. Mounting a module is trusting
  its routes; it is not agreeing to let it unprotect its siblings.
- **Exemptions are logged at boot**, module by module, with the resolved
  paths. They are the one declaration that *removes* a protection, so they
  leave the loudest trace.

Rows that depend on the module's bound config can still be added
imperatively from `OnStart` via `rt.Authorizer().AddPolicy(...)`; the
declarative field is for the common case and gets boot-time validation.

## RBAC

Provide a policy file, and the framework loads an enforcer accessible from
the application:

```yaml
rbac_policy_file: ./auth/policy.csv
```

```go
allowed := a.Authorizer.Can(userID, "articles", "edit") // returns bool
```

The enforcer is available to all extension modules. A superuser bypass is
built in for the bootstrap case.

### Default-deny with deny-override

The built-in Casbin model is **default-deny with deny-override
semantics**:

- A request with **no matching policy** is denied. Operators must
  grant access explicitly.
- A request matching an **allow** rule is permitted — unless a
  matching **deny** rule also exists, in which case it is denied.
  Deny rules always override allows.

The programmatic API mirrors that:

```go
// Grant a role full access to an API surface.
e.AddPolicy("admin", "/api/*", "*")

// Block a specific user from one endpoint, even though their role
// would otherwise allow it.
e.Deny("alice", "/api/users/1", "delete")

// RemovePolicy lifts BOTH the allow and the deny variants matching
// (sub, obj, act) — operators say "stop applying this rule" without
// having to know which effect was originally written.
e.RemovePolicy("alice", "/api/users/1", "delete")
```

CSV policy files carry an `eft` column, so a row reads
`p, <subject>, <object>, <action>, <effect>` where the effect is `allow` or
`deny`. From code, `AddPolicy` stamps `allow` and `Deny` writes the deny
variant.

Casbin itself is an implementation detail of `authz.Enforcer`. Its concrete
type is not part of the public API and callers cannot reach it, which is what
keeps Casbin replaceable without breaking your code.

### Reading policy state

Three read-only forwarders expose the live ruleset without requiring
access to the underlying Casbin implementation:

```go
// All permission rules as (sub, obj, act, eft) tuples.
rules, err := e.GetPolicy()

// All role-assignment rules as (user, role) tuples.
groupings, err := e.GetGroupingPolicy()

// All role names referenced by a grouping policy.
roles, err := e.GetAllRoles()
```

These are available to application code that needs to audit the live
policy (e.g. for display in a custom UI or an audit log export).

### SSR-friendly denial handling

By default, `Middleware()` and `RequireRole(...)` write a JSON error envelope
on denial — 401 or 403. That is the right answer for an API, but not for a
server-rendered site, where an anonymous visitor should be redirected to a
login page and a signed-in user lacking the role should see a styled error
page.

To replace the default behaviour, use `MiddlewareWithOptions` and
`RequireRoleWithOptions`.

```go
import (
    "net/http"
    "github.com/jcsvwinston/nucleus/pkg/authz"
)

onDeny := authz.DenialHandler(func(w http.ResponseWriter, r *http.Request, d authz.Denial) {
    if !d.Authenticated {
        // Anonymous visitor — redirect to login.
        http.Redirect(w, r, "/auth/login", http.StatusFound)
        return
    }
    // Signed-in user without the required role — show a 403 page.
    http.Error(w, "Access denied", http.StatusForbidden)
})

// Global policy gate — SSR variant.
router.Use(enforcer.MiddlewareWithOptions(authz.AuthzOptions{OnDeny: onDeny}))

// Role guard on a single route — SSR variant.
router.With(enforcer.RequireRoleWithOptions(authz.AuthzOptions{OnDeny: onDeny}, "admin")).
    Get("/admin/dashboard", adminDashboard)
```

`authz.Denial` carries three fields set by the middleware before
calling `OnDeny`:

| Field           | Type   | Meaning                                                                                    |
| --------------- | ------ | ------------------------------------------------------------------------------------------ |
| `Status`        | `int`  | HTTP status the default path would use: 401 (no identity) or 403 (insufficient role).     |
| `Authenticated` | `bool` | `false` when the visitor is anonymous; `true` when signed in but lacking role/permission. |
| `Reason`        | `string` | Human-readable explanation, e.g. `"insufficient role"`.                                 |

The `OnDeny` handler owns the response — it must write a status and body
and must not call the next handler. Passing a nil `OnDeny` (the zero
`AuthzOptions`) reproduces the default JSON envelope exactly, so existing
callers are unaffected.

### Subject and action resolvers

`AuthzOptions` carries two additional optional fields that change _what_
the middleware checks, rather than just how it responds on denial.

**`ResolveSubject`** (`authz.SubjectResolver`) overrides the policy subject.

Its signature is `func(r *http.Request, claims *auth.Claims) string`, and the
default returns `claims.UserID`. If your policy table is keyed by role rather
than by individual user, return `claims.Role` instead — the enforcer then
looks the role up directly in the policy CSV.

**`ResolveAction`** (`authz.ActionResolver`) overrides the policy action.

Its signature is `func(r *http.Request) string`, and the default maps the HTTP
method: GET→`read`, POST→`create`, PUT/PATCH→`update`, DELETE→`delete`.
Pure-HTML forms can only POST, so a resolver can inspect the URL path and map
a POST ending in `/delete` to the `delete` action, making deny-override rules
fire correctly.

Both resolvers default to `nil`, which is the standard behaviour — callers
that set only `OnDeny` are unaffected.

Here is a combined server-rendered setup: role-keyed policies, path-aware
action mapping, and redirect-on-denial.

```go
import (
    "net/http"
    "strings"
    "github.com/jcsvwinston/nucleus/pkg/auth"
    "github.com/jcsvwinston/nucleus/pkg/authz"
)

opts := authz.AuthzOptions{
    // Check policies keyed by role, not by individual user ID.
    ResolveSubject: authz.SubjectResolver(func(r *http.Request, c *auth.Claims) string {
        return c.Role
    }),
    // Map pure-HTML form POSTs to the correct action.
    // Default: GET/HEAD→"read", POST→"create", PUT/PATCH→"update", DELETE→"delete".
    ResolveAction: authz.ActionResolver(func(r *http.Request) string {
        if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/delete") {
            return "delete"
        }
        return "create" // treat other POSTs as create
    }),
    // Redirect anonymous visitors; show a styled page for signed-in denials.
    OnDeny: authz.DenialHandler(func(w http.ResponseWriter, r *http.Request, d authz.Denial) {
        if !d.Authenticated {
            http.Redirect(w, r, "/auth/login", http.StatusFound)
            return
        }
        http.Error(w, "Access denied", http.StatusForbidden)
    }),
}

router.Use(enforcer.MiddlewareWithOptions(opts))
```

Two limits are worth knowing. `ResolveSubject` is honoured by
`MiddlewareWithOptions` only — `RequireRole` and `RequireRoleWithOptions`
match the claim's role directly and ignore it. And a resolver that returns an
empty string produces a policy query matching nothing: the request is denied
under default-deny, and a warning is logged so the misconfiguration is
auditable.

## Applications built directly on `pkg/app`

Applications assembled with `pkg/app` rather than `pkg/nucleus` compose the
same middleware on the Mux. The session middleware is already mounted
globally; add it to a sub-route only if you are deliberately replacing the
global mount with a scoped one. It must never be mounted twice on the same
request path.

```go
// pkg/app-level wiring (not module code)
a.Router.Mux.Route("/api/admin", func(sub *router.Mux) {
    sub.Use(sessionIdentityMiddleware)
    sub.Use(a.Authorizer.RequireRole("admin"))
    // ...
})
```
