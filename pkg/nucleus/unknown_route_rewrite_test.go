package nucleus_test

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
)

// TestRun_ARewriteInAModuleMiddlewareIsJudgedAgainstTheFullPath pins
// ADR-033 for a module whose Middleware rewrites the path under its
// Prefix. The framework mounts Module.Middleware inside the Route, on the
// stripped request, so the root gates have already stepped aside for the
// unregistered alias when the rewrite lands it on a route. They run again
// — on the path with the prefix restored, the namespace the policy rows
// are written in. Judging the stripped path made an alias answer
// differently from the real path in both directions: /api/alias/static/report
// matched the bootstrap row for /static/* and answered 200 where
// /api/static/report answers 403, and /api/alias/items answered 403 where
// the granted /api/items answers 204.
func TestRun_ARewriteInAModuleMiddlewareIsJudgedAgainstTheFullPath(t *testing.T) {
	cfg := app.DefaultConfig()
	cfg.Host = "127.0.0.1"
	cfg.CSRFEnabled = true
	cfg.JWTSecret = strings.Repeat("rewrite-probe-secret", 2)
	cfg.Databases = map[string]app.DatabaseConfig{
		"default": {URL: "sqlite://" + filepath.Join(t.TempDir(), "rewrite.db")},
	}

	alias := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/alias/") {
				r2 := r.Clone(r.Context())
				u := *r.URL
				u.Path = "/" + strings.TrimPrefix(r.URL.Path, "/alias/")
				u.RawPath = ""
				r2.URL = &u
				r = r2
			}
			next.ServeHTTP(w, r)
		})
	}
	mod := nucleus.Module[struct{}]{
		Name:       "items",
		Prefix:     "/api",
		Middleware: []nucleus.Middleware{alias},
		// Objects are relative to Prefix: this row grants /api/items.
		Policies: []nucleus.PolicyRule{{Subject: "anonymous", Object: "/items", Action: "read"}},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/items", func(c *nucleus.Context) error { return c.NoContent() })
			r.Get("/static/report", func(c *nucleus.Context) error { return c.NoContent() })
		},
	}
	srv := nucleustest.StartApp(t, nucleus.App{
		Config:  cfg,
		Modules: map[string]nucleus.ModuleSpec{"items": mod.Build()},
	})
	t.Cleanup(srv.Stop)

	cases := []struct {
		name string
		path string
		want int
	}{
		{"granted route", "/api/items", http.StatusNoContent},
		{"alias onto the granted route answers the same", "/api/alias/items", http.StatusNoContent},
		{"registered route, no row (the bootstrap /static/* row is for the root)", "/api/static/report", http.StatusForbidden},
		{"alias onto it answers the same", "/api/alias/static/report", http.StatusForbidden},
		{"alias onto nothing", "/api/alias/nope", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := srv.Client().Get(srv.URL(tc.path))
			if err != nil {
				t.Fatalf("GET %s: %v", tc.path, err)
			}
			resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Fatalf("GET %s: status = %d, want %d", tc.path, resp.StatusCode, tc.want)
			}
		})
	}
}
