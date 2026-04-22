package openapi

import (
	"bytes"
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

	param := PathParameter("id", IDSchema(), "Widget identifier")
	if param.In != "path" || !param.Required || param.Schema.Format != "int64" {
		t.Fatalf("unexpected path parameter helper output: %#v", param)
	}
}
