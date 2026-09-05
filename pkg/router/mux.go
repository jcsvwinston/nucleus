package router

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"html/template"
)

// Middleware is a function that wraps an http.Handler with additional behavior.
type Middleware = func(http.Handler) http.Handler

// RouteEntry represents a registered route for introspection via Walk.
type RouteEntry struct {
	Method      string
	Pattern     string
	Middlewares int
	// handler is what was registered, kept so Walk can hand it back; the
	// chi-compatible callback used to receive nil here (NU-32).
	handler http.Handler
}

// Mux wraps http.ServeMux with convenience methods for route registration,
// middleware chaining, grouping, and sub-router mounting. It serves as a
// drop-in replacement for chi.Router using only the Go standard library
// (requires Go 1.22+ for method-aware patterns and path value extraction).
type Mux struct {
	mux         *http.ServeMux
	middlewares []Middleware
	handler     http.Handler // cached: middleware chain wrapping mux
	routes      []RouteEntry
	// mounts holds the sub-Muxes Route (or Mount with a *Mux) registered,
	// keyed by the clean mount prefix ("/api"; "" for a root mount), so the
	// routing decision ServeHTTP records can see through the mount to the
	// sub-router's own route table (see resolve). Shared with Group scopes,
	// which register into the same ServeMux.
	mounts map[string]*Mux
	mu     sync.RWMutex
	// mountsMu guards mounts on its own: a Group scope shares the map with
	// its parent but not the parent's mu.
	mountsMu *sync.RWMutex
	isGroup  bool // true when this Mux is a Group scope sharing parent's ServeMux

	session   *auth.SessionManager
	templates *template.Template

	// How handleError reports an unclassified error: where the detail is
	// logged, and whether the body may carry it (WithDevelopmentErrors).
	logger            *slog.Logger
	developmentErrors bool
}

// NewMux creates a new Mux backed by a fresh http.ServeMux.
func NewMux() *Mux {
	smux := http.NewServeMux()
	return &Mux{
		mux:      smux,
		handler:  smux,
		mounts:   map[string]*Mux{},
		mountsMu: &sync.RWMutex{},
	}
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

// Use appends one or more middlewares to the Mux's middleware stack.
// For top-level Mux instances, middlewares are applied via ServeHTTP to all
// requests. For Group scopes, middlewares wrap individual handlers at
// registration time.
func (m *Mux) Use(mws ...Middleware) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.middlewares = append(m.middlewares, mws...)
	if !m.isGroup {
		m.rebuildHandler()
	}
}

// rebuildHandler recomputes the cached handler chain. Must be called under lock.
func (m *Mux) rebuildHandler() {
	h := http.Handler(m.mux)
	for i := len(m.middlewares) - 1; i >= 0; i-- {
		h = m.middlewares[i](h)
	}
	m.handler = h
}

// applyGroupMiddlewares wraps a handler with the middleware stack of a Group
// scope. For non-group muxes the handler is returned unchanged.
func (m *Mux) applyGroupMiddlewares(h http.Handler) http.Handler {
	if !m.isGroup || len(m.middlewares) == 0 {
		return h
	}
	for i := len(m.middlewares) - 1; i >= 0; i-- {
		h = m.middlewares[i](h)
	}
	return h
}

// ---------------------------------------------------------------------------
// Route registration
// ---------------------------------------------------------------------------

// Get registers a handler for GET requests matching pattern.
func (m *Mux) Get(pattern string, handlers ...Handler) {
	m.handle("GET", pattern, handlers...)
}

// Post registers a handler for POST requests matching pattern.
func (m *Mux) Post(pattern string, handlers ...Handler) {
	m.handle("POST", pattern, handlers...)
}

// Put registers a handler for PUT requests matching pattern.
func (m *Mux) Put(pattern string, handlers ...Handler) {
	m.handle("PUT", pattern, handlers...)
}

// Patch registers a handler for PATCH requests matching pattern.
func (m *Mux) Patch(pattern string, handlers ...Handler) {
	m.handle("PATCH", pattern, handlers...)
}

// Delete registers a handler for DELETE requests matching pattern.
func (m *Mux) Delete(pattern string, handlers ...Handler) {
	m.handle("DELETE", pattern, handlers...)
}

// Handle registers a handler for all HTTP methods matching pattern.
func (m *Mux) Handle(pattern string, h http.Handler) {
	m.handleStandard("", pattern, h)
}

// HandleFunc registers a HandlerFunc for all HTTP methods matching pattern.
func (m *Mux) HandleFunc(pattern string, h http.HandlerFunc) {
	m.handleStandard("", pattern, h)
}

func (m *Mux) handle(method, pattern string, handlers ...Handler) {
	h := m.applyGroupMiddlewares(ContextHandler(handlers...))
	// Wrap with a middleware that injects Mux-level dependencies into Context
	h = m.injectDependencies(h)
	m.register(method, pattern, h)
}

func (m *Mux) injectDependencies(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.RLock()
		sm := m.session
		tpl := m.templates
		m.mu.RUnlock()

		ctx := r.Context()
		if sm != nil {
			ctx = context.WithValue(ctx, sessionKey, sm)
		}
		if tpl != nil {
			ctx = context.WithValue(ctx, templatesKey, tpl)
		}
		if m.logger != nil || m.developmentErrors {
			ctx = context.WithValue(ctx, errorPolicyKey, errorPolicy{logger: m.logger, exposeDetail: m.developmentErrors})
		}

		if ctx != r.Context() {
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

// SetSessionManager sets the session manager for the Mux and its sub-routers.
func (m *Mux) SetSessionManager(sm *auth.SessionManager) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.session = sm
}

// SetHTMLTemplates sets the template engine for the Mux and its sub-routers.
func (m *Mux) SetHTMLTemplates(t *template.Template) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.templates = t
}

func (m *Mux) handleStandard(method, pattern string, h http.Handler) {
	h = m.applyGroupMiddlewares(h)
	m.register(method, pattern, h)
}

func (m *Mux) register(method, pattern string, h http.Handler) {
	p := pattern

	// In Go 1.22 net/http.ServeMux, a pattern ending in "/" is a subtree
	// pattern (matches everything under it) unless it has the "{$}" suffix.
	// For standard routes (Get, Post, etc.), we usually want exact matches.
	// We append "{$}" to patterns ending in "/" IF they are not empty and
	// don't already have a wildcard or "{$}".
	if method != "" && strings.HasSuffix(pattern, "/") &&
		!strings.Contains(pattern, "{") && !strings.HasSuffix(pattern, "{$}") {
		p += "{$}"
	}

	if method != "" {
		p = method + " " + p
	}
	h = recordRoute(pattern, h)
	m.mux.Handle(p, h)

	m.mu.Lock()
	m.routes = append(m.routes, RouteEntry{
		Method:      method,
		Pattern:     pattern,
		Middlewares: len(m.middlewares),
		handler:     h,
	})
	m.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Grouping and sub-routing
// ---------------------------------------------------------------------------

// newChild is THE single derivation point for every composition path —
// Group, With and Route (QCD-FW-8). It copies the parent's request-time
// dependencies (session manager, template engine) into the child; the
// shape differs only in the route namespace: an inline child (Group/With)
// shares the parent's ServeMux, a mounted child (Route) gets its own.
//
// History: the three call sites used to build their children by hand, and
// Route's copy (a bare NewMux()) forgot session and templates — so every
// nucleus module declaring a Prefix answered ErrTemplateEngineNotSet /
// ErrSessionManagerNotSet even with templates correctly loaded. Any new
// Mux-level dependency MUST be added here, not at a call site.
func (m *Mux) newChild(inline bool) *Mux {
	sub := &Mux{
		session:   m.session,
		templates: m.templates,
	}
	if inline {
		sub.mux = m.mux
		sub.isGroup = true
		// Same ServeMux, same mount table: a Route inside a Group lands in
		// the parent's namespace and the parent's decision must know it.
		sub.mounts = m.mounts
		sub.mountsMu = m.mountsMu
	} else {
		smux := http.NewServeMux()
		sub.mux = smux
		sub.handler = smux
		sub.mounts = map[string]*Mux{}
		sub.mountsMu = &sync.RWMutex{}
	}
	return sub
}

// Group creates an inline scope that shares the parent's ServeMux but
// maintains its own middleware stack. Middlewares added via Use inside the
// group only apply to routes registered within that group.
func (m *Mux) Group(fn func(sub *Mux)) {
	sub := m.newChild(true)
	// Nested Group scopes inherit parent group middlewares.
	if m.isGroup && len(m.middlewares) > 0 {
		sub.middlewares = append(sub.middlewares, m.middlewares...)
	}
	fn(sub)

	m.mu.Lock()
	m.routes = append(m.routes, sub.routes...)
	m.mu.Unlock()
}

// With adds a list of middlewares to an inline sub-router and returns it.
func (m *Mux) With(mws ...Middleware) *Mux {
	sub := m.newChild(true)
	if len(m.middlewares) > 0 {
		sub.middlewares = append(sub.middlewares, m.middlewares...)
	}
	sub.middlewares = append(sub.middlewares, mws...)
	return sub
}

// Route creates a sub-router mounted under the given pattern prefix. The sub-
// router has its own middleware stack and its own route namespace, and
// inherits the parent's session manager and template engine (QCD-FW-8).
func (m *Mux) Route(pattern string, fn func(sub *Mux)) {
	sub := m.newChild(false)
	fn(sub)
	m.Mount(pattern, sub)
}

// Mount registers handler under the given pattern prefix. Requests matching
// the prefix are forwarded to handler with the prefix stripped. If pattern
// does not end with "/", a trailing slash is appended so that the ServeMux
// treats it as a subtree pattern.
func (m *Mux) Mount(pattern string, handler http.Handler) {
	cleanPattern := strings.TrimSpace(pattern)
	if cleanPattern == "" || cleanPattern == "/" {
		// Mounting at root should not strip prefix and must avoid invalid ""
		// patterns in net/http.ServeMux.
		root := m.applyGroupMiddlewares(handler)
		m.mux.Handle("/", root)
		m.recordMount("", handler)

		m.mu.Lock()
		m.routes = append(m.routes, RouteEntry{
			Method:      "*",
			Pattern:     "/*",
			Middlewares: len(m.middlewares),
			handler:     root,
		})
		m.mu.Unlock()
		return
	}
	if !strings.HasPrefix(cleanPattern, "/") {
		cleanPattern = "/" + cleanPattern
	}
	cleanPattern = strings.TrimRight(cleanPattern, "/")

	mounted := http.StripPrefix(cleanPattern, handler)
	mounted = m.applyGroupMiddlewares(mounted)
	mounted = recordMountPrefix(cleanPattern, mounted)

	// Register subtree handler (with trailing slash).
	m.mux.Handle(cleanPattern+"/", mounted)

	// The exact path ("/users") serves the subtree root as if it were
	// "/users/": a Resource's collection answers at both spellings and an
	// API client never has to follow a 307 to list (NU-31). The mounted
	// handler sees the path it would see under the prefix, "/".
	var exact http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r2 := r.Clone(r.Context())
		u := *r.URL
		u.Path = "/"
		if u.RawPath != "" {
			u.RawPath = "/"
		}
		r2.URL = &u
		handler.ServeHTTP(w, r2)
	})
	exact = m.applyGroupMiddlewares(exact)
	exact = recordMountPrefix(cleanPattern, exact)
	m.mux.Handle(cleanPattern, exact)
	m.recordMount(cleanPattern, handler)

	m.mu.Lock()
	m.routes = append(m.routes, RouteEntry{
		Method:      "*",
		Pattern:     cleanPattern + "/*",
		Middlewares: len(m.middlewares),
		handler:     mounted,
	})
	m.mu.Unlock()
}

// recordMount remembers a mounted handler that is itself a Mux, so resolve
// can continue the routing decision inside it. A plain handler (a file
// server, a third-party mux) is opaque: the mount prefix is the whole
// decision, and Matched reports true for everything under it.
func (m *Mux) recordMount(prefix string, handler http.Handler) {
	sub, ok := handler.(*Mux)
	if !ok {
		return
	}
	m.mountsMu.Lock()
	m.mounts[prefix] = sub
	m.mountsMu.Unlock()
}

// resolve takes the routing decision Matched reports: whether a registered
// route serves r. When the pattern that matched is a mount of another Mux
// (Route, or Mount with a *Mux) the decision is not taken yet — the parent
// only matched the prefix — so the lookup continues in the sub-router with
// the prefix stripped, the way StripPrefix will strip it at dispatch, down
// to a leaf pattern or a miss. Without this every path under a module's
// Prefix counted as matched and the gates at the root answered 403 or 419
// for handlers the module did not have.
func (m *Mux) resolve(r *http.Request) bool {
	_, pattern := m.mux.Handler(r)
	if pattern == "" {
		return false
	}
	// Mounts register two method-less patterns, "/api/" and "/api"; both
	// map to the "/api" key. Routes carry their method ("GET /users"), so
	// they never collide with a mount key.
	m.mountsMu.RLock()
	sub, ok := m.mounts[strings.TrimSuffix(pattern, "/")]
	m.mountsMu.RUnlock()
	if !ok {
		return true
	}
	prefix := strings.TrimSuffix(pattern, "/")
	r2 := r.Clone(r.Context())
	u := *r.URL
	u.Path = strings.TrimPrefix(u.Path, prefix)
	if u.Path == "" {
		u.Path = "/" // the exact mount path serves the sub-router's root
	}
	if u.RawPath != "" {
		u.RawPath = strings.TrimPrefix(u.RawPath, prefix)
		if u.RawPath == "" {
			u.RawPath = "/"
		}
	}
	r2.URL = &u
	return sub.resolve(r2)
}

// Static registers a handler to serve static files from root directory under
// the given pattern prefix.
func (m *Mux) Static(pattern, root string) {
	fs := http.FileServer(http.Dir(root))
	m.Mount(pattern, fs)
}

// recordRoute notes the route template into the telemetry holder (see
// routeHolder in otel.go) when the mux dispatches to h: the holder's prefix
// — accumulated by every Mount on the way down — plus this pattern is the
// full template, "/users/{id}", whatever copies of the request the
// middlewares made.
func recordRoute(pattern string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rh, ok := r.Context().Value(routeCtxKey{}).(*routeHolder); ok {
			rh.pattern = rh.prefix + pattern
		}
		h.ServeHTTP(w, r)
	})
}

// recordMountPrefix extends the holder's prefix with a mount point before
// the mounted handler registers its own (relative) pattern.
func recordMountPrefix(prefix string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rh, ok := r.Context().Value(routeCtxKey{}).(*routeHolder); ok {
			rh.prefix += prefix
			rh.pattern = rh.prefix + "/"
		}
		h.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------------------
// http.Handler implementation
// ---------------------------------------------------------------------------

// ServeHTTP dispatches the request through the middleware chain and into the
// underlying ServeMux.
func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.RLock()
	h := m.handler
	m.mu.RUnlock()
	// Resolve the route BEFORE the middleware chain runs and record whether
	// one exists, so a security layer mounted on this Mux can tell "no
	// handler serves this path" from "this handler is not permitted". The
	// ServeMux repeats the lookup when it dispatches; the cost is one tree
	// walk, and the alternative — a uniform 403 or 419 for every path
	// nobody serves — hid the 404 that tells a caller they mistyped the
	// URL (see Matched). The decision sees through mounted sub-routers.
	r = r.WithContext(context.WithValue(r.Context(), routeMatchKey{}, m.resolve(r)))
	// The session manager and the templates are injected at the top of the
	// chain too, not only around each handler: a top-level middleware such
	// as CSRF with UseSessionToken used to find no session in the context
	// and fall back to the cookie without a word (NU-33).
	m.injectDependencies(h).ServeHTTP(w, r)
}

// routeMatchKey carries the routing decision Mux.ServeHTTP took before the
// middleware chain ran: true when a registered pattern serves the request,
// false when the ServeMux would answer 404 (or 405) itself.
type routeMatchKey struct{}

// Matched reports whether a registered route serves the request. A Mux
// resolves the route before its middleware chain runs and records the
// answer in the request context, so a middleware mounted with Use can let
// an unregistered path fall through to the mux's own 404 instead of
// answering for a handler that does not exist — the framework's
// default-deny authorizer and the CSRF middleware both do.
//
// The decision sees through mounted sub-routers (Route, or Mount with a
// *Mux): at the parent, a path under a mount prefix counts as matched only
// when the sub-router — or a sub-router of its own, any depth down — has a
// route for the rest of the path, so a gate mounted at the root judges the
// same route table a gate inside the mount would. A handler mounted with
// Mount that is not a Mux is opaque, and everything under its prefix
// reports true. A method-only mismatch (the path is registered for other
// methods) reports false, and the mux answers 405.
//
// When no Mux has dispatched the request — the middleware is wrapped
// around a plain http.Handler, or the test calls it directly — there is no
// routing decision to consult and Matched reports true, so every security
// layer keeps enforcing as if the route existed.
func Matched(r *http.Request) bool {
	if r == nil {
		return true
	}
	matched, ok := r.Context().Value(routeMatchKey{}).(bool)
	if !ok {
		return true
	}
	return matched
}

// ---------------------------------------------------------------------------
// Introspection
// ---------------------------------------------------------------------------

// Walk iterates over all registered routes, calling fn for each one. The
// signature is compatible with the chi.Walk callback API so that callers
// can migrate without code changes beyond the call site.
func (m *Mux) Walk(fn func(method string, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error) error {
	m.mu.RLock()
	snapshot := make([]RouteEntry, len(m.routes))
	copy(snapshot, m.routes)
	m.mu.RUnlock()

	for _, re := range snapshot {
		dummyMWs := make([]func(http.Handler) http.Handler, re.Middlewares)
		if err := fn(re.Method, re.Pattern, re.handler, dummyMWs...); err != nil {
			return err
		}
	}
	return nil
}
