// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
	routerpkg "github.com/jcsvwinston/nucleus/pkg/router"
)

// newMountCore builds the minimal container mountModule touches: router,
// logger (captured) and config with the given env.
func newMountCore(env string) (*app.App, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return &app.App{
		Logger: slog.New(slog.NewTextHandler(buf, nil)),
		Config: &app.Config{Env: env},
		Router: &routerpkg.Router{Mux: routerpkg.NewMux()},
	}, buf
}

func nopHandler(*Context) error { return nil }

// In development the boot log answers "which routes did MY modules
// register?" — the one question `nucleus routes` cannot (it builds a fresh
// app from config and never sees the binary's modules). One line per
// mounted route, naming the module (NC-06/AN-08, GF-04c).
func TestMountModuleLogsRoutesInDevelopment(t *testing.T) {
	core, buf := newMountCore("development")
	spec := Module[struct{}]{
		Name: "blog",
		Routes: func(r Router, _ struct{}) {
			r.Get("/blogs", nopHandler)
			r.Post("/blogs", nopHandler)
		},
	}.Build()

	mountModule(core, spec, nil)

	out := buf.String()
	for _, want := range []string{"module route mounted", "module=blog", "method=GET", "method=POST", "pattern=/blogs"} {
		if !strings.Contains(out, want) {
			t.Fatalf("development boot log must contain %q, got:\n%s", want, out)
		}
	}
}

// Framework routes registered before the module must not be re-attributed
// to it: only the entries the module itself added are logged.
func TestMountModuleLogsOnlyTheModulesOwnRoutes(t *testing.T) {
	core, buf := newMountCore("development")
	core.Router.Get("/healthz", func(c *routerpkg.Context) error { return nil })

	spec := Module[struct{}]{
		Name: "blog",
		Routes: func(r Router, _ struct{}) {
			r.Get("/blogs", nopHandler)
		},
	}.Build()
	mountModule(core, spec, nil)

	if strings.Contains(buf.String(), "pattern=/healthz") {
		t.Fatalf("pre-existing framework routes must not be logged as module routes:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "pattern=/blogs") {
		t.Fatalf("the module's own route must be logged:\n%s", buf.String())
	}
}

// A module mounted under a Prefix logs absolute patterns — the path a
// request (and an rbac_policy.csv row) actually uses, not the sub-router's
// relative one.
func TestMountModuleLogsPrefixedRoutesAbsolute(t *testing.T) {
	core, buf := newMountCore("development")
	spec := Module[struct{}]{
		Name:   "admin",
		Prefix: "/admin",
		Routes: func(r Router, _ struct{}) {
			r.Get("/panel", nopHandler)
		},
	}.Build()

	mountModule(core, spec, nil)

	if !strings.Contains(buf.String(), "pattern=/admin/panel") {
		t.Fatalf("prefixed module routes must be logged absolute, got:\n%s", buf.String())
	}
}

// Production boots stay quiet: the route table is a development aid, not
// operational log volume.
func TestMountModuleDoesNotLogRoutesInProduction(t *testing.T) {
	core, buf := newMountCore("production")
	spec := Module[struct{}]{
		Name: "blog",
		Routes: func(r Router, _ struct{}) {
			r.Get("/blogs", nopHandler)
		},
	}.Build()

	mountModule(core, spec, nil)

	if strings.Contains(buf.String(), "module route mounted") {
		t.Fatalf("production boot must not log the route table, got:\n%s", buf.String())
	}
}

// The routes must still work after mounting with logging in place (the
// counting walk must not disturb registration).
func TestMountModuleRoutesStillServe(t *testing.T) {
	core, _ := newMountCore("development")
	spec := Module[struct{}]{
		Name: "blog",
		Routes: func(r Router, _ struct{}) {
			r.Get("/blogs", func(c *Context) error { return c.JSON(http.StatusOK, map[string]string{"ok": "true"}) })
		},
	}.Build()
	mountModule(core, spec, nil)

	rec := newRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/blogs", nil)
	core.Router.ServeHTTP(rec, req)
	if rec.status != http.StatusOK {
		t.Fatalf("GET /blogs after mount: want 200, got %d", rec.status)
	}
}

// minimal ResponseWriter recorder (avoids importing net/http/httptest just
// for a status code).
type statusRecorder struct {
	status int
	header http.Header
	body   bytes.Buffer
}

func newRecorder() *statusRecorder {
	return &statusRecorder{header: make(http.Header)}
}

func (r *statusRecorder) Header() http.Header { return r.header }
func (r *statusRecorder) WriteHeader(s int)   { r.status = s }
func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(b)
}
