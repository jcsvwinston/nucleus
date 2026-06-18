package openapi

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestDocument_MarshalAndWrite(t *testing.T) {
	doc := NewDocument("Demo API", "0.1.0")
	doc.AddSchema("Ping", ObjectSchema(map[string]Schema{
		"status": {Type: "string"},
	}, "status"))
	doc.Paths["/ping"] = PathItem{
		Get: &Operation{
			OperationID: "ping",
			Summary:     "Health check",
			Description: "Returns the health probe payload.",
			Parameters: []Parameter{
				PathParameter("id", IDSchema(), "Ping identifier"),
			},
			Responses: map[string]Response{
				"200": JSONResponse("OK", RefSchema("Ping")),
			},
		},
	}

	body, err := Marshal(doc)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, `"openapi": "3.1.0"`) {
		t.Fatalf("expected openapi version in JSON: %s", text)
	}
	if !strings.Contains(text, `"/ping"`) {
		t.Fatalf("expected path in JSON: %s", text)
	}
	if !strings.Contains(text, `"#/components/schemas/Ping"`) {
		t.Fatalf("expected schema ref in JSON: %s", text)
	}
	if !strings.Contains(text, `"parameters"`) || !strings.Contains(text, `"in": "path"`) {
		t.Fatalf("expected path parameter in JSON: %s", text)
	}

	var buf bytes.Buffer
	if err := WriteJSON(&buf, doc); err != nil {
		t.Fatalf("write json failed: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected written JSON body")
	}
}

func TestHelpers(t *testing.T) {
	body := JSONRequestBody(RefSchema("Widget"), true)
	if body == nil || !body.Required {
		t.Fatalf("expected required JSON request body, got %#v", body)
	}
	if _, ok := body.Content["application/json"]; !ok {
		t.Fatalf("expected application/json content, got %#v", body.Content)
	}

	response := JSONResponse("Widget payload", ObjectSchema(map[string]Schema{
		"data": ArraySchema(RefSchema("Widget")),
	}, "data"))
	if response.Description != "Widget payload" {
		t.Fatalf("unexpected response description: %#v", response)
	}
	if got := response.Content["application/json"].Schema.Properties["data"]; got.Type != "array" {
		t.Fatalf("expected array schema helper, got %#v", got)
	}

	dataEnvelope := DataEnvelopeSchema(RefSchema("Widget"))
	if dataEnvelope.Type != "object" || len(dataEnvelope.Required) != 1 || dataEnvelope.Required[0] != "data" {
		t.Fatalf("unexpected data envelope helper output: %#v", dataEnvelope)
	}
	if got := dataEnvelope.Properties["data"]; got.Ref != "#/components/schemas/Widget" {
		t.Fatalf("expected data envelope to keep schema ref, got %#v", got)
	}

	collectionEnvelope := CollectionEnvelopeSchema(RefSchema("Widget"))
	if got := collectionEnvelope.Properties["data"]; got.Type != "array" {
		t.Fatalf("expected collection envelope data array, got %#v", got)
	}
	if got := collectionEnvelope.Properties["count"]; got.Type != "integer" {
		t.Fatalf("expected collection envelope count integer, got %#v", got)
	}

	param := PathParameter("id", IDSchema(), "Widget identifier")
	if param.In != "path" || !param.Required || param.Schema.Format != "int64" {
		t.Fatalf("unexpected path parameter helper output: %#v", param)
	}

	query := QueryParameter("q", Schema{Type: "string"}, "Search term", false)
	if query.In != "query" || query.Required {
		t.Fatalf("unexpected query parameter helper output: %#v", query)
	}

	search := SearchQueryParameter("Filter widgets")
	if search.Name != "q" || search.In != "query" || search.Schema.Type != "string" || search.Required {
		t.Fatalf("unexpected search query helper output: %#v", search)
	}

	empty := EmptyResponse("Deleted")
	if empty.Description != "Deleted" || empty.Content != nil {
		t.Fatalf("unexpected empty response helper output: %#v", empty)
	}

	errorResponse := ErrorResponse("Validation failed")
	errorSchema := errorResponse.Content["application/json"].Schema
	if errorSchema.Type != "object" {
		t.Fatalf("expected error response object schema, got %#v", errorSchema)
	}
	errorField, ok := errorSchema.Properties["error"]
	if !ok {
		t.Fatalf("expected nested error field in error schema, got %#v", errorSchema.Properties)
	}
	if _, ok := errorField.Properties["code"]; !ok {
		t.Fatalf("expected error code field, got %#v", errorField.Properties)
	}
	if _, ok := errorField.Properties["message"]; !ok {
		t.Fatalf("expected error message field, got %#v", errorField.Properties)
	}
}

func TestHandler(t *testing.T) {
	t.Run("nil provider", func(t *testing.T) {
		handler := Handler(nil)
		if handler == nil {
			t.Fatal("expected handler to be returned")
		}
	})

	t.Run("provider returns nil", func(t *testing.T) {
		handler := Handler(func() *Document {
			return nil
		})
		if handler == nil {
			t.Fatal("expected handler to be returned")
		}
	})

	t.Run("provider returns valid document", func(t *testing.T) {
		doc := NewDocument("Test", "1.0")
		handler := Handler(func() *Document {
			return doc
		})
		if handler == nil {
			t.Fatal("expected handler to be returned")
		}
	})
}

func TestHandlerFunc(t *testing.T) {
	t.Run("valid provider", func(t *testing.T) {
		doc := NewDocument("Test", "1.0")
		handlerFunc := HandlerFunc(func() *Document {
			return doc
		})
		if handlerFunc == nil {
			t.Fatal("expected handler func to be returned")
		}
	})
}

func TestEnsurePaths(t *testing.T) {
	t.Run("nil document", func(t *testing.T) {
		var doc *Document
		doc.EnsurePaths()
		// Should not panic
	})

	t.Run("document with nil paths", func(t *testing.T) {
		doc := NewDocument("Test", "1.0")
		doc.Paths = nil
		doc.EnsurePaths()
		if doc.Paths == nil {
			t.Error("expected paths to be initialized")
		}
	})

	t.Run("document with existing paths", func(t *testing.T) {
		doc := NewDocument("Test", "1.0")
		doc.Paths["/test"] = PathItem{}
		doc.EnsurePaths()
		if len(doc.Paths) != 1 {
			t.Error("expected paths to remain unchanged")
		}
	})
}

func TestEnsureComponents(t *testing.T) {
	t.Run("nil document", func(t *testing.T) {
		var doc *Document
		doc.EnsureComponents()
		// Should not panic
	})

	t.Run("document with nil schemas", func(t *testing.T) {
		doc := NewDocument("Test", "1.0")
		doc.Components.Schemas = nil
		doc.EnsureComponents()
		if doc.Components.Schemas == nil {
			t.Error("expected schemas to be initialized")
		}
	})
}

func TestAddSchema(t *testing.T) {
	t.Run("nil document", func(t *testing.T) {
		var doc *Document
		doc.AddSchema("Test", Schema{})
		// Should not panic
	})

	t.Run("valid document", func(t *testing.T) {
		doc := NewDocument("Test", "1.0")
		schema := Schema{Type: "string"}
		doc.AddSchema("Test", schema)
		if _, ok := doc.Components.Schemas["Test"]; !ok {
			t.Error("expected schema to be added")
		}
	})
}

func TestSecuritySchemesAndRequirements(t *testing.T) {
	doc := NewDocument("Secure API", "1.0.0")
	doc.AddSecurityScheme("bearerAuth", BearerAuthScheme("JWT"))
	doc.Security = []SecurityRequirement{Require("bearerAuth")}

	doc.Paths["/me"] = PathItem{
		Get: &Operation{ // nil Security → inherits the global requirement
			OperationID: "me",
			Responses:   map[string]Response{"200": EmptyResponse("OK")},
		},
	}
	doc.Paths["/token"] = PathItem{
		Post: &Operation{ // explicit empty override → public
			OperationID: "issueToken",
			Security:    PublicSecurity(),
			Responses:   map[string]Response{"200": EmptyResponse("OK")},
		},
	}

	body, err := Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	text := string(body)
	for _, want := range []string{
		`"securitySchemes"`,
		`"bearerAuth"`,
		`"type": "http"`,
		`"scheme": "bearer"`,
		`"bearerFormat": "JWT"`,
		`"security": [`, // the document-level requirement
	} {
		if !strings.Contains(text, want) {
			t.Errorf("marshalled doc missing %q:\n%s", want, text)
		}
	}

	// Round-trip to assert the override semantics survive serialisation: the
	// public operation keeps an explicit empty requirement list; the unset
	// operation inherits (no security key → nil pointer).
	var rt Document
	if err := json.Unmarshal(body, &rt); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := rt.Paths["/me"].Get.Security; got != nil {
		t.Errorf("GET /me should inherit global security (nil pointer), got %v", *got)
	}
	tok := rt.Paths["/token"].Post.Security
	if tok == nil {
		t.Fatal("POST /token should carry an explicit security override, got nil")
	}
	if len(*tok) != 0 {
		t.Errorf("POST /token public override should be an empty requirement list, got %v", *tok)
	}

	// The document-level requirement and the scheme itself round-trip too.
	if len(rt.Security) != 1 || len(rt.Security[0]["bearerAuth"]) != 0 {
		t.Errorf("document security should round-trip to one scopeless bearerAuth requirement, got %v", rt.Security)
	}
	if got := rt.Components.SecuritySchemes["bearerAuth"]; got.Type != "http" || got.Scheme != "bearer" || got.BearerFormat != "JWT" {
		t.Errorf("bearerAuth scheme should round-trip, got %#v", got)
	}
}

func TestAddSecurityScheme(t *testing.T) {
	t.Run("nil document", func(t *testing.T) {
		var doc *Document
		doc.AddSecurityScheme("bearerAuth", BearerAuthScheme("JWT"))
		// Should not panic.
	})

	t.Run("valid document", func(t *testing.T) {
		doc := NewDocument("Test", "1.0")
		doc.AddSecurityScheme("bearerAuth", BearerAuthScheme("JWT"))
		if got, ok := doc.Components.SecuritySchemes["bearerAuth"]; !ok || got.Scheme != "bearer" {
			t.Errorf("expected bearerAuth scheme registered, got %#v (ok=%v)", got, ok)
		}
	})
}

func TestSecurityHelpers(t *testing.T) {
	if s := BearerAuthScheme("JWT"); s.Type != "http" || s.Scheme != "bearer" || s.BearerFormat != "JWT" {
		t.Fatalf("unexpected bearer scheme: %#v", s)
	}
	if s := APIKeyScheme("X-API-Key", "header"); s.Type != "apiKey" || s.Name != "X-API-Key" || s.In != "header" {
		t.Fatalf("unexpected apiKey scheme: %#v", s)
	}
	if r := Require("bearerAuth"); len(r["bearerAuth"]) != 0 {
		t.Fatalf("expected no scopes for bearerAuth, got %#v", r)
	}
	if r := Require("oauth", "read", "write"); len(r["oauth"]) != 2 {
		t.Fatalf("expected two scopes, got %#v", r)
	}
	if p := PublicSecurity(); p == nil || len(*p) != 0 {
		t.Fatalf("PublicSecurity should be a non-nil empty override, got %v", p)
	}
	if s := RequireSecurity(Require("bearerAuth")); s == nil || len(*s) != 1 {
		t.Fatalf("RequireSecurity should wrap one requirement, got %v", s)
	}
}
