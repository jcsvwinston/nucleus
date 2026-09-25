// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package apibench

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
	"github.com/jcsvwinston/nucleus/pkg/openapi"
)

// The openapi family asks whether the API an application serves is
// DESCRIBED by a contract that comes from the code and is enforced against
// it — the listón is Goa and FastAPI: the document derives from the routes
// and the types, requests are validated against it, clients are generated
// from it, and a change that breaks it is caught before it ships.

// OA-01: an OpenAPI 3.1 document model with schemas, parameters and security.
func probeDocumentModel(t *testing.T, _ *env) verdict {
	doc := openapi.NewDocument("bench", "1.0.0")
	if doc.OpenAPI != "3.1.0" {
		t.Logf("document version %q", doc.OpenAPI)
		return partial
	}
	doc.AddSchema("Thing", openapi.ObjectSchema(map[string]openapi.Schema{"id": openapi.IDSchema()}, "id"))
	doc.AddSecurityScheme("bearer", openapi.BearerAuthScheme("JWT"))
	raw, err := openapi.Marshal(doc)
	if err != nil {
		t.Logf("marshal: %v", err)
		return partial
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return partial
	}
	return present
}

// OA-02: the application serves its document at a route.
func probeServedDocument(t *testing.T, _ *env) verdict {
	a := buildWith(t, func(b *nucleus.AppBuilder) {
		b.WithOpenAPIHandler("/openapi.json", openapi.Handler(func() *openapi.Document {
			return openapi.NewDocument("bench", "1.0.0")
		}))
	}, benchModule())
	srv := nucleustest.StartApp(t, a)
	resp, raw := doOn(t, srv, http.MethodGet, "/openapi.json", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Logf("GET /openapi.json → %d", resp.StatusCode)
		return absent
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil || m["openapi"] != "3.1.0" {
		t.Logf("body: %.200s", raw)
		return partial
	}
	return present
}

// OA-03: the document is DERIVED from the registered routes — an application
// with routes and no hand-written document still publishes them.
func probeDerivedFromRoutes(t *testing.T, e *env) verdict {
	resp, raw := e.do(t, http.MethodGet, "/openapi.json", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Logf("an application with routes and no document provider answers %d at /openapi.json: the document is whatever the author writes by hand (the scaffold's internal/contracts registrars), never read from the router", resp.StatusCode)
		return absent
	}
	var doc struct {
		Paths map[string]any `json:"paths"`
	}
	_ = json.Unmarshal(raw, &doc)
	for p := range doc.Paths {
		if p == "/bench/things" {
			return present
		}
	}
	t.Logf("a document is served but does not list the module's routes: %v", doc.Paths)
	return partial
}

// OA-04: schemas derive from Go structs.
func probeSchemaFromStruct(t *testing.T, _ *env) verdict {
	re := regexp.MustCompile(`"reflect"|SchemaOf|SchemaFor|FromStruct`)
	if files := sourceMatches(t, "pkg/openapi", re); len(files) > 0 {
		t.Logf("struct-derived schemas in %v", files)
		return present
	}
	t.Log("pkg/openapi builds schemas by hand (ObjectSchema, ArraySchema…); nothing reads a struct's fields and tags")
	return absent
}

// OA-05: requests are validated against the document.
func probeRequestValidation(t *testing.T, _ *env) verdict {
	re := regexp.MustCompile(`openapi\.Document[^\n]*Middleware|ValidateRequest|RequestValidator`)
	for _, dir := range []string{"pkg/openapi", "pkg/router", "pkg/nucleus"} {
		if files := sourceMatches(t, dir, re); len(files) > 0 {
			t.Logf("request validation against the document in %v", files)
			return present
		}
	}
	t.Log("no middleware validates a request against the OpenAPI document; validation is struct tags on whatever the handler binds (HT-01), which the document knows nothing about")
	return absent
}

// OA-06: a test can assert a response conforms to the document.
func probeResponseConformance(t *testing.T, _ *env) verdict {
	re := regexp.MustCompile(`openapi`)
	if files := sourceMatches(t, "pkg/nucleustest", re); len(files) > 0 {
		t.Logf("the kit knows the document: %v", files)
		return present
	}
	t.Log("pkg/nucleustest never reads the document: a response that drifts from the contract passes every test")
	return absent
}

// OA-07: a client is generated from the document (TypeScript first).
func probeClientGenerator(t *testing.T, _ *env) verdict {
	re := regexp.MustCompile(`(?i)typescript|generate.?client|openapi-typescript|\.ts"`)
	if files := sourceMatches(t, "internal/cli", re); len(files) > 0 {
		t.Logf("client generation in %v", files)
		return present
	}
	if files := sourceMatches(t, "internal/cli", regexp.MustCompile(`func runOpenAPI`)); len(files) > 0 {
		t.Logf("`nucleus openapi --out` exports the document (%v); nothing generates a client from it", files)
		return partial
	}
	return absent
}

// OA-08: the scaffold's document declares the application's security scheme.
func probeSecuritySchemeDeclared(t *testing.T, _ *env) verdict {
	re := regexp.MustCompile(`AddSecurityScheme|BearerAuthScheme|APIKeyScheme`)
	if files := sourceMatches(t, "internal/cli", re); len(files) > 0 {
		t.Logf("security scheme written by %v", files)
		return present
	}
	t.Log("the contracts the scaffold writes declare paths and schemas and no security scheme: the document says the API is open")
	return absent
}

// OA-09: the document is under contract control — a baseline, a freeze guard,
// something that turns a breaking change into a red check.
func probeSpecUnderContractControl(t *testing.T, _ *env) verdict {
	root := repoRoot(t)
	entries, _ := os.ReadDir(filepath.Join(root, "contracts", "baseline"))
	for _, en := range entries {
		if regexp.MustCompile(`(?i)openapi`).MatchString(en.Name()) {
			t.Logf("baseline %s", en.Name())
			return present
		}
	}
	re := regexp.MustCompile(`(?i)openapi`)
	if files := sourceMatches(t, "scripts", re); len(files) > 0 {
		t.Logf("scripts naming the document: %v (none freezes it)", files)
	}
	t.Log("contracts/baseline freezes exported symbols, CLI commands, config keys and the security posture; the OpenAPI document is not among them, so a path or a field can disappear with every check green")
	return absent
}

// OA-10: the generated application publishes its document.
func probeStarterPublishesSpec(t *testing.T, _ *env) verdict {
	cli := sourceMatches(t, "internal/cli", regexp.MustCompile(`func runOpenAPI`))
	served := sourceMatches(t, "internal/cli", regexp.MustCompile(`WithOpenAPIHandler`))
	switch {
	case len(served) > 0:
		t.Logf("the scaffold wires the document into the application: %v", served)
		return present
	case len(cli) > 0:
		t.Logf("the CLI exports the document to a file (%v); the generated application does not serve it — WithOpenAPIHandler exists in pkg/nucleus and no template calls it", cli)
		return partial
	}
	return absent
}
