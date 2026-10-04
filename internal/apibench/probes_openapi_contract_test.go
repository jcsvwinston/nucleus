// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package apibench

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
	"github.com/jcsvwinston/nucleus/pkg/openapi"
)

// The probes of the document as a contract the application is held to
// (A10 S6): enforced on requests, checked on responses from a test, and
// frozen so a breaking change turns a check red.

type benchCreate struct {
	Title string `json:"title" validate:"required,max=12"`
	Kind  string `json:"kind" validate:"oneof=a b"`
}

type benchItem struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

// contractServer boots a module whose document comes from a hand-written
// base: a POST that requires a body, and a GET whose 200 promises an item.
func contractServer(t *testing.T, validate bool, reached *int) *nucleustest.Server {
	base := openapi.NewDocument("bench", "1.0.0")
	base.Paths["/contract/items"] = openapi.PathItem{Post: &openapi.Operation{
		Parameters:  []openapi.Parameter{openapi.QueryParameter("dry", openapi.Schema{Type: "boolean"}, "", false)},
		RequestBody: openapi.JSONRequestBody(openapi.SchemaOf[benchCreate](base), true),
		Responses:   map[string]openapi.Response{"204": openapi.EmptyResponse("created"), "default": openapi.ErrorResponse("error")},
	}}
	base.Paths["/contract/items/{id}"] = openapi.PathItem{Get: &openapi.Operation{
		Responses: map[string]openapi.Response{"200": openapi.JSONResponse("item", openapi.SchemaOf[benchItem](base)), "default": openapi.ErrorResponse("error")},
	}}
	m := nucleus.Module[struct{}]{
		Name:   "contract",
		Prefix: "/contract",
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Post("/items", func(c *nucleus.Context) error { *reached++; return c.NoContent() })
			r.Get("/items/{id}", func(c *nucleus.Context) error {
				if c.Param("id") == "drift" {
					return c.JSON(http.StatusOK, map[string]any{"id": 1})
				}
				return c.JSON(http.StatusOK, benchItem{ID: 1, Title: "kept"})
			})
		},
	}.Build()
	b := nucleus.New().WithOpenAuthz().WithOpenAPIDocument("/openapi.json", base)
	if validate {
		b = b.WithOpenAPIValidation()
	}
	a, err := b.Mount(m).Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	a.Config = benchConfig(t)
	return nucleustest.StartApp(t, a)
}

// OA-05: requests are validated against the document — a request that
// departs from the declared operation is refused before the handler runs,
// naming each field; a conforming one reaches it.
func probeRequestValidation(t *testing.T, _ *env) verdict {
	reached := 0
	srv := contractServer(t, true, &reached)
	bad := srv.Post("/contract/items?dry=perhaps", map[string]any{"title": "a title far too long", "kind": "z"})
	named := bad.Status == http.StatusBadRequest
	for _, f := range []string{"query.dry", "/title", "/kind"} {
		if !strings.Contains(string(bad.Body), f) {
			named = false
		}
	}
	refused := reached == 0
	good := srv.Post("/contract/items?dry=true", map[string]any{"title": "short", "kind": "a"})
	passed := good.Status == http.StatusNoContent && reached == 1
	t.Logf("departing → %d %.200s (handler reached %v); conforming → %d", bad.Status, bad.Body, !refused, good.Status)
	switch {
	case named && refused && passed:
		return present
	case named || refused:
		return partial
	}
	return absent
}

// conformTB records a failed AssertConforms instead of failing the probe.
type conformTB struct {
	failed bool
	msg    string
}

func (c *conformTB) Helper() {}
func (c *conformTB) Fatalf(format string, args ...any) {
	c.failed, c.msg = true, fmt.Sprintf(format, args...)
}

// OA-06: a test asserts a response conforms to the document — the kit
// passes the conforming response and fails the drifted one, naming what
// drifted.
func probeResponseConformance(t *testing.T, _ *env) verdict {
	reached := 0
	srv := contractServer(t, false, &reached)
	ok := &conformTB{}
	srv.AssertConforms(ok, srv.Get("/contract/items/1"))
	drift := &conformTB{}
	srv.AssertConforms(drift, srv.Get("/contract/items/drift"))
	t.Logf("conforming: failed=%v %s; drifted: failed=%v %.300s", ok.failed, ok.msg, drift.failed, drift.msg)
	switch {
	case !ok.failed && drift.failed && strings.Contains(drift.msg, "/title"):
		return present
	case drift.failed:
		return partial
	}
	return absent
}

// OA-09: the document is under contract control — the starter's document
// is frozen in contracts/baseline, and the comparison reports a breaking
// change against it (an operation removed, a response field removed, a new
// required request field) while passing the baseline itself.
func probeSpecUnderContractControl(t *testing.T, _ *env) verdict {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "contracts", "baseline", "starter_openapi.json"))
	if err != nil {
		t.Logf("no frozen document: %v", err)
		return absent
	}
	var frozen openapi.Document
	if err := json.Unmarshal(raw, &frozen); err != nil || frozen.OpenAPI != "3.1.0" || len(frozen.Paths) == 0 {
		t.Logf("the frozen starter document is not an OpenAPI 3.1 document with paths: %v", err)
		return partial
	}
	if cs := openapi.BreakingChanges(&frozen, &frozen); len(cs) != 0 {
		t.Logf("the baseline breaks against itself: %v", cs)
		return partial
	}
	var mutated openapi.Document
	_ = json.Unmarshal(raw, &mutated)
	var removed string
	for p := range mutated.Paths {
		removed = p
		delete(mutated.Paths, p)
		break
	}
	cs := openapi.BreakingChanges(&frozen, &mutated)
	if len(cs) == 0 {
		t.Logf("removing %s from the starter's document is not reported", removed)
		return partial
	}
	t.Logf("removing %s: %v", removed, cs)
	return present
}
