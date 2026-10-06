// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/router"
)

// NU-113: on the application router, a route registered through With ran
// the router's whole middleware stack twice per request — the stack New
// mounts, the session, the observability hook, the default-deny layer — and
// the handler saw an empty RouteFromContext. The routing guide's
// r.With(router.Timeout(...)) is exactly that call. Group never did it.

// invocations counts the calls of a middleware.
type invocations struct {
	mu sync.Mutex
	n  int
}

func (iv *invocations) mw(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		iv.mu.Lock()
		iv.n++
		iv.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

func (iv *invocations) count() int {
	iv.mu.Lock()
	defer iv.mu.Unlock()
	return iv.n
}

func TestAppRouterWith_RunsTheStackOnceAndKeepsTheRoute(t *testing.T) {
	a, err := New(testAppConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })

	// A middleware mounted on the application router the way New mounts
	// its own: whatever runs it twice runs the whole stack twice.
	stack := &invocations{}
	a.Router.Use(stack.mw)

	var mu sync.Mutex
	routes := map[string]string{}
	handler := func(c *router.Context) error {
		mu.Lock()
		// RequestURI: inside Route the path is stripped of the mount prefix.
		routes[c.Request.RequestURI] = router.RouteFromContext(c.Request.Context())
		mu.Unlock()
		return c.NoContent()
	}

	with, group, groupWith, scoped, scopedWith := &invocations{}, &invocations{}, &invocations{}, &invocations{}, &invocations{}
	a.Router.With(with.mw).Get("/reports/{id}", handler)
	a.Router.Group(func(g *router.Mux) {
		g.Use(group.mw)
		g.Get("/grouped/{id}", handler)
		g.With(groupWith.mw).Get("/grouped/{id}/export", handler)
	})
	a.Router.Route("/api", func(sub *router.Mux) {
		sub.Use(scoped.mw)
		sub.With(scopedWith.mw).Get("/items/{id}", handler)
	})

	for _, path := range []string{"/reports/7", "/grouped/7", "/grouped/7/export", "/api/items/7"} {
		if err := a.Authorizer.AddPolicy("anonymous", path, "read"); err != nil {
			t.Fatalf("AddPolicy %s: %v", path, err)
		}
	}

	cases := []struct {
		path  string
		route string
		added map[string]*invocations // the scope middleware on the way, each to run once
	}{
		{"/reports/7", "/reports/{id}", map[string]*invocations{"with": with}},
		{"/grouped/7", "/grouped/{id}", map[string]*invocations{"group": group}},
		{"/grouped/7/export", "/grouped/{id}/export", map[string]*invocations{"group": group, "with": groupWith}},
		{"/api/items/7", "/api/items/{id}", map[string]*invocations{"route": scoped, "with": scopedWith}},
	}
	for _, tc := range cases {
		before := stack.count()
		counts := map[string]int{}
		for name, iv := range tc.added {
			counts[name] = iv.count()
		}

		rec := httptest.NewRecorder()
		a.Router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("GET %s: %d %s, want 204", tc.path, rec.Code, rec.Body)
		}
		if n := stack.count() - before; n != 1 {
			t.Errorf("GET %s ran the application router's stack %d times, want 1", tc.path, n)
		}
		for name, iv := range tc.added {
			if n := iv.count() - counts[name]; n != 1 {
				t.Errorf("GET %s ran the %s middleware %d times, want 1", tc.path, name, n)
			}
		}
		mu.Lock()
		got := routes[tc.path]
		mu.Unlock()
		if got != tc.route {
			t.Errorf("GET %s: RouteFromContext inside the handler = %q, want %q", tc.path, got, tc.route)
		}
	}
}
