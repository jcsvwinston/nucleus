// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package apibench

import (
	"encoding/json"
	"net/http"
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
