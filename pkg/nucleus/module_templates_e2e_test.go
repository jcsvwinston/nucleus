// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// E2E for Module.Templates (ADR-022 §3): a module's embedded templates
// render through the framework engine under the module-name namespace, on a
// PREFIX-mounted module (the sub-mux copies the engine at derivation — the
// order constraint that forced the pre-app.New merge), with the module's
// own Policies opening the route. Zero files in templates_dir, zero manual
// RBAC edits.
package nucleus

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

func TestRun_ModuleTemplatesEndToEnd(t *testing.T) {
	port := freeLocalPort(t)

	modDef := Module[struct{}]{
		Name:   "shop",
		Prefix: "/shop",
		Templates: fstest.MapFS{
			"index.html": &fstest.MapFile{Data: []byte("<h1>{{.title}}</h1>")},
		},
		Policies: []PolicyRule{
			{Subject: "anonymous", Object: "/", Action: "read"},
			{Subject: "anonymous", Object: "/*", Action: "read"},
		},
		Routes: func(r Router, _ struct{}) {
			r.Get("/", func(c *Context) error {
				// Engine-backed render lives on the embedded router context;
				// the name is the module namespace + FS path.
				return c.Context.HTML(http.StatusOK, "shop/index.html", map[string]interface{}{"title": "embedded"})
			})
		},
	}

	cfg := app.DefaultConfig()
	cfg.Host = "127.0.0.1"
	cfg.Port = port
	cfg.Databases = map[string]app.DatabaseConfig{
		"default": {URL: "sqlite://" + filepath.Join(t.TempDir(), "e2e.db")},
	}

	runDone := make(chan error, 1)
	go func() {
		runDone <- Run(App{
			Config:  cfg,
			Modules: map[string]ModuleSpec{"shop": modDef.Build()},
		})
	}()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 2 * time.Second}
	waitForServer(t, client, base, runDone)

	resp, err := client.Get(base + "/shop/")
	if err != nil {
		t.Fatalf("GET /shop/: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /shop/: want 200, got %d body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "<h1>embedded</h1>") {
		t.Errorf("want the embedded template rendered with data, got %q", body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("want text/html, got %q", ct)
	}

	shutDownRunApp(t, runDone)
}
