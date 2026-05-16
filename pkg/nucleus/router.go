package nucleus

import (
	"fmt"
	"net/http"

	routerpkg "github.com/jcsvwinston/nucleus/pkg/router"
)

// Handler is the simplified per-request handler signature used by every
// nucleus route. Returning an error is treated as a 500 by the router
// renderer unless the handler itself wrote a response. See pkg/router
// for the underlying error-rendering behaviour.
type Handler func(*Context) error

// Middleware is the standard net/http middleware shape. It is a type
// alias rather than a defined type so that callers can pass plain
// `func(http.Handler) http.Handler` values without explicit conversion.
type Middleware = func(http.Handler) http.Handler

// Method identifies a REST verb for resource controllers. Used by
// Router.Resource to explicitly declare which handlers the framework
// should mount — silent presence-based discovery is rejected by
// design (ADR-010 §7).
type Method int

const (
	// Index maps to GET / on the resource prefix.
	Index Method = iota + 1
	// Show maps to GET /{id} on the resource prefix.
	Show
	// Create maps to POST / on the resource prefix.
	Create
	// Update maps to PUT /{id} on the resource prefix.
	Update
	// Destroy maps to DELETE /{id} on the resource prefix.
	Destroy
)

// Methods is a small helper that returns its variadic Method arguments
// as a slice. The single intended caller is Router.Resource:
//
//	r.Resource("/tickets", controller{}, nucleus.Methods(nucleus.Index, nucleus.Show))
//
// Using a helper rather than a bare slice literal keeps the call site
// readable and lets the package upgrade the underlying representation
// (currently `[]Method`) without breaking callers.
func Methods(ms ...Method) []Method {
	out := make([]Method, len(ms))
	copy(out, ms)
	return out
}

// Lister is implemented by controllers that handle Method Index.
type Lister interface {
	Index(c *Context) error
}

// Shower is implemented by controllers that handle Method Show.
type Shower interface {
	Show(c *Context) error
}

// Creator is implemented by controllers that handle Method Create.
type Creator interface {
	Create(c *Context) error
}

// Updater is implemented by controllers that handle Method Update.
type Updater interface {
	Update(c *Context) error
}

// Destroyer is implemented by controllers that handle Method Destroy.
type Destroyer interface {
	Destroy(c *Context) error
}

// Router is the routing surface exposed to module Routes() callbacks and
// to direct registration on AppBuilder. It is an interface (not an
// alias for *routerpkg.Router) so that modules do not take a hard
// import on pkg/router (ADR-010 §7).
//
// Three coexisting styles, all usable within the same module:
//
//   - Flat declarative: `r.Get("/articles", List)`
//   - Resource REST: `r.Resource("/tickets", controller{}, nucleus.Methods(...))`
//   - Nested closures: `r.Group("/admin", func(g Router) { ... })`
type Router interface {
	// Get registers an HTTP GET handler at the given path.
	Get(path string, h Handler)
	// Post registers an HTTP POST handler at the given path.
	Post(path string, h Handler)
	// Put registers an HTTP PUT handler at the given path.
	Put(path string, h Handler)
	// Patch registers an HTTP PATCH handler at the given path.
	Patch(path string, h Handler)
	// Delete registers an HTTP DELETE handler at the given path.
	Delete(path string, h Handler)

	// Group opens a sub-router scoped to the given prefix. Middleware
	// registered via the sub-router's Use applies only inside the
	// group. Groups can be nested arbitrarily.
	Group(prefix string, fn func(g Router))

	// Resource registers REST routes against a controller. The
	// `methods` argument lists which routes to mount — there is no
	// silent presence-based discovery. The controller must satisfy
	// the corresponding interface (Lister / Shower / Creator /
	// Updater / Destroyer) for every Method listed, or Resource
	// panics at construction time with a clear error.
	Resource(prefix string, controller any, methods []Method)

	// Use appends middleware to the router's chain. The middleware
	// applies to every route registered through this Router instance
	// (including those registered in nested Group blocks).
	Use(mw ...Middleware)
}

// nucleusRouter is the production Router implementation. It wraps a
// *routerpkg.Mux (the underlying primitive that does actual route
// registration) and translates the nucleus Handler / Method / Resource
// surface into the lower-level routerpkg calls.
//
// Modules never see this type — they receive the Router interface.
type nucleusRouter struct {
	mux *routerpkg.Mux
}

// newRouter builds a Router that registers against the supplied Mux.
// Used by AppBuilder.Build to construct the root router and by
// nucleusRouter.Group to construct sub-routers.
func newRouter(mux *routerpkg.Mux) *nucleusRouter {
	return &nucleusRouter{mux: mux}
}

func (r *nucleusRouter) Get(path string, h Handler)    { r.mux.Get(path, adaptHandler(h)) }
func (r *nucleusRouter) Post(path string, h Handler)   { r.mux.Post(path, adaptHandler(h)) }
func (r *nucleusRouter) Put(path string, h Handler)    { r.mux.Put(path, adaptHandler(h)) }
func (r *nucleusRouter) Patch(path string, h Handler)  { r.mux.Patch(path, adaptHandler(h)) }
func (r *nucleusRouter) Delete(path string, h Handler) { r.mux.Delete(path, adaptHandler(h)) }

// Group delegates to routerpkg.Mux.Route, which produces a sub-Mux
// scoped to the prefix. The fn callback runs against a nucleusRouter
// wrapping the sub-Mux, so further registration inherits the prefix
// and any middleware the sub-Mux carries.
func (r *nucleusRouter) Group(prefix string, fn func(g Router)) {
	r.mux.Route(prefix, func(sub *routerpkg.Mux) {
		fn(newRouter(sub))
	})
}

// Resource enforces explicit Method declarations and panics if the
// controller does not satisfy the corresponding interface. The panic
// happens at construction time (during Routes() of a module Build),
// never on the request path — same shape as
// router.CSRFMiddleware (`MustCompile`-style misconfiguration handling).
func (r *nucleusRouter) Resource(prefix string, controller any, methods []Method) {
	if controller == nil {
		panic(fmt.Sprintf("nucleus.Router.Resource(%q): controller is nil", prefix))
	}
	for _, m := range methods {
		switch m {
		case Index:
			h, ok := controller.(Lister)
			if !ok {
				panic(fmt.Sprintf("nucleus.Router.Resource(%q): Index requested but controller %T does not implement nucleus.Lister", prefix, controller))
			}
			r.Get(prefix, h.Index)
		case Show:
			h, ok := controller.(Shower)
			if !ok {
				panic(fmt.Sprintf("nucleus.Router.Resource(%q): Show requested but controller %T does not implement nucleus.Shower", prefix, controller))
			}
			r.Get(prefix+"/{id}", h.Show)
		case Create:
			h, ok := controller.(Creator)
			if !ok {
				panic(fmt.Sprintf("nucleus.Router.Resource(%q): Create requested but controller %T does not implement nucleus.Creator", prefix, controller))
			}
			r.Post(prefix, h.Create)
		case Update:
			h, ok := controller.(Updater)
			if !ok {
				panic(fmt.Sprintf("nucleus.Router.Resource(%q): Update requested but controller %T does not implement nucleus.Updater", prefix, controller))
			}
			r.Put(prefix+"/{id}", h.Update)
		case Destroy:
			h, ok := controller.(Destroyer)
			if !ok {
				panic(fmt.Sprintf("nucleus.Router.Resource(%q): Destroy requested but controller %T does not implement nucleus.Destroyer", prefix, controller))
			}
			r.Delete(prefix+"/{id}", h.Destroy)
		default:
			panic(fmt.Sprintf("nucleus.Router.Resource(%q): unknown Method %d", prefix, m))
		}
	}
}

// Use forwards middleware registration to the underlying Mux.
func (r *nucleusRouter) Use(mw ...Middleware) {
	r.mux.Use(mw...)
}

// adaptHandler bridges nucleus.Handler (func(*Context) error) and
// routerpkg.Handler (func(*routerpkg.Context) error) by wrapping the
// routerpkg context in a nucleus.Context.
func adaptHandler(h Handler) routerpkg.Handler {
	return func(rc *routerpkg.Context) error {
		return h(&Context{Context: rc})
	}
}
