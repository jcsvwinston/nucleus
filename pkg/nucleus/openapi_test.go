package nucleus

import (
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/openapi"
)

func testOpenAPIProvider() *openapi.Document { return &openapi.Document{} }

// TestWithOpenAPIRecordsSpec verifies the fluent builder records an OpenAPISpec
// that survives Build() (ADR-010 Phase 4, Slice 2).
func TestWithOpenAPIRecordsSpec(t *testing.T) {
	a, err := New().
		WithOpenAPI("/openapi.json", testOpenAPIProvider).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if a.OpenAPI == nil {
		t.Fatal("App.OpenAPI is nil; WithOpenAPI should have recorded a spec")
	}
	if a.OpenAPI.Pattern != "/openapi.json" {
		t.Fatalf("OpenAPI.Pattern = %q, want /openapi.json", a.OpenAPI.Pattern)
	}
	if a.OpenAPI.Provider == nil {
		t.Fatal("OpenAPI.Provider is nil; want the supplied provider")
	}
}

// TestWithOpenAPINilProviderIsError verifies a nil provider records a deferred
// builder error surfaced at Build.
func TestWithOpenAPINilProviderIsError(t *testing.T) {
	_, err := New().WithOpenAPI("/openapi.json", nil).Build()
	if err == nil {
		t.Fatal("WithOpenAPI(nil) should record a builder error")
	}
}

// TestWithOpenAPILastWins verifies a second WithOpenAPI replaces the first.
func TestWithOpenAPILastWins(t *testing.T) {
	a, err := New().
		WithOpenAPI("/v1.json", testOpenAPIProvider).
		WithOpenAPI("/v2.json", testOpenAPIProvider).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if a.OpenAPI == nil || a.OpenAPI.Pattern != "/v2.json" {
		t.Fatalf("OpenAPI spec = %+v, want Pattern /v2.json (last-wins)", a.OpenAPI)
	}
}

// TestWithOpenAPIHandlerRecordsSpec verifies the stdlib-first setter records
// an OpenAPISpec carrying the handler (DEP-2026-008) that survives Build().
func TestWithOpenAPIHandlerRecordsSpec(t *testing.T) {
	a, err := New().
		WithOpenAPIHandler("/openapi.json", openapi.Handler(testOpenAPIProvider)).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if a.OpenAPI == nil {
		t.Fatal("App.OpenAPI is nil; WithOpenAPIHandler should have recorded a spec")
	}
	if a.OpenAPI.Pattern != "/openapi.json" {
		t.Fatalf("OpenAPI.Pattern = %q, want /openapi.json", a.OpenAPI.Pattern)
	}
	if a.OpenAPI.Handler == nil {
		t.Fatal("OpenAPI.Handler is nil; want the supplied handler")
	}
	if a.OpenAPI.Provider != nil {
		t.Fatal("OpenAPI.Provider should stay nil on the handler path")
	}
}

// TestWithOpenAPIHandlerNilIsError verifies a nil handler records a deferred
// builder error surfaced at Build.
func TestWithOpenAPIHandlerNilIsError(t *testing.T) {
	_, err := New().WithOpenAPIHandler("/openapi.json", nil).Build()
	if err == nil {
		t.Fatal("WithOpenAPIHandler(nil) should record a builder error")
	}
}

// TestWithOpenAPIHandlerReplacesProviderSpec verifies the two setters share
// last-wins semantics across each other: the handler spec replaces a
// previously recorded provider spec wholesale.
func TestWithOpenAPIHandlerReplacesProviderSpec(t *testing.T) {
	a, err := New().
		WithOpenAPI("/v1.json", testOpenAPIProvider).
		WithOpenAPIHandler("/v2.json", openapi.Handler(testOpenAPIProvider)).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if a.OpenAPI == nil || a.OpenAPI.Pattern != "/v2.json" || a.OpenAPI.Handler == nil || a.OpenAPI.Provider != nil {
		t.Fatalf("OpenAPI spec = %+v, want handler-only spec at /v2.json (last-wins)", a.OpenAPI)
	}
}
