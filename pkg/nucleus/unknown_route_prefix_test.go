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

// TestRun_UnknownPathUnderAModulePrefixAnswers404 pins ADR-033 for the
// shape a generated module takes: a Prefix mounts the module as a
// sub-router, and the default-deny and CSRF gates sit on the root router.
// The routing decision those gates read has to see through the mount to
// the module's own route table — otherwise every path under the prefix
// counted as matched, and a typo under /api answered 403 with a hint to
// grant a route that did not exist, while the same typo at the root
// answered 404.
func TestRun_UnknownPathUnderAModulePrefixAnswers404(t *testing.T) {
	cfg := app.DefaultConfig()
	cfg.Host = "127.0.0.1"
	cfg.CSRFEnabled = true
	cfg.JWTSecret = strings.Repeat("prefix-probe-secret", 2)
	cfg.Databases = map[string]app.DatabaseConfig{
		"default": {URL: "sqlite://" + filepath.Join(t.TempDir(), "prefix.db")},
	}

	mod := nucleus.Module[struct{}]{
		Name:   "items",
		Prefix: "/api",
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/items", func(c *nucleus.Context) error { return c.NoContent() })
			// A nested group is a mount inside the mount.
			r.Group("/v2", func(g nucleus.Router) {
				g.Get("/things", func(c *nucleus.Context) error { return c.NoContent() })
			})
		},
	}
	srv := nucleustest.StartApp(t, nucleus.App{
		Config:  cfg,
		Modules: map[string]nucleus.ModuleSpec{"items": mod.Build()},
	})
	t.Cleanup(srv.Stop)

	cases := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{"registered route, no policy row", http.MethodGet, "/api/items", http.StatusForbidden},
		{"registered route under a nested group, no policy row", http.MethodGet, "/api/v2/things", http.StatusForbidden},
		{"unknown path under the prefix", http.MethodGet, "/api/nope", http.StatusNotFound},
		{"unknown POST under the prefix (CSRF on, no token)", http.MethodPost, "/api/nope", http.StatusNotFound},
		{"unknown path below a route", http.MethodGet, "/api/items/1/typo", http.StatusNotFound},
		{"unknown path under the nested group", http.MethodGet, "/api/v2/nope", http.StatusNotFound},
		{"the prefix itself, no root route", http.MethodGet, "/api", http.StatusNotFound},
		{"registered path, unregistered method", http.MethodDelete, "/api/items", http.StatusMethodNotAllowed},
		{"unknown path outside the prefix", http.MethodGet, "/nope", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, srv.URL(tc.path), nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatalf("%s %s: %v", tc.method, tc.path, err)
			}
			resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Fatalf("%s %s: status = %d, want %d", tc.method, tc.path, resp.StatusCode, tc.want)
			}
		})
	}
}
