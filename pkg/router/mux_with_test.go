// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// NU-113 (A11 N7, nucleus#597): Mux.With copied its parent's middleware
// into the scope it returned, whatever the parent was. A Group scope's
// middleware is applied to each handler at registration, so a scope derived
// from it must carry it; but the middleware of a top-level Mux — and of a
// sub-router built by Route — is applied by ServeHTTP in front of every
// request, and a route registered through With on it ran that whole stack a
// second time: two http_request log lines per request, two spans, the
// limiter charged twice, and the second TelemetryMiddleware handed the
// handler a fresh route holder the mux never wrote to, so RouteFromContext
// answered "". Group never had the defect: it inherits only from a Group.
package router

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// tally counts how many times each named middleware runs.
type tally struct {
	mu sync.Mutex
	n  map[string]int
}

func newTally() *tally { return &tally{n: map[string]int{}} }

func (tl *tally) mw(name string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tl.mu.Lock()
			tl.n[name]++
			tl.mu.Unlock()
			next.ServeHTTP(w, r)
		})
	}
}

func (tl *tally) count(name string) int {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	return tl.n[name]
}

// logLines is a log sink the timeout middleware's goroutine may write to.
type logLines struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logLines) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logLines) requests() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Count(l.buf.String(), "msg=http_request")
}

// TestMuxWith_RunsTheStackOnce drives every way of deriving a scope through
// a router built by New — the default stack plus one middleware mounted
// with Use, the way pkg/app mounts its own — and checks, for one request,
// that the stack ran once (one http_request line, one "stack" call), that
// every middleware the scopes added ran once, and that the handler sees the
// route template.
func TestMuxWith_RunsTheStackOnce(t *testing.T) {
	cases := []struct {
		name  string
		setup func(r *Router, tl *tally, h Handler)
		path  string
		route string   // what RouteFromContext answers inside the handler
		added []string // the middleware the scopes added, each to run once
	}{
		{
			name: "With on the top level",
			setup: func(r *Router, tl *tally, h Handler) {
				r.With(tl.mw("with")).Get("/reports/{id}", h)
			},
			path: "/reports/7", route: "/reports/{id}", added: []string{"with"},
		},
		{
			name: "With chained on With",
			setup: func(r *Router, tl *tally, h Handler) {
				r.With(tl.mw("outer")).With(tl.mw("inner")).Get("/reports/{id}", h)
			},
			path: "/reports/7", route: "/reports/{id}", added: []string{"outer", "inner"},
		},
		{
			name: "Group (the control)",
			setup: func(r *Router, tl *tally, h Handler) {
				r.Group(func(g *Mux) {
					g.Use(tl.mw("group"))
					g.Get("/reports/{id}", h)
				})
			},
			path: "/reports/7", route: "/reports/{id}", added: []string{"group"},
		},
		{
			name: "With inside Group",
			setup: func(r *Router, tl *tally, h Handler) {
				r.Group(func(g *Mux) {
					g.Use(tl.mw("group"))
					g.With(tl.mw("with")).Get("/reports/{id}", h)
				})
			},
			path: "/reports/7", route: "/reports/{id}", added: []string{"group", "with"},
		},
		{
			name: "With inside a nested Group",
			setup: func(r *Router, tl *tally, h Handler) {
				r.Group(func(g *Mux) {
					g.Use(tl.mw("outer"))
					g.Group(func(g *Mux) {
						g.Use(tl.mw("inner"))
						g.With(tl.mw("with")).Get("/reports/{id}", h)
					})
				})
			},
			path: "/reports/7", route: "/reports/{id}", added: []string{"outer", "inner", "with"},
		},
		{
			name: "Group inside With",
			setup: func(r *Router, tl *tally, h Handler) {
				r.With(tl.mw("with")).Group(func(g *Mux) {
					g.Use(tl.mw("group"))
					g.Get("/reports/{id}", h)
				})
			},
			path: "/reports/7", route: "/reports/{id}", added: []string{"with", "group"},
		},
		{
			name: "With inside Route",
			setup: func(r *Router, tl *tally, h Handler) {
				r.Route("/api", func(sub *Mux) {
					sub.Use(tl.mw("route"))
					sub.With(tl.mw("with")).Get("/items/{id}", h)
				})
			},
			path: "/api/items/7", route: "/api/items/{id}", added: []string{"route", "with"},
		},
		{
			name: "With inside a Group inside Route",
			setup: func(r *Router, tl *tally, h Handler) {
				r.Route("/api", func(sub *Mux) {
					sub.Use(tl.mw("route"))
					sub.Group(func(g *Mux) {
						g.Use(tl.mw("group"))
						g.With(tl.mw("with")).Get("/items/{id}", h)
					})
				})
			},
			path: "/api/items/7", route: "/api/items/{id}", added: []string{"route", "group", "with"},
		},
		{
			name: "Route inside With",
			setup: func(r *Router, tl *tally, h Handler) {
				r.With(tl.mw("with")).Route("/v1", func(sub *Mux) {
					sub.Use(tl.mw("route"))
					sub.Get("/ping", h)
				})
			},
			path: "/v1/ping", route: "/v1/ping", added: []string{"with", "route"},
		},
		{
			name: "Mount on With",
			setup: func(r *Router, tl *tally, h Handler) {
				panel := NewMux()
				panel.Use(tl.mw("panel"))
				panel.Get("/stats", h)
				r.With(tl.mw("with")).Mount("/panel", panel)
			},
			path: "/panel/stats", route: "/panel/stats", added: []string{"with", "panel"},
		},
		{
			name: "With on a mounted Mux",
			setup: func(r *Router, tl *tally, h Handler) {
				panel := NewMux()
				panel.Use(tl.mw("panel"))
				panel.With(tl.mw("with")).Get("/stats", h)
				r.Mount("/panel", panel)
			},
			path: "/panel/stats", route: "/panel/stats", added: []string{"panel", "with"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := &logLines{}
			r := New(slog.New(slog.NewTextHandler(logs, nil)))
			tl := newTally()
			r.Use(tl.mw("stack"))

			var route string
			var served int
			tc.setup(r, tl, func(c *Context) error {
				served++
				route = RouteFromContext(c.Request.Context())
				return c.NoContent()
			})

			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != http.StatusNoContent || served != 1 {
				t.Fatalf("GET %s: %d, handler ran %d times; want 204 once", tc.path, rec.Code, served)
			}
			if n := logs.requests(); n != 1 {
				t.Errorf("GET %s wrote %d http_request lines, want 1: the router's stack ran %d times", tc.path, n, n)
			}
			if n := tl.count("stack"); n != 1 {
				t.Errorf("the middleware mounted with Use ran %d times, want 1", n)
			}
			for _, name := range tc.added {
				if n := tl.count(name); n != 1 {
					t.Errorf("middleware %q ran %d times, want 1", name, n)
				}
			}
			if route != tc.route {
				t.Errorf("RouteFromContext inside the handler = %q, want %q", route, tc.route)
			}
		})
	}
}

// What With adds stays on the routes registered through it.
func TestMuxWith_LeavesTheParentsRoutesAlone(t *testing.T) {
	r := New(quietLogger())
	tl := newTally()
	r.Use(tl.mw("stack"))
	ok := func(c *Context) error { return c.NoContent() }
	r.With(tl.mw("with")).Get("/guarded", ok)
	r.Get("/open", ok)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/open", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("GET /open: %d, want 204", rec.Code)
	}
	if n := tl.count("with"); n != 0 {
		t.Errorf("the With middleware ran %d times on a route of the parent, want 0", n)
	}
	if n := tl.count("stack"); n != 1 {
		t.Errorf("the stack ran %d times on GET /open, want 1", n)
	}
}

// The routing guide's own example: a route that declares a longer timeout
// than the router-wide one outlives it. With the stack running a second
// time inside With, the inner copy of the router-wide timeout set a deadline
// of its own, the route's Timeout moved that one, and the outer deadline
// still fired.
func TestMuxWith_ALongerRouteTimeoutOutlivesTheRouterWideOne(t *testing.T) {
	m := NewMux()
	m.Use(TimeoutMiddleware(60 * time.Millisecond))
	m.With(Timeout(time.Second)).Get("/export", sleeper(150*time.Millisecond, nil))

	if rec := doShape(t, m, http.MethodGet, "/export", "application/json"); rec.Code != http.StatusOK {
		t.Fatalf("/export with its own 1s under a 60ms router-wide timeout: %d %s, want 200", rec.Code, rec.Body)
	}
}
