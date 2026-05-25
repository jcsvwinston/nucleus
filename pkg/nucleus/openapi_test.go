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
