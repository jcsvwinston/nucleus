package nucleus

import (
	"net/http"
	"net/http/httptest"
	"testing"

	routerpkg "github.com/jcsvwinston/nucleus/pkg/router"
)

// TestRouterWith_AppliesPerRouteAndIsolatesSiblings is the core guard for
// Router.With (finding #24): middleware chained via With runs on the route
// registered through the returned Router, and does NOT leak onto sibling routes
// registered on the parent.
func TestRouterWith_AppliesPerRouteAndIsolatesSiblings(t *testing.T) {
	mux := routerpkg.NewMux()
	a := newRouterAdapterFromMux(mux, "")

	mark := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Guard", "ran")
			next.ServeHTTP(w, r)
		})
	}
	ok := func(c *Context) error { return c.String(http.StatusOK, "ok") }

	a.With(mark).Get("/guarded", ok)
	a.Get("/open", ok) // sibling on the parent — must NOT inherit mark

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/guarded", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /guarded: want 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("X-Guard"); got != "ran" {
		t.Errorf("With middleware did not run on its route (X-Guard=%q, want \"ran\")", got)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/open", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /open: want 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("X-Guard"); got != "" {
		t.Errorf("With middleware leaked onto sibling /open (X-Guard=%q); per-route middleware must not affect parent routes", got)
	}
}

// TestRouterWith_ShortCircuitsHandler verifies the role-guard use case: a With
// middleware that denies the request (writes its own response and does not call
// next) prevents the route handler from running.
func TestRouterWith_ShortCircuitsHandler(t *testing.T) {
	mux := routerpkg.NewMux()
	a := newRouterAdapterFromMux(mux, "")

	deny := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "forbidden", http.StatusForbidden) // does not call next
		})
	}
	handlerRan := false
	a.With(deny).Get("/x", func(c *Context) error {
		handlerRan = true
		return c.String(http.StatusOK, "ok")
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("denied route: want 403, got %d", rec.Code)
	}
	if handlerRan {
		t.Error("route handler ran despite the With middleware short-circuiting")
	}
}

// TestRouterWith_ComposesWithPrefix confirms With preserves the adapter's
// prefix AND still runs its middleware: a With chained off a prefixed adapter
// registers under the prefix with the guard applied.
func TestRouterWith_ComposesWithPrefix(t *testing.T) {
	mux := routerpkg.NewMux()
	a := newRouterAdapterFromMux(mux, "/api")

	mwRan, hit := false, false
	a.With(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mwRan = true
			next.ServeHTTP(w, r)
		})
	}).Get("/widgets", func(c *Context) error {
		hit = true
		return c.String(http.StatusOK, "ok")
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/widgets", nil))
	if rec.Code != http.StatusOK || !hit || !mwRan {
		t.Fatalf("With under prefix /api: GET /api/widgets want 200+hit+mwRan, got %d (hit=%v mwRan=%v)", rec.Code, hit, mwRan)
	}
}

// withResourceController is a minimal Index-only controller for exercising
// With() + Resource().
type withResourceController struct{ indexed *bool }

func (c withResourceController) Index(ctx *Context) error {
	*c.indexed = true
	return ctx.String(http.StatusOK, "ok")
}

// TestRouterWith_AppliesToResource guards that With middleware applies to
// Resource registrations too (Resource routes through the same sub-Mux). This
// is the auth-relevant case: r.With(RequireRole(...)).Resource(...) must not
// silently drop the guard.
func TestRouterWith_AppliesToResource(t *testing.T) {
	mux := routerpkg.NewMux()
	a := newRouterAdapterFromMux(mux, "")

	guardRan, indexed := false, false
	guard := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			guardRan = true
			next.ServeHTTP(w, r)
		})
	}
	a.With(guard).Resource("/billing", withResourceController{indexed: &indexed}, Methods(Index))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/billing", nil))
	if rec.Code != http.StatusOK || !indexed {
		t.Fatalf("With().Resource(): GET /billing want 200+indexed, got %d (indexed=%v)", rec.Code, indexed)
	}
	if !guardRan {
		t.Error("With middleware did not run on a Resource route — guards mounted via With().Resource() would be silently bypassed")
	}
}

// TestRouterWith_NestedUnderGroup confirms the documented additive composition:
// a With inside a Group inherits the group's URL prefix and the chain composes.
func TestRouterWith_NestedUnderGroup(t *testing.T) {
	mux := routerpkg.NewMux()
	a := newRouterAdapterFromMux(mux, "")

	guardRan := false
	guard := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			guardRan = true
			next.ServeHTTP(w, r)
		})
	}
	a.Group("/admin", func(g Router) {
		g.With(guard).Get("/stats", func(c *Context) error { return c.String(http.StatusOK, "ok") })
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/stats", nil))
	if rec.Code != http.StatusOK || !guardRan {
		t.Fatalf("Group+With: GET /admin/stats want 200+guardRan, got %d (guardRan=%v)", rec.Code, guardRan)
	}
}
