package nucleus

import (
	"fmt"
	"net/http"
	"strings"

	routerpkg "github.com/jcsvwinston/nucleus/pkg/router"
)

// Handler is the framework's handler signature. Modules and ad-hoc
// route registrations both produce values of this type. The framework
// adapts Handler to the underlying `*router.Router` Handler shape at
// registration time.
type Handler func(*Context) error

// Router is the routing surface a module receives via
// `ModuleSpec.Routes(r Router)`. It is defined in `pkg/nucleus` (not as
// an alias for `*router.Router`) so that modules do not take a hard
// import on `pkg/router`. The framework constructs the implementation
// in `Start()`; module code never instantiates a `Router` itself.
//
// Three coexisting styles are supported per ADR-010 §7:
//
//   - Flat declarative: `r.Get("/articles", ListArticles)` — small or
//     audit-sensitive modules.
//   - REST resource: `r.Resource("/tickets", controller{}, nucleus.Methods(Index, Show, Create))`
//     — CRUD modules. Methods to register are passed explicitly via a
//     variadic argument; the framework does not discover methods via
//     reflection. Adding a `Patch` method to the controller does not
//     silently register a PATCH route.
//   - Nested groups: `r.Group("/admin", func(g Router) { ... })` — areas
//     with nested URL hierarchy and inherited middleware. Middleware
//     composes additively at every group level.
//
// Mixing the three styles within the same module is supported.
type Router interface {
	Get(path string, handlers ...Handler)
	Post(path string, handlers ...Handler)
	Put(path string, handlers ...Handler)
	Patch(path string, handlers ...Handler)
	Delete(path string, handlers ...Handler)
	Group(prefix string, fn func(g Router))
	Resource(path string, controller any, methods MethodSet)

	// With returns a Router that applies the given middleware to every route
	// registered on the returned value, WITHOUT affecting routes registered on
	// the parent — the per-route / per-scope counterpart to a module's global
	// Middleware. Chain it before a single route to guard just that endpoint:
	//
	//	r.With(rt.Authorizer().RequireRole("admin")).Get("/billing", billing)
	//
	// Middleware is `func(http.Handler) http.Handler`, so any standard net/http
	// middleware — the framework's `Enforcer.RequireRole`, `router.CSRFMiddleware`,
	// or a hand-written guard — mounts directly, with no adapter. With composes
	// additively: each nested With / Group layer adds to the chain (outer→inner),
	// on top of any module-level Middleware.
	With(mw ...Middleware) Router

	// Mount attaches a standard http.Handler subtree at pattern (joined to the
	// module's prefix). Everything under pattern is delegated to h — use it to
	// mount a self-contained sub-application whose internal routing the framework
	// should not interpret: an admin panel's own router (e.g. orbit), a
	// static-file server, or any third-party http.Handler. Unlike Get/Post/…,
	// which register a single endpoint, Mount owns the whole subtree below
	// pattern. A request for the bare pattern without a trailing slash (e.g.
	// GET /admin) is 307-redirected to the canonical pattern/ (GET /admin/).
	// Module-level and With/Group middleware still wrap the mounted handler.
	Mount(pattern string, h http.Handler)
}

// ResourceMethod identifies a REST verb to register on a Resource.
// Callers compose a `MethodSet` via `nucleus.Methods(...)` and pass it
// as the third argument of `Router.Resource`. The framework asserts
// the controller satisfies the corresponding sub-interface (Indexer,
// Shower, Creator, Updater, Patcher, Destroyer) for each requested
// method, and registers only the requested verbs. Registration is
// auditable: a quick read of the call site shows which routes a
// controller exposes.
type ResourceMethod int

const (
	// Index registers GET /resource backed by the Indexer sub-interface.
	Index ResourceMethod = iota
	// Show registers GET /resource/{id} backed by the Shower sub-interface.
	Show
	// Create registers POST /resource backed by the Creator sub-interface.
	Create
	// Update registers PUT /resource/{id} backed by the Updater sub-interface.
	Update
	// Patch registers PATCH /resource/{id} backed by the Patcher sub-interface.
	Patch
	// Destroy registers DELETE /resource/{id} backed by the Destroyer sub-interface.
	Destroy
)

// MethodSet is the value type produced by `Methods(...)`. It carries
// the list of REST methods a Resource registration should expose. The
// framework treats the set as an unordered collection; ordering of
// arguments to `Methods()` does not affect routing.
type MethodSet struct {
	methods []ResourceMethod
}

// Methods constructs a MethodSet from the supplied REST verbs. The
// canonical usage is at the call site of `Router.Resource`:
//
//	r.Resource("/tickets", ticketsController{}, nucleus.Methods(
//	    nucleus.Index, nucleus.Show, nucleus.Create,
//	))
func Methods(ms ...ResourceMethod) MethodSet {
	out := MethodSet{methods: make([]ResourceMethod, len(ms))}
	copy(out.methods, ms)
	return out
}

// Has reports whether the given method is in the set.
func (s MethodSet) Has(m ResourceMethod) bool {
	for _, x := range s.methods {
		if x == m {
			return true
		}
	}
	return false
}

// REST sub-interfaces. Each Resource verb is backed by a one-method
// interface; the framework type-asserts the controller against the
// sub-interfaces for the requested methods only. A controller may
// implement any subset of the six interfaces.

// Indexer handles GET /resource.
type Indexer interface{ Index(*Context) error }

// Shower handles GET /resource/{id}.
type Shower interface{ Show(*Context) error }

// Creator handles POST /resource.
type Creator interface{ Create(*Context) error }

// Updater handles PUT /resource/{id}.
type Updater interface{ Update(*Context) error }

// Patcher handles PATCH /resource/{id}.
type Patcher interface{ Patch(*Context) error }

// Destroyer handles DELETE /resource/{id}.
type Destroyer interface{ Destroy(*Context) error }

// routerAdapter is the concrete `Router` implementation produced by
// the framework's startup sequence. It wraps a `*routerpkg.Router` (or
// a sub-Mux produced by `Group`) and a path prefix; handler
// registrations join the prefix to the supplied path. The adapter is
// unexported because module code only ever sees the `Router`
// interface.
type routerAdapter struct {
	mux    *routerpkg.Mux
	prefix string
}

func newRouterAdapter(r *routerpkg.Router, prefix string) *routerAdapter {
	return &routerAdapter{mux: r.Mux, prefix: prefix}
}

func newRouterAdapterFromMux(m *routerpkg.Mux, prefix string) *routerAdapter {
	return &routerAdapter{mux: m, prefix: prefix}
}

// joinPath joins the adapter's prefix to a route path. It must never return
// an empty string: an empty pattern reaches net/http.ServeMux.Handle as
// "GET " (method + space + empty path) and panics the mux at startup — the
// footgun documented in examples/mvc_api/internal/notes/module.go. Any empty
// result is therefore floored to "/". Non-empty joins collapse accidental
// double slashes (e.g. prefix "/api/" + path "/notes" -> "/api/notes") so a
// "//" pattern can never reach the mux either.
func (a *routerAdapter) joinPath(p string) string {
	if a.prefix == "" {
		if p == "" {
			return "/"
		}
		return p
	}
	// prefix is non-empty here.
	if p == "" || p == "/" {
		return a.prefix
	}
	joined := strings.TrimRight(a.prefix, "/") + "/" + strings.TrimLeft(p, "/")
	if joined == "" {
		return "/"
	}
	return joined
}

func adaptHandler(h Handler) routerpkg.Handler {
	return func(c *routerpkg.Context) error {
		return h(&Context{Context: c})
	}
}

func adaptHandlers(hs []Handler) []routerpkg.Handler {
	out := make([]routerpkg.Handler, len(hs))
	for i, h := range hs {
		out[i] = adaptHandler(h)
	}
	return out
}

func (a *routerAdapter) Get(path string, handlers ...Handler) {
	a.mux.Get(a.joinPath(path), adaptHandlers(handlers)...)
}

func (a *routerAdapter) Post(path string, handlers ...Handler) {
	a.mux.Post(a.joinPath(path), adaptHandlers(handlers)...)
}

func (a *routerAdapter) Put(path string, handlers ...Handler) {
	a.mux.Put(a.joinPath(path), adaptHandlers(handlers)...)
}

func (a *routerAdapter) Patch(path string, handlers ...Handler) {
	a.mux.Patch(a.joinPath(path), adaptHandlers(handlers)...)
}

func (a *routerAdapter) Delete(path string, handlers ...Handler) {
	a.mux.Delete(a.joinPath(path), adaptHandlers(handlers)...)
}

func (a *routerAdapter) Group(prefix string, fn func(g Router)) {
	joined := a.joinPath(prefix)
	a.mux.Route(joined, func(sub *routerpkg.Mux) {
		fn(newRouterAdapterFromMux(sub, ""))
	})
}

// With delegates to routerpkg.Mux.With: an inline sub-Mux that SHARES the
// parent's ServeMux (unlike Group, which mounts a sub-tree) but carries its own
// middleware chain, so the guard applies to every registration on the returned
// adapter — Get/Post/… and Resource alike — with no URL-prefix change. The
// prefix is preserved. nucleus.Middleware and routerpkg.Middleware are the same
// func(http.Handler) http.Handler alias, so the spread needs no conversion.
func (a *routerAdapter) With(mw ...Middleware) Router {
	return &routerAdapter{mux: a.mux.With(mw...), prefix: a.prefix}
}

// Mount delegates to routerpkg.Mux.Mount, attaching the http.Handler subtree at
// the prefix-joined pattern — the same mechanism the framework uses internally
// to mount the admin panel. joinPath applies the module prefix and floors an
// empty pattern to "/", consistent with Get/Post/….
func (a *routerAdapter) Mount(pattern string, h http.Handler) {
	a.mux.Mount(a.joinPath(pattern), h)
}

func (a *routerAdapter) Resource(path string, controller any, methods MethodSet) {
	base := a.joinPath(path)
	// Defense in depth alongside joinPath: a Resource("") at a module root
	// (empty prefix) must mount the collection at "/" rather than register
	// the empty pattern "GET ", which panics net/http.ServeMux at startup.
	// See examples/mvc_api/internal/notes/module.go for the documented footgun.
	if base == "" {
		base = "/"
	}
	item := strings.TrimRight(base, "/") + "/{id}"

	// Each requested verb must be backed by the matching sub-interface
	// on the controller. A missing implementation is a programming
	// error — silently skipping the registration would produce a 404
	// at the first request with no indication of the cause. Failing
	// loud at registration time mirrors the spirit of ADR-010 §202
	// ("silently registering whatever methods happen to be present
	// on the controller is a footgun") and keeps the audit trail in
	// the startup log rather than in a future bug report.

	if methods.Has(Index) {
		c, ok := controller.(Indexer)
		if !ok {
			panic(missingResourceMethodError(path, "Index", "Indexer"))
		}
		a.mux.Get(base, adaptHandler(c.Index))
	}
	if methods.Has(Show) {
		c, ok := controller.(Shower)
		if !ok {
			panic(missingResourceMethodError(path, "Show", "Shower"))
		}
		a.mux.Get(item, adaptHandler(c.Show))
	}
	if methods.Has(Create) {
		c, ok := controller.(Creator)
		if !ok {
			panic(missingResourceMethodError(path, "Create", "Creator"))
		}
		a.mux.Post(base, adaptHandler(c.Create))
	}
	if methods.Has(Update) {
		c, ok := controller.(Updater)
		if !ok {
			panic(missingResourceMethodError(path, "Update", "Updater"))
		}
		a.mux.Put(item, adaptHandler(c.Update))
	}
	if methods.Has(Patch) {
		c, ok := controller.(Patcher)
		if !ok {
			panic(missingResourceMethodError(path, "Patch", "Patcher"))
		}
		a.mux.Patch(item, adaptHandler(c.Patch))
	}
	if methods.Has(Destroy) {
		c, ok := controller.(Destroyer)
		if !ok {
			panic(missingResourceMethodError(path, "Destroy", "Destroyer"))
		}
		a.mux.Delete(item, adaptHandler(c.Destroy))
	}
}

func missingResourceMethodError(path, verb, iface string) error {
	return fmt.Errorf("nucleus: Router.Resource(%q): nucleus.%s requested but controller does not implement nucleus.%s", path, verb, iface)
}
