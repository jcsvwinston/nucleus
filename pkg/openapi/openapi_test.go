package openapi

import (
	"bytes"
	"strings"
	"testing"
)

func TestDocument_MarshalAndWrite(t *testing.T) {
	doc := NewDocument("Demo API", "0.1.0")
	doc.AddSchema("Ping", Schema{
		Type: "object",
		Properties: map[string]Schema{
			"status": {Type: "string"},
		},
		Required: []string{"status"},
	})
	doc.Paths["/ping"] = PathItem{
		Get: &Operation{
			OperationID: "ping",
			Summary:     "Health check",
			Responses: map[string]Response{
				"200": {
					Description: "OK",
					Content:     JSONContent(RefSchema("Ping")),
				},
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

	var buf bytes.Buffer
	if err := WriteJSON(&buf, doc); err != nil {
		t.Fatalf("write json failed: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected written JSON body")
	}
}
