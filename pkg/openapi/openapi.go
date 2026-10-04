package openapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
)

// Document is a small OpenAPI 3.1 document model used by generated contracts.
// It intentionally covers the subset Nucleus scaffolds need today.
type Document struct {
	OpenAPI    string                `json:"openapi"`
	Info       Info                  `json:"info"`
	Servers    []Server              `json:"servers,omitempty"`
	Security   []SecurityRequirement `json:"security,omitempty"`
	Paths      map[string]PathItem   `json:"paths,omitempty"`
	Components Components            `json:"components,omitempty"`

	// typeNames remembers which component name each Go struct type was
	// registered under by SchemaFor, so a type reached twice is referenced
	// twice instead of registered twice.
	typeNames map[reflect.Type]string
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
	Schemas         map[string]Schema         `json:"schemas,omitempty"`
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes,omitempty"`
}

// SecurityScheme is an OpenAPI 3.1 Security Scheme Object (§4.8.27). Nucleus
// models the two schemes a generated contract realistically needs — HTTP
// authentication (`type: http`, e.g. `bearer` or `basic`) and API keys
// (`type: apiKey`). oauth2 / openIdConnect flows are out of scope for the
// scaffold subset.
type SecurityScheme struct {
	Type         string `json:"type"` // "http" | "apiKey"
	Description  string `json:"description,omitempty"`
	Scheme       string `json:"scheme,omitempty"`       // type=http: "bearer" | "basic"
	BearerFormat string `json:"bearerFormat,omitempty"` // scheme=bearer: token-shape hint, e.g. "JWT"
	Name         string `json:"name,omitempty"`         // type=apiKey: request element name
	In           string `json:"in,omitempty"`           // type=apiKey: "header" | "query" | "cookie"
}

// SecurityRequirement is an OpenAPI Security Requirement Object (§4.8.30): a map
// of security-scheme name to the scope names it requires. HTTP and apiKey
// schemes take no scopes, so their requirement value is an empty slice.
type SecurityRequirement map[string][]string

type PathItem struct {
	Get    *Operation `json:"get,omitempty"`
	Post   *Operation `json:"post,omitempty"`
	Put    *Operation `json:"put,omitempty"`
	Patch  *Operation `json:"patch,omitempty"`
	Delete *Operation `json:"delete,omitempty"`
}

// Operation returns the operation the item declares for an HTTP method
// (case-insensitive), or nil.
func (p PathItem) Operation(method string) *Operation {
	switch strings.ToUpper(method) {
	case http.MethodGet:
		return p.Get
	case http.MethodPost:
		return p.Post
	case http.MethodPut:
		return p.Put
	case http.MethodPatch:
		return p.Patch
	case http.MethodDelete:
		return p.Delete
	}
	return nil
}

// SetOperation sets the operation for an HTTP method and reports whether
// the item has a slot for it (GET, POST, PUT, PATCH, DELETE).
func (p *PathItem) SetOperation(method string, op *Operation) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet:
		p.Get = op
	case http.MethodPost:
		p.Post = op
	case http.MethodPut:
		p.Put = op
	case http.MethodPatch:
		p.Patch = op
	case http.MethodDelete:
		p.Delete = op
	default:
		return false
	}
	return true
}

type Operation struct {
	OperationID string   `json:"operationId,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	// Security overrides the document's global security for this operation. A
	// nil value inherits the global default; a non-nil value (including an empty
	// slice — see PublicSecurity) replaces it. Pointer so an explicit empty
	// override survives JSON marshalling (a plain slice would be omitted).
	Security    *[]SecurityRequirement `json:"security,omitempty"`
	Parameters  []Parameter            `json:"parameters,omitempty"`
	RequestBody *RequestBody           `json:"requestBody,omitempty"`
	Responses   map[string]Response    `json:"responses"`
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

	// Nullable admits JSON null besides Type. OpenAPI 3.1 has no nullable
	// keyword: it is written as a type list, ["string", "null"], and read
	// back from one.
	Nullable bool `json:"-"`

	Enum             []any    `json:"enum,omitempty"`
	Minimum          *float64 `json:"minimum,omitempty"`
	Maximum          *float64 `json:"maximum,omitempty"`
	ExclusiveMinimum *float64 `json:"exclusiveMinimum,omitempty"`
	ExclusiveMaximum *float64 `json:"exclusiveMaximum,omitempty"`
	MinLength        *int     `json:"minLength,omitempty"`
	MaxLength        *int     `json:"maxLength,omitempty"`
	Pattern          string   `json:"pattern,omitempty"`
	MinItems         *int     `json:"minItems,omitempty"`
	MaxItems         *int     `json:"maxItems,omitempty"`
}

// schemaJSON is Schema without its methods, so MarshalJSON and
// UnmarshalJSON can delegate to encoding/json without recursing.
type schemaJSON Schema

// MarshalJSON writes Type as a list with "null" when the schema is
// Nullable — the OpenAPI 3.1 spelling of a nullable type.
func (s Schema) MarshalJSON() ([]byte, error) {
	if !s.Nullable || s.Type == "" {
		return json.Marshal(schemaJSON(s))
	}
	typ := s.Type
	s.Type = ""
	raw, err := json.Marshal(schemaJSON(s))
	if err != nil {
		return nil, err
	}
	types, _ := json.Marshal([]string{typ, "null"})
	if string(raw) == "{}" {
		return []byte(`{"type":` + string(types) + `}`), nil
	}
	return append([]byte(`{"type":`+string(types)+`,`), raw[1:]...), nil
}

// UnmarshalJSON reads Type both as a string and as a 3.1 type list; a list
// that carries "null" sets Nullable.
func (s *Schema) UnmarshalJSON(raw []byte) error {
	var probe struct {
		Type json.RawMessage `json:"type"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return err
	}
	var types []string
	listed := len(probe.Type) > 0 && probe.Type[0] == '['
	if listed {
		if err := json.Unmarshal(probe.Type, &types); err != nil {
			return err
		}
		// Decode the rest with the list removed.
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return err
		}
		delete(m, "type")
		raw, _ = json.Marshal(m)
	}
	var out schemaJSON
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	*s = Schema(out)
	if listed {
		for _, t := range types {
			if t == "null" {
				s.Nullable = true
				continue
			}
			if s.Type == "" {
				s.Type = t
			}
		}
	}
	return nil
}

// documentJSON is Document without its methods, for MarshalJSON.
type documentJSON Document

// MarshalJSON makes a *Document a json.Marshaler, which is how a stable
// surface that cannot name this experimental package takes one: the
// framework's WithOpenAPIDocument accepts base documents as
// json.Marshaler. The bytes are what Marshal writes, without the indent.
func (d *Document) MarshalJSON() ([]byte, error) {
	if d == nil {
		return []byte("null"), nil
	}
	return json.Marshal((*documentJSON)(d))
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
	// Schemas only; SecuritySchemes is initialised on demand in AddSecurityScheme.
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

// AddSecurityScheme registers a named scheme under components.securitySchemes
// (lazily creating the map). Reference it by the same name from a
// SecurityRequirement.
func (d *Document) AddSecurityScheme(name string, scheme SecurityScheme) {
	if d == nil {
		return
	}
	if d.Components.SecuritySchemes == nil {
		d.Components.SecuritySchemes = map[string]SecurityScheme{}
	}
	d.Components.SecuritySchemes[name] = scheme
}

// BearerAuthScheme returns an HTTP bearer Security Scheme. bearerFormat is an
// optional hint for the token shape (e.g. "JWT") and may be empty.
func BearerAuthScheme(bearerFormat string) SecurityScheme {
	return SecurityScheme{Type: "http", Scheme: "bearer", BearerFormat: bearerFormat}
}

// APIKeyScheme returns an apiKey Security Scheme carried in the named request
// element. in is one of "header", "query" or "cookie".
func APIKeyScheme(name, in string) SecurityScheme {
	return SecurityScheme{Type: "apiKey", Name: name, In: in}
}

// Require builds a Security Requirement naming one registered scheme, with
// optional OAuth-style scopes (empty for http/apiKey schemes). A nil scope
// slice — including a spread nil — is normalised to an empty list, the correct
// serialisation for a scheme that takes no scopes.
func Require(scheme string, scopes ...string) SecurityRequirement {
	if scopes == nil {
		scopes = []string{}
	}
	return SecurityRequirement{scheme: scopes}
}

// RequireSecurity wraps requirements into the pointer form an Operation's
// Security field takes; a non-nil value overrides the document's global
// security for that operation.
func RequireSecurity(reqs ...SecurityRequirement) *[]SecurityRequirement {
	if reqs == nil {
		reqs = []SecurityRequirement{}
	}
	return &reqs
}

// PublicSecurity returns an explicit empty security override marking an
// operation as requiring NO authentication, even when the document declares a
// global security requirement. (A nil Operation.Security inherits the global.)
// This is a contract DECLARATION only — runtime auth enforcement is the app
// middleware's responsibility, never the document's.
func PublicSecurity() *[]SecurityRequirement {
	return &[]SecurityRequirement{}
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

func SearchQueryParameter(description string) Parameter {
	return QueryParameter("q", Schema{Type: "string"}, description, false)
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
