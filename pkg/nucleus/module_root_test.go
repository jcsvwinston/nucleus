// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// QCD-FW-13 / QCD-FW-15: a module could not declare its OWN mount root,
// and its CSRF exemptions arrived with neither an operator veto nor a
// trace in the boot log.
//
// ADR-022 §1 promises that a mounted module stops answering "a mute
// 403/419 until the operator hand-edited rbac_policy_file and
// csrf_exempt_paths". It held for every path EXCEPT the one a module is
// most likely to serve: its own root. `validatePolicyRule` demanded a
// leading "/", so the shortest declarable object was "/" → "<prefix>/",
// and keyMatch("/consola", "/consola/") is false. The external demo
// carried a programmatic enf.AddPolicy per module to paper over it — the
// very workaround the ADR exists to remove, moved from the CSV to code.
package nucleus

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

// A module's whole surface, declared once.
func TestResolveModuleRoot_WholeSurface(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
		object string
		want   []string
	}{
		{
			name:   "slash means root and subtree",
			prefix: "/consola",
			object: "/",
			want:   []string{"/consola", "/consola/*"},
		},
		{
			name:   "empty means the root exactly",
			prefix: "/consola",
			object: "",
			want:   []string{"/consola"},
		},
		{
			name:   "explicit subtree is unchanged",
			prefix: "/consola",
			object: "/*",
			want:   []string{"/consola/*"},
		},
		{
			name:   "explicit path is unchanged",
			prefix: "/consola",
			object: "/lista",
			want:   []string{"/consola/lista"},
		},
		{
			name:   "no prefix, whole surface",
			prefix: "",
			object: "/",
			want:   []string{"/", "/*"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveModulePolicyObjects(tc.prefix, tc.object)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("resolveModulePolicyObjects(%q, %q) = %v, want %v", tc.prefix, tc.object, got, tc.want)
			}
		})
	}
}

// The CSRF matcher is a RAW PREFIX check, not keyMatch, so the module root
// needs the opposite treatment: "<prefix>" already covers the subtree, and
// the trailing slash is what broke the collection POST (a module at
// /api/v1/announcements exempting "/" did not cover POST to the collection
// path itself and stayed at 419).
func TestResolveModuleCSRFExemption_CoversCollectionPost(t *testing.T) {
	for _, object := range []string{"/", ""} {
		got := resolveModuleCSRFPath("/api/v1/announcements", object)
		if got != "/api/v1/announcements" {
			t.Errorf("object %q must resolve to the bare prefix so the collection POST is covered, got %q", object, got)
		}
	}
	if got := resolveModuleCSRFPath("/api/v1/announcements", "/webhook"); got != "/api/v1/announcements/webhook" {
		t.Errorf("an explicit sub-path must be preserved, got %q", got)
	}
}

// The dangerous case: a module WITHOUT Prefix declaring "/" resolves to
// "/" and turns CSRF off for the entire application — including its
// sibling modules. Mounting a module is trusting its routes, which the ADR
// accepts; extending that trust to unprotect everyone else is not the same
// trade, and it happened with no veto and no log line.
func TestModuleCSRFExemption_AppWideIsRejected(t *testing.T) {
	err := validateCSRFExemption("goloso", "", "/")
	if err == nil {
		t.Fatal("an exemption that resolves to \"/\" disables CSRF for every module in the app; it must be rejected")
	}
	for _, want := range []string{"goloso", "CSRF"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error must name the module and what it would disable (%q), got: %v", want, err)
		}
	}

	// The same declaration is legitimate for a module that owns a prefix.
	if err := validateCSRFExemption("consola", "/consola", "/"); err != nil {
		t.Errorf("a prefixed module exempting its own surface is the documented case: %v", err)
	}
}

// End-to-end over the public Run surface with defaults ON, reproducing the
// external demo's case: a module mounted under a Prefix that declares its
// own surface once. Before QCD-FW-13 the subtree answered 200 while the
// module's ROOT answered 403, and the demo carried a programmatic
// enf.AddPolicy per module to paper over it.
func TestRun_ModuleDeclaresItsOwnRoot(t *testing.T) {
	port := freeLocalPort(t)

	modDef := Module[struct{}]{
		Name:   "consola",
		Prefix: "/consola",
		Policies: []PolicyRule{
			{Subject: "anonymous", Object: "/", Action: "*"},
		},
		CSRFExempt: []string{"/"},
		Routes: func(r Router, _ struct{}) {
			r.Get("/", func(c *Context) error { return c.JSON(http.StatusOK, map[string]string{"page": "root"}) })
			r.Get("/lista", func(c *Context) error { return c.JSON(http.StatusOK, map[string]string{"page": "list"}) })
			r.Post("/", func(c *Context) error { return c.JSON(http.StatusOK, map[string]string{"posted": "root"}) })
		},
	}

	cfg := app.DefaultConfig()
	cfg.Host = "127.0.0.1"
	cfg.Port = port
	cfg.CSRFEnabled = true
	cfg.Databases = map[string]app.DatabaseConfig{
		"default": {URL: "sqlite://" + filepath.Join(t.TempDir(), "root.db")},
	}

	runDone := make(chan error, 1)
	go func() {
		runDone <- Run(App{
			Config:  cfg,
			Modules: map[string]ModuleSpec{"consola": modDef.Build()},
		})
	}()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 2 * time.Second}
	waitForServer(t, client, base, runDone)

	for path, label := range map[string]string{
		"/consola":       "the module's own root",
		"/consola/lista": "a route under the module",
	} {
		resp, err := client.Get(base + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s (%s): want 200 from the module's own declaration, got %d", path, label, resp.StatusCode)
		}
	}

	// The collection POST on the prefix itself — the case a trailing slash
	// left at 419.
	resp, err := client.Post(base+"/consola", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST /consola: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("POST /consola: want 200 (the module exempted its own surface), got %d", resp.StatusCode)
	}
}
