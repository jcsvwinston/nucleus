package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// FuzzMuxRouteMatching fuzzes the request line — method and path — against a
// fixed, developer-authored route table. The path is the untrusted half of
// routing: patterns are written by the application, but what arrives on the
// wire is whatever a client typed, percent-encoded, doubled up or walked with
// dot segments.
//
// The property is not "does not panic": it is that Matched agrees with what
// the mux actually does. Matched is the routing decision every WhenMatched
// gate consults — the framework's default-deny authorizer and the CSRF
// middleware are both built on it — so a request that reaches a handler while
// Matched reports false is an unguarded hit: the gate stepped aside and the
// handler ran anyway. That is the direction asserted first, and the one that
// matters for security. The converse (Matched true, no handler) is asserted
// too, because a gate that judges a path nothing serves answers 403/419 for a
// handler that does not exist — the 404-for-unknown-paths contract of
// mux_matched_test.go, here over arbitrary paths instead of eleven hand-picked
// ones.
//
// Seeds: every case from TestMatched_ReportsTheRoutingDecisionBeforeMiddleware
// Runs, plus the shapes that broke before — the mount's exact path serving the
// subtree root (NU-31), the see-through of nested mounts, the trailing slash
// under a mount — plus the encodings a client picks and an application never
// writes: %2e%2e, %2f, doubled slashes, dot segments, a bare wildcard spelling.
// The ones worth keeping under their own name are committed as corpus files in
// testdata/fuzz/FuzzMuxRouteMatching, where Go looks for them and where a
// future crash is written.
func FuzzMuxRouteMatching(f *testing.F) {
	mux := fuzzRouteTable()

	seeds := []struct{ method, path string }{
		{http.MethodGet, "/users"},
		{http.MethodGet, "/users/42"},
		{http.MethodPost, "/users/42/roles/admin"},
		{http.MethodGet, "/nope"},
		{http.MethodPost, "/users"},          // method mismatch -> 405
		{http.MethodGet, "/api/items"},       // under a mount
		{http.MethodGet, "/api/nope"},        // the parent must see through the mount
		{http.MethodDelete, "/api/items"},    // method mismatch under a mount
		{http.MethodGet, "/api/items/"},      // trailing slash under a mount
		{http.MethodGet, "/api"},             // the mount's exact path (NU-31)
		{http.MethodGet, "/api/v2/things"},   // nested mount
		{http.MethodGet, "/api/v2/nope"},     // nested mount, no route
		{http.MethodGet, "/opaque/anything"}, // mounted plain handler: opaque
		{http.MethodGet, "/health/"},         // registered with a trailing slash
		{http.MethodGet, "/files/a/b/c"},     // {path...} wildcard
		{http.MethodGet, "/users/%2e%2e/admin"},
		{http.MethodGet, "/api/..%2fitems"},
		{http.MethodGet, "//users"},
		{http.MethodGet, "/api/./items"},
		{http.MethodGet, "/api/v2/../items"},
		{http.MethodGet, "/users/{id}"},
		{http.MethodGet, "/"},
		{"BREW", "/users"},
	}
	for _, s := range seeds {
		f.Add(s.method, s.path)
	}

	f.Fuzz(func(t *testing.T, method, path string) {
		if !strings.HasPrefix(path, "/") {
			return // not a request path; the client never gets to send this
		}
		req, err := http.NewRequest(method, "http://nucleus.test"+path, nil)
		if err != nil {
			return // an unparseable method or target never reaches a mux
		}
		req.RemoteAddr = "192.0.2.10:4321"

		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		decision := rec.Header().Get(fuzzMatchedHeader)
		reached := rec.Header().Get(fuzzReachedHeader) == "yes"

		if reached && decision != "yes" {
			t.Fatalf("%s %s reached a handler while Matched reported %q: a WhenMatched gate would have stepped aside and left the hit unguarded",
				method, path, decision)
		}
		// A path the mux has to canonicalise ("//users", "/api/./items") is
		// answered with a redirect before any handler runs, and the decision
		// is taken against the CLEANED path — net/http's ServeMux.Handler
		// hands back the pattern the redirect target matches. Matched
		// therefore reports yes, which is the safe direction (the gate runs
		// and can still refuse); the equality below simply does not apply.
		// None of the handlers in the table answers 3xx, so a 3xx here is
		// always the mux redirecting.
		if rec.Code >= 300 && rec.Code < 400 {
			if reached {
				t.Fatalf("%s %s: a handler answered %d; the table has no redirecting handler", method, path, rec.Code)
			}
			return
		}
		if decision == "yes" && !reached {
			t.Fatalf("%s %s: Matched reported yes but no handler ran (status %d): a gate would answer for a handler that does not exist",
				method, path, rec.Code)
		}
		if !reached && rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s: no handler ran and the mux answered %d; an unrouted request must be a 404 or a 405",
				method, path, rec.Code)
		}
	})
}

const (
	fuzzMatchedHeader = "X-Fuzz-Matched"
	fuzzReachedHeader = "X-Fuzz-Reached"
)

// fuzzRouteTable builds the route table the fuzz target routes against: a
// leaf, two wildcard shapes, a mount, a mount inside that mount, and a mount
// whose target is not a Mux (opaque: the prefix is the whole decision). It is
// built once and only read afterwards — registration is what takes the lock,
// serving does not mutate it.
func fuzzRouteTable() *Mux {
	m := NewMux()
	m.Use(fuzzRecordMatched)
	m.Get("/users", fuzzReach)
	m.Get("/users/{id}", fuzzReach)
	m.Post("/users/{id}/roles/{role}", fuzzReach)
	m.Get("/files/{path...}", fuzzReach)
	m.Get("/health/", fuzzReach)
	m.Route("/api", func(sub *Mux) {
		sub.Get("/items", fuzzReach)
		sub.Route("/v2", func(nested *Mux) {
			nested.Get("/things", fuzzReach)
		})
	})
	m.Mount("/opaque", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(fuzzReachedHeader, "yes")
		w.WriteHeader(http.StatusTeapot)
	}))
	return m
}

// fuzzRecordMatched copies the routing decision into a response header, the
// way a WhenMatched gate reads it — a header survives even when the mux
// answers 404 and no handler ever runs.
func fuzzRecordMatched(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if Matched(r) {
			w.Header().Set(fuzzMatchedHeader, "yes")
		} else {
			w.Header().Set(fuzzMatchedHeader, "no")
		}
		next.ServeHTTP(w, r)
	})
}

func fuzzReach(c *Context) error {
	c.Writer.Header().Set(fuzzReachedHeader, "yes")
	return c.NoContent()
}
