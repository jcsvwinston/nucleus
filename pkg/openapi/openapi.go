package openapi

import (
	"encoding/json"
	"fmt"
	"io"
)

// Document is a small OpenAPI 3.1 document model used by generated contracts.
// It intentionally covers the subset GoFrame scaffolds need today.
type Document struct {
	OpenAPI    string              `json:"openapi"`
	Info       Info                `json:"info"`
	Servers    []Server            `json:"servers,omitempty"`
	Paths      map[string]PathItem `json:"paths,omitempty"`
	Components Components          `json:"components,omitempty"`
}

type Info struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

type Server struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

type Components struct {
	Schemas map[string]Schema `json:"schemas,omitempty"`
}

type PathItem struct {
	Get    *Operation `json:"get,omitempty"`
	Post   *Operation `json:"post,omitempty"`
	Put    *Operation `json:"put,omitempty"`
	Delete *Operation `json:"delete,omitempty"`
}

type Operation struct {
	OperationID string              `json:"operationId,omitempty"`
	Summary     string              `json:"summary,omitempty"`
	Description string              `json:"description,omitempty"`
	Tags        []string            `json:"tags,omitempty"`
	Parameters  []Parameter         `json:"parameters,omitempty"`
	RequestBody *RequestBody        `json:"requestBody,omitempty"`
	Responses   map[string]Response `json:"responses"`
}

type Parameter struct {
	Name        string `json:"name"`
	In          string `json:"in"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Schema      Schema `json:"schema"`
}

type RequestBody struct {
	Required bool                 `json:"required,omitempty"`
	Content  map[string]MediaType `json:"content"`
}

type Response struct {
	Description string               `json:"description"`
	Content     map[string]MediaType `json:"content,omitempty"`
}

type MediaType struct {
	Schema Schema `json:"schema"`
}

type Schema struct {
	Ref                  string            `json:"$ref,omitempty"`
	Type                 string            `json:"type,omitempty"`
	Format               string            `json:"format,omitempty"`
	Description          string            `json:"description,omitempty"`
	Properties           map[string]Schema `json:"properties,omitempty"`
	Items                *Schema           `json:"items,omitempty"`
	Required             []string          `json:"required,omitempty"`
	AdditionalProperties *Schema           `json:"additionalProperties,omitempty"`
}

func NewDocument(title, version string) *Document {
	return &Document{
		OpenAPI: "3.1.0",
		Info: Info{
			Title:   title,
			Version: version,
		},
		Paths: map[string]PathItem{},
		Components: Components{
			Schemas: map[string]Schema{},
		},
	}
}

func (d *Document) EnsurePaths() {
	if d == nil {
		return
	}
	if d.Paths == nil {
		d.Paths = map[string]PathItem{}
	}
}

func (d *Document) EnsureComponents() {
	if d == nil {
		return
	}
	if d.Components.Schemas == nil {
		d.Components.Schemas = map[string]Schema{}
	}
}

func (d *Document) AddSchema(name string, schema Schema) {
	if d == nil {
		return
	}
	d.EnsureComponents()
	d.Components.Schemas[name] = schema
}

func RefSchema(name string) Schema {
	return Schema{Ref: "#/components/schemas/" + name}
}

func IDSchema() Schema {
	return Schema{Type: "integer", Format: "int64"}
}

func ArraySchema(items Schema) Schema {
	item := items
	return Schema{
		Type:  "array",
		Items: &item,
	}
}

func ObjectSchema(properties map[string]Schema, required ...string) Schema {
	schema := Schema{
		Type:       "object",
		Properties: properties,
	}
	if len(required) > 0 {
		schema.Required = append([]string(nil), required...)
	}
	return schema
}

func DataEnvelopeSchema(data Schema) Schema {
	return ObjectSchema(map[string]Schema{
		"data": data,
	}, "data")
}

func CollectionEnvelopeSchema(item Schema) Schema {
	return ObjectSchema(map[string]Schema{
		"data":  ArraySchema(item),
		"count": {Type: "integer"},
	}, "data", "count")
}

func JSONContent(schema Schema) map[string]MediaType {
	return map[string]MediaType{
		"application/json": {Schema: schema},
	}
}

func JSONRequestBody(schema Schema, required bool) *RequestBody {
	return &RequestBody{
		Required: required,
		Content:  JSONContent(schema),
	}
}

func JSONResponse(description string, schema Schema) Response {
	return Response{
		Description: description,
		Content:     JSONContent(schema),
	}
}

func EmptyResponse(description string) Response {
	return Response{Description: description}
}

func ErrorSchema() Schema {
	anyDetails := Schema{}
	return ObjectSchema(map[string]Schema{
		"error": ObjectSchema(map[string]Schema{
			"code":    {Type: "string"},
			"message": {Type: "string"},
			"details": {
				Type:                 "object",
				AdditionalProperties: &anyDetails,
			},
		}, "code", "message"),
	}, "error")
}

func ErrorResponse(description string) Response {
	return JSONResponse(description, ErrorSchema())
}

func PathParameter(name string, schema Schema, description string) Parameter {
	return Parameter{
		Name:        name,
		In:          "path",
		Description: description,
		Required:    true,
		Schema:      schema,
	}
}

func QueryParameter(name string, schema Schema, description string, required bool) Parameter {
	return Parameter{
		Name:        name,
		In:          "query",
		Description: description,
		Required:    required,
		Schema:      schema,
	}
}

func Marshal(doc *Document) ([]byte, error) {
	if doc == nil {
		return nil, fmt.Errorf("openapi: document is nil")
	}
	return json.MarshalIndent(doc, "", "  ")
}

func WriteJSON(w io.Writer, doc *Document) error {
	if w == nil {
		return fmt.Errorf("openapi: writer is nil")
	}
	body, err := Marshal(doc)
	if err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}
