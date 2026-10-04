// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleustest_test

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
	"github.com/jcsvwinston/nucleus/pkg/openapi"
)

type conformItem struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

// recordingTB is a testingTB that records the failure instead of ending
// the test, so a test can assert that AssertConforms fails.
type recordingTB struct {
	failed bool
	msg    string
}

func (r *recordingTB) Helper() {}
func (r *recordingTB) Fatalf(format string, args ...any) {
	r.failed = true
	r.msg = fmt.Sprintf(format, args...)
}

func TestAssertConformsHoldsTheApplicationToItsDocument(t *testing.T) {
	base := openapi.NewDocument("conform", "1.0.0")
	base.Paths["/api/items/{id}"] = openapi.PathItem{Get: &openapi.Operation{
		Responses: map[string]openapi.Response{
			"200":     openapi.JSONResponse("the item", openapi.SchemaOf[conformItem](base)),
			"default": openapi.ErrorResponse("error"),
		},
	}}
	m := nucleus.Module[struct{}]{
		Name: "items",
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/api/items/{id}", func(c *nucleus.Context) error {
				if c.Param("id") == "2" {
					// Drifted: the handler stopped writing title.
					return c.JSON(http.StatusOK, map[string]any{"id": 2})
				}
				return c.JSON(http.StatusOK, conformItem{ID: 1, Title: "kept"})
			})
		},
	}.Build()
	a, err := nucleus.New().WithOpenAuthz().WithOpenAPIDocument("/openapi.json", base).Mount(m).Build()
	if err != nil {
		t.Fatal(err)
	}
	a.Config = app.DefaultConfig()
	a.Config.Env = "development"
	a.Config.Databases = map[string]app.DatabaseConfig{"default": {URL: "sqlite://" + filepath.Join(t.TempDir(), "c.db")}}
	srv := nucleustest.StartApp(t, a)

	srv.AssertConforms(t, srv.Get("/api/items/1"))

	rec := &recordingTB{}
	srv.AssertConforms(rec, srv.Get("/api/items/2"))
	if !rec.failed || !strings.Contains(rec.msg, "/title: is required") {
		t.Fatalf("a drifted response passed (failed=%v): %s", rec.failed, rec.msg)
	}

	rec = &recordingTB{}
	srv.AssertConforms(rec, srv.Get("/api/elsewhere"))
	if !rec.failed || !strings.Contains(rec.msg, "declares no operation") {
		t.Fatalf("a response to an undeclared route passed: %s", rec.msg)
	}
}

// A plain handler's operation says nothing about its answer, so any answer
// conforms; a typed endpoint's error answers in either shape the framework
// writes.
func TestAssertConformsPlainHandlersAndErrors(t *testing.T) {
	m := nucleus.Module[struct{}]{
		Name: "plain",
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/api/plain", func(c *nucleus.Context) error { return c.String(http.StatusTeapot, "anything") })
		},
	}.Build()
	a, err := nucleus.New().WithOpenAuthz().WithOpenAPIDocument("/openapi.json").Mount(m).Build()
	if err != nil {
		t.Fatal(err)
	}
	a.Config = app.DefaultConfig()
	a.Config.Env = "development"
	a.Config.Databases = map[string]app.DatabaseConfig{"default": {URL: "sqlite://" + filepath.Join(t.TempDir(), "p.db")}}
	srv := nucleustest.StartApp(t, a)
	srv.AssertConforms(t, srv.Get("/api/plain"))
}
