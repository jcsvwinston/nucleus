package nucleus

import (
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/openapi"
)

func testOpenAPIProvider() *openapi.Document { return &openapi.Document{} }

// TestWithOpenAPIHandlerRecordsSpec verifies the stdlib setter records an
// OpenAPISpec carrying the handler that survives Build() (the provider-typed
// members were removed in v0.12.0, DEP-2026-008).
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
}

// TestWithOpenAPIHandlerNilIsError verifies a nil handler records a deferred
// builder error surfaced at Build.
func TestWithOpenAPIHandlerNilIsError(t *testing.T) {
	_, err := New().WithOpenAPIHandler("/openapi.json", nil).Build()
	if err == nil {
		t.Fatal("WithOpenAPIHandler(nil) should record a builder error")
	}
}

// TestWithOpenAPIHandlerLastWins verifies a second call replaces the first.
func TestWithOpenAPIHandlerLastWins(t *testing.T) {
	a, err := New().
		WithOpenAPIHandler("/v1.json", openapi.Handler(testOpenAPIProvider)).
		WithOpenAPIHandler("/v2.json", openapi.Handler(testOpenAPIProvider)).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if a.OpenAPI == nil || a.OpenAPI.Pattern != "/v2.json" {
		t.Fatalf("OpenAPI spec = %+v, want Pattern /v2.json (last-wins)", a.OpenAPI)
	}
}
