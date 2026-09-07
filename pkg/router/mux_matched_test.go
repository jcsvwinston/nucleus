package router

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// recordMatched is a middleware that copies the routing decision into a
// response header so a test can read it without a handler — an unmatched
// request has none.
func recordMatched(header string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if Matched(r) {
				w.Header().Set(header, "yes")
			} else {
				w.Header().Set(header, "no")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// TestMatched_ReportsTheRoutingDecisionBeforeMiddlewareRuns is the
// contract behind the 404-for-unknown-paths change: a middleware mounted
// with Use can tell whether a registered route serves the request, at the
// parent and inside a mounted sub-router, before anything answers.
func TestMatched_ReportsTheRoutingDecisionBeforeMiddlewareRuns(t *testing.T) {
	m := NewMux()
	m.Use(recordMatched("X-Matched"))
	m.Get("/users", func(c *Context) error { return c.NoContent() })
	m.Route("/api", func(sub *Mux) {
		sub.Use(recordMatched("X-Sub-Matched"))
		sub.Get("/items", func(c *Context) error { return c.NoContent() })
		// A mount inside the mount: the decision has to see through every
		// level, not only the first.
		sub.Route("/v2", func(nested *Mux) {
			nested.Get("/things", func(c *Context) error { return c.NoContent() })
		})
	})
	// A mount whose target is not a Mux: the parent cannot look inside it,
	// so the mount prefix is the whole decision.
	m.Mount("/opaque", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	cases := []struct {
		name        string
		method      string
		path        string
		wantStatus  int
		wantMatched string
		wantSub     string // "" when the sub-router is never reached
	}{
		{"registered route", http.MethodGet, "/users", http.StatusNoContent, "yes", ""},
		{"unregistered path", http.MethodGet, "/nope", http.StatusNotFound, "no", ""},
		{"method mismatch on a registered path", http.MethodPost, "/users", http.StatusMethodNotAllowed, "no", ""},
		{"registered route under a mount", http.MethodGet, "/api/items", http.StatusNoContent, "yes", "yes"},
		// The parent sees through the mount: the prefix matching is not a
		// route, and a gate mounted at the top level must not answer for a
		// handler the sub-router does not have.
		{"unregistered path under a mount", http.MethodGet, "/api/nope", http.StatusNotFound, "no", "no"},
		{"method mismatch under a mount", http.MethodDelete, "/api/items", http.StatusMethodNotAllowed, "no", "no"},
		{"trailing slash on a route under a mount", http.MethodGet, "/api/items/", http.StatusNotFound, "no", "no"},
		{"the mount's exact path with no root route", http.MethodGet, "/api", http.StatusNotFound, "no", "no"},
		{"registered route under a nested mount", http.MethodGet, "/api/v2/things", http.StatusNoContent, "yes", "yes"},
		{"unregistered path under a nested mount", http.MethodGet, "/api/v2/nope", http.StatusNotFound, "no", "no"},
		{"mounted plain handler", http.MethodGet, "/opaque/anything", http.StatusTeapot, "yes", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			m.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
			if rec.Code != tc.wantStatus {
				t.Fatalf("%s %s: status = %d, want %d", tc.method, tc.path, rec.Code, tc.wantStatus)
			}
			if got := rec.Header().Get("X-Matched"); got != tc.wantMatched {
				t.Fatalf("%s %s: Matched at the parent = %q, want %q", tc.method, tc.path, got, tc.wantMatched)
			}
			if got := rec.Header().Get("X-Sub-Matched"); got != tc.wantSub {
				t.Fatalf("%s %s: Matched inside the mount = %q, want %q", tc.method, tc.path, got, tc.wantSub)
			}
		})
	}
}

// TestMatched_WithoutAMuxReportsTrue: a middleware wrapped around a plain
// handler has no routing decision to consult and must keep enforcing, so
// the conservative answer is "matched".
func TestMatched_WithoutAMuxReportsTrue(t *testing.T) {
	if !Matched(httptest.NewRequest(http.MethodGet, "/anything", nil)) {
		t.Fatal("Matched must report true when no Mux dispatched the request")
	}
	if !Matched(nil) {
		t.Fatal("Matched(nil) must report true")
	}
}

// TestCSRF_UnregisteredPathFallsThroughToThe404 pins the CSRF half of the
// change: a POST to a path nobody serves answers the mux's 404, not a 419
// vouching for a form that does not exist — while a registered route
// without a token is still refused.
func TestCSRF_UnregisteredPathFallsThroughToThe404(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := New(logger, WithCSRF())
	r.Post("/form", func(c *Context) error { return c.NoContent() })

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST /nope with CSRF on: status = %d, want 404 (the mux answers, not the CSRF gate); body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/form", nil))
	if rec.Code != 419 {
		t.Fatalf("POST /form without a token: status = %d, want 419 (registered routes stay CSRF-protected)", rec.Code)
	}
}

// rewritePath is a Use middleware in the shape of a request interceptor
// that aliases one prefix onto another — the "standard Go middleware"
// an operator can mount through http_interceptors.
func rewritePath(from, to string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, from) {
				r2 := r.Clone(r.Context())
				u := *r.URL
				u.Path = to + strings.TrimPrefix(r.URL.Path, from)
				u.RawPath = ""
				r2.URL = &u
				r = r2
			}
			next.ServeHTTP(w, r)
		})
	}
}

// TestMatched_FollowsTheRequestEachMiddlewareSees: the routing decision is
// not frozen when the request enters the Mux — a middleware that rewrites
// the path changes the answer for everything downstream of it. A frozen
// decision let an unregistered alias walk past every gate onto a
// registered route.
func TestMatched_FollowsTheRequestEachMiddlewareSees(t *testing.T) {
	m := NewMux()
	m.Use(recordMatched("X-Before"))
	m.Use(rewritePath("/alias/", "/"))
	m.Use(recordMatched("X-After"))
	m.Get("/users", func(c *Context) error { return c.NoContent() })
	m.Route("/api", func(sub *Mux) {
		sub.Use(recordMatched("X-Sub-Before"))
		sub.Use(rewritePath("/alias/", "/"))
		sub.Use(recordMatched("X-Sub-After"))
		sub.Get("/items", func(c *Context) error { return c.NoContent() })
	})

	cases := []struct {
		name                        string
		path                        string
		wantStatus                  int
		wantBefore, wantAfter       string
		wantSubBefore, wantSubAfter string
	}{
		{"registered route", "/users", http.StatusNoContent, "yes", "yes", "", ""},
		{"alias rewritten onto a registered route", "/alias/users", http.StatusNoContent, "no", "yes", "", ""},
		{"alias rewritten onto nothing", "/alias/nope", http.StatusNotFound, "no", "no", "", ""},
		{"alias rewritten inside a mount", "/api/alias/items", http.StatusNoContent, "no", "no", "no", "yes"},
		{"alias rewritten onto nothing inside a mount", "/api/alias/nope", http.StatusNotFound, "no", "no", "no", "no"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != tc.wantStatus {
				t.Fatalf("GET %s: status = %d, want %d", tc.path, rec.Code, tc.wantStatus)
			}
			for header, want := range map[string]string{
				"X-Before": tc.wantBefore, "X-After": tc.wantAfter,
				"X-Sub-Before": tc.wantSubBefore, "X-Sub-After": tc.wantSubAfter,
			} {
				if got := rec.Header().Get(header); got != want {
					t.Errorf("GET %s: %s = %q, want %q", tc.path, header, got, want)
				}
			}
		})
	}
}

// TestWhenMatched_AGateThatSteppedAsideRunsAgainWhenARewriteLandsOnARoute:
// a gate built with WhenMatched lets an unmatched request through — the
// mux's 404 answers — but if a later middleware rewrites the path onto a
// registered route, the router runs the gate before the handler anyway.
// Otherwise a rewriting middleware mounted after the gate would turn any
// unregistered alias into an unguarded door to any registered route.
func TestWhenMatched_AGateThatSteppedAsideRunsAgainWhenARewriteLandsOnARoute(t *testing.T) {
	deny := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Gate-Saw", r.URL.Path)
			w.WriteHeader(http.StatusForbidden)
		})
	}
	m := NewMux()
	m.Use(WhenMatched(deny))
	m.Use(rewritePath("/alias/", "/"))
	m.Get("/users", func(c *Context) error { return c.NoContent() })
	m.Route("/api", func(sub *Mux) {
		sub.Use(rewritePath("/alias/", "/"))
		sub.Get("/items", func(c *Context) error { return c.NoContent() })
	})

	cases := []struct {
		name       string
		path       string
		wantStatus int
		wantSaw    string // the path the gate judged; "" when it never ran
	}{
		{"registered route", "/users", http.StatusForbidden, "/users"},
		{"unregistered path", "/nope", http.StatusNotFound, ""},
		{"alias rewritten onto a registered route", "/alias/users", http.StatusForbidden, "/users"},
		{"alias rewritten onto nothing", "/alias/nope", http.StatusNotFound, ""},
		// The gate stepped aside at the root, so it judges the path in the
		// root's namespace — the mount prefix restored — never the stripped
		// path a sub-router sees: policy rows and CSRF exemptions are
		// written against the full path.
		{"alias rewritten inside a mount", "/api/alias/items", http.StatusForbidden, "/api/items"},
		{"alias rewritten onto nothing inside a mount", "/api/alias/nope", http.StatusNotFound, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != tc.wantStatus {
				t.Fatalf("GET %s: status = %d, want %d", tc.path, rec.Code, tc.wantStatus)
			}
			if got := rec.Header().Get("X-Gate-Saw"); got != tc.wantSaw {
				t.Fatalf("GET %s: the gate judged %q, want %q", tc.path, got, tc.wantSaw)
			}
		})
	}
}

// TestWhenMatched_ADeferredGateJudgesThePathInItsOwnNamespace pins the
// replay of a deferred gate across mount levels: each gate that stepped
// aside runs on the path as its own level spells it — the root gate with
// every mount prefix restored, a gate inside /api with only the prefixes
// below it — and the handler still receives the stripped request, with
// the context the gates passed down. A gate that judged the stripped path
// evaluated the root's policy rows against a path that lives inside the
// mount, so a rewrite inside a mount could both bypass a root exemption
// and deny a granted route.
func TestWhenMatched_ADeferredGateJudgesThePathInItsOwnNamespace(t *testing.T) {
	type sawKey struct{}
	record := func(header string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set(header, r.URL.Path)
				// A value the gate adds must reach the handler.
				ctx := context.WithValue(r.Context(), sawKey{}, header)
				next.ServeHTTP(w, r.WithContext(ctx))
			})
		}
	}
	m := NewMux()
	m.Use(WhenMatched(record("X-Root-Saw")))
	m.Route("/api", func(sub *Mux) {
		sub.Use(WhenMatched(record("X-Api-Saw")))
		sub.Route("/v2", func(nested *Mux) {
			nested.Use(rewritePath("/alias/", "/"))
			nested.Get("/things", func(c *Context) error {
				c.Writer.Header().Set("X-Handler-Saw", c.Request.URL.Path)
				v, _ := c.Request.Context().Value(sawKey{}).(string)
				c.Writer.Header().Set("X-Handler-Ctx", v)
				return c.NoContent()
			})
		})
	})

	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v2/alias/things", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	for header, want := range map[string]string{
		"X-Root-Saw":    "/api/v2/things",
		"X-Api-Saw":     "/v2/things",
		"X-Handler-Saw": "/things",
		"X-Handler-Ctx": "X-Api-Saw", // the innermost gate's context reached the handler
	} {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

// TestCSRF_ARewriteAfterTheGateCannotSkipIt pins the CSRF gate on the same
// rule: a POST to an unregistered alias is a 404 when nothing rewrites it,
// and a 419 when a later middleware lands it on a registered form.
func TestCSRF_ARewriteAfterTheGateCannotSkipIt(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := New(logger, WithCSRF())
	r.Use(rewritePath("/alias/", "/"))
	r.Post("/form", func(c *Context) error { return c.NoContent() })

	for path, want := range map[string]int{"/alias/form": 419, "/alias/nope": http.StatusNotFound, "/form": 419} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
		if rec.Code != want {
			t.Fatalf("POST %s without a token: status = %d, want %d; body=%s", path, rec.Code, want, rec.Body.String())
		}
	}
}

// TestWhenMatched_ADeferredGateThatParsesTheFormLeavesItForTheHandler
// pins the third review round's major: the prefix-restored replay hands a
// gate that stepped aside at the root a clone of the request, and a gate
// that reads the form there — the CSRF gate does, for a token sent in the
// form field — consumes the Body the clone shares with the original and
// caches the parsed form on the clone alone. The handler must read the
// same fields through the alias as through the real path.
func TestWhenMatched_ADeferredGateThatParsesTheFormLeavesItForTheHandler(t *testing.T) {
	formGate := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Gate-Saw", r.URL.Path)
			w.Header().Set("X-Gate-Tok", r.FormValue("tok"))
			next.ServeHTTP(w, r)
		})
	}
	m := NewMux()
	m.Use(WhenMatched(formGate))
	m.Route("/api", func(sub *Mux) {
		sub.Use(rewritePath("/alias/", "/"))
		sub.Post("/form", func(c *Context) error {
			c.Writer.Header().Set("X-Handler-Name", c.Request.FormValue("name"))
			return c.NoContent()
		})
	})

	for _, path := range []string{"/api/form", "/api/alias/form"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("tok=1&name=bob"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			m.ServeHTTP(rec, req)
			if rec.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want 204", rec.Code)
			}
			for header, want := range map[string]string{
				"X-Gate-Saw":     "/api/form",
				"X-Gate-Tok":     "1",
				"X-Handler-Name": "bob",
			} {
				if got := rec.Header().Get(header); got != want {
					t.Errorf("%s = %q, want %q", header, got, want)
				}
			}
		})
	}
}
