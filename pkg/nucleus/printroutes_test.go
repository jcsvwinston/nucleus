// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/internal/routedump"
	"github.com/jcsvwinston/nucleus/pkg/app"
)

// With NUCLEUS_PRINT_ROUTES set, Run prints the route table of the binary —
// framework routes and every module's, prefix applied and attributed — and
// returns without ever listening. This is what `nucleus routes` executes.
func TestRun_PrintRoutesEnvPrintsTableAndExits(t *testing.T) {
	t.Setenv(routedump.EnvVar, "1")
	out := &syncBuffer{}
	prev := printRoutesOut
	printRoutesOut = out
	t.Cleanup(func() { printRoutesOut = prev })

	var (
		lifecycleShutdown bool
		moduleShutdown    bool
	)
	notes := Module[struct{}]{
		Name:   "notes",
		Prefix: "/api",
		Routes: func(r Router, _ struct{}) {
			r.Get("/notes", func(c *Context) error { return c.JSON(http.StatusOK, nil) })
			r.Post("/notes", func(c *Context) error { return c.JSON(http.StatusCreated, nil) })
		},
		OnShutdown: func(context.Context, Runtime, struct{}) error { moduleShutdown = true; return nil },
	}
	direct := Module[struct{}]{
		Name: "direct",
		Routes: func(r Router, _ struct{}) {
			r.Get("/direct", func(c *Context) error { return c.JSON(http.StatusOK, nil) })
		},
	}

	port := freeLocalPort(t)
	cfg := app.DefaultConfig()
	cfg.Host = "127.0.0.1"
	cfg.Port = port
	cfg.Env = "development"
	cfg.Databases = map[string]app.DatabaseConfig{
		"default": {URL: "sqlite://" + filepath.Join(t.TempDir(), "routes.db")},
	}

	runDone := make(chan error, 1)
	go func() {
		runDone <- Run(App{
			Config:    cfg,
			Modules:   map[string]ModuleSpec{"notes": notes.Build(), "direct": direct.Build()},
			Lifecycle: LifecycleHooks{OnShutdown: func(context.Context) error { lifecycleShutdown = true; return nil }},
		})
	}()

	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run with %s set must exit cleanly, got: %v", routedump.EnvVar, err)
		}
	case <-time.After(20 * time.Second):
		t.Fatalf("Run kept serving instead of printing its routes and returning")
	}

	if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond); err == nil {
		conn.Close()
		t.Fatalf("a listener is open on %d: the print-routes exit must never serve", port)
	}
	if !moduleShutdown || !lifecycleShutdown {
		t.Errorf("shutdown hooks must run on the print-routes exit (module=%v lifecycle=%v)", moduleShutdown, lifecycleShutdown)
	}

	doc, found, err := routedump.Parse([]byte(out.String()))
	if err != nil {
		t.Fatalf("parse document: %v\noutput:\n%s", err, out.String())
	}
	if !found {
		t.Fatalf("no route document on the output:\n%s", out.String())
	}
	if doc.Env != "development" {
		t.Errorf("env: got %q, want development", doc.Env)
	}

	type key struct{ method, pattern, module string }
	got := map[key]bool{}
	for _, r := range doc.Routes {
		got[key{r.Method, r.Pattern, r.Module}] = true
	}
	for _, want := range []key{
		{"GET", "/healthz", ""},
		{"GET", "/api/notes", "notes"},
		{"POST", "/api/notes", "notes"},
		{"GET", "/direct", "direct"},
	} {
		if !got[want] {
			t.Errorf("route %+v missing from the document; got %+v", want, doc.Routes)
		}
	}
	// The prefixed module's subtree mount and the direct module's own entry
	// both live on the root mux; neither may be reported twice or as the
	// framework's.
	for _, dup := range []key{{"*", "/api/*", ""}, {"GET", "/direct", ""}, {"GET", "/api/notes", ""}} {
		if got[dup] {
			t.Errorf("entry %+v must not appear (module entries are attributed once); got %+v", dup, doc.Routes)
		}
	}
}

// Without the variable, nothing changes: the application serves. Pinned so
// a future default cannot flip a production binary into print-and-exit.
func TestRun_PrintRoutesEnvOffByDefault(t *testing.T) {
	t.Setenv(routedump.EnvVar, "")
	if printRoutesRequested() {
		t.Fatal("an empty variable must not request the print-routes exit")
	}
	t.Setenv(routedump.EnvVar, "0")
	if printRoutesRequested() {
		t.Fatal("NUCLEUS_PRINT_ROUTES=0 must not request the print-routes exit")
	}
}
