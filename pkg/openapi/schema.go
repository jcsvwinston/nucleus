// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package openapi

import (
	"encoding"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// SchemaOf returns the schema of T as encoding/json writes it, registering
// every named struct type it reaches under the document's
// components.schemas and answering a $ref to it. It is how a handler's
// request and response types become the contract without writing them a
// second time as hand-built schemas:
//
//	type Article struct {
//	    ID    int64  `json:"id"`
//	    Title string `json:"title" validate:"required,max=200"`
//	    Body  string `json:"body,omitempty"`
//	}
//	ref := openapi.SchemaOf[Article](doc) // {"$ref": "#/components/schemas/Article"}
//
// The rules follow encoding/json, so the schema describes the bytes the
// handler actually sends:
//
//   - The property name is the json tag's name, or the Go field name when
//     there is no tag; `json:"-"` and unexported fields are left out, and
//     embedded structs are flattened the way encoding/json flattens them.
//   - A field is required unless its json tag says omitempty (or
//     omitzero), it is a pointer, or its validate tag says omitempty;
//     `validate:"required"` makes it required regardless. A slice or map
//     without omitempty is required and nullable — encoding/json writes
//     null for a nil one.
//   - A pointer is nullable. time.Time is a date-time string, []byte a
//     base64 string, a type that implements encoding.TextMarshaler a
//     string (uuid.UUID carries format uuid), a map an object with
//     additionalProperties, an interface the empty schema.
//   - validate tags become constraints: min/max/len (length for strings,
//     items for slices, bounds for numbers), gt/gte/lt/lte, oneof (enum),
//     email, url/uri, uuid, ipv4/ipv6, hostname; after `dive` they apply to
//     the elements. A `doc:"…"` tag becomes the description.
//
// A type that implements json.Marshaler writes what it decides, which no
// reflection can know: it gets the empty schema and a description saying
// so, rather than a guess.
func SchemaOf[T any](doc *Document) Schema {
	return doc.SchemaFor(reflect.TypeOf((*T)(nil)).Elem())
}

// SchemaFor is SchemaOf for a reflect.Type, for callers that hold a type
// rather than a type parameter (a framework that recorded a handler's
// input type at registration, for instance). A nil document still returns
// the schema; named structs are then inlined instead of referenced.
func (d *Document) SchemaFor(t reflect.Type) Schema {
	if t == nil {
		return Schema{}
	}
	g := schemaGen{doc: d}
	return g.schema(t, true)
}

// schemaGen carries the per-call state of one SchemaFor: the document whose
// components receive the named structs, and the struct types currently
// being expanded inline (a nil document cannot reference, so a recursive
// type is cut there with the empty schema).
type schemaGen struct {
	doc      *Document
	inlining map[reflect.Type]bool
}

var (
	timeType          = reflect.TypeOf(time.Time{})
	durationType      = reflect.TypeOf(time.Duration(0))
	rawMessageType    = reflect.TypeOf(json.RawMessage(nil))
	jsonMarshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	textMarshalerType = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
)

func (g *schemaGen) schema(t reflect.Type, top bool) Schema {
	for t.Kind() == reflect.Pointer {
		s := g.schema(t.Elem(), top)
		if s.Ref == "" {
			s.Nullable = true
		}
		return s
	}

	switch {
	case t == timeType:
		return Schema{Type: "string", Format: "date-time"}
	case t == durationType:
		return Schema{Type: "integer", Format: "int64", Description: "duration in nanoseconds"}
	case t == rawMessageType:
		return Schema{}
	case implements(t, jsonMarshalerType):
		return Schema{Description: "custom JSON encoding (" + t.String() + "): shape not derivable from the Go type"}
	case implements(t, textMarshalerType):
		s := Schema{Type: "string"}
		if t.PkgPath() == "github.com/google/uuid" && t.Name() == "UUID" {
			s.Format = "uuid"
		}
		return s
	}

	switch t.Kind() {
	case reflect.Bool:
		return Schema{Type: "boolean"}
	case reflect.Int, reflect.Int64:
		return Schema{Type: "integer", Format: "int64"}
	case reflect.Int8, reflect.Int16, reflect.Int32:
		return Schema{Type: "integer", Format: "int32"}
	case reflect.Uint, reflect.Uint64, reflect.Uintptr:
		return Schema{Type: "integer", Format: "int64", Minimum: float(0)}
	case reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return Schema{Type: "integer", Format: "int32", Minimum: float(0)}
	case reflect.Float32:
		return Schema{Type: "number", Format: "float"}
	case reflect.Float64:
		return Schema{Type: "number", Format: "double"}
	case reflect.String:
		return Schema{Type: "string"}
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 && !implements(t.Elem(), jsonMarshalerType) && !implements(t.Elem(), textMarshalerType) {
			return Schema{Type: "string", Format: "byte"}
		}
		items := g.schema(t.Elem(), false)
		return Schema{Type: "array", Items: &items}
	case reflect.Array:
		items := g.schema(t.Elem(), false)
		n := t.Len()
		return Schema{Type: "array", Items: &items, MinItems: &n, MaxItems: &n}
	case reflect.Map:
		values := g.schema(t.Elem(), false)
		return Schema{Type: "object", AdditionalProperties: &values}
	case reflect.Interface:
		return Schema{}
	case reflect.Struct:
		return g.structSchema(t)
	}
	// Channels, funcs and complex numbers have no JSON form: encoding/json
	// refuses them, so there is nothing to describe.
	return Schema{}
}

func implements(t, iface reflect.Type) bool {
	return t.Implements(iface) || reflect.PointerTo(t).Implements(iface)
}

func float(f float64) *float64 { return &f }

// structSchema answers a $ref for a named struct (registering it once) and
// the inline object for an anonymous one.
func (g *schemaGen) structSchema(t reflect.Type) Schema {
	if t.Name() == "" || g.doc == nil {
		if g.inlining[t] {
			return Schema{Description: "recursive reference to " + t.String()}
		}
		if g.inlining == nil {
			g.inlining = map[reflect.Type]bool{}
		}
		g.inlining[t] = true
		defer delete(g.inlining, t)
		return g.objectSchema(t)
	}
	name, known := g.doc.componentName(t)
	if !known {
		// Reserve the name before walking the fields, so a type that
		// refers to itself (a tree, a linked list) finds the reference.
		g.doc.AddSchema(name, Schema{})
		g.doc.Components.Schemas[name] = g.objectSchema(t)
	}
	return RefSchema(name)
}

// componentName returns the component name a struct type is registered
// under, and whether it already was. The Go type name is the default; a
// second, different type with the same name (two packages each with an
// Article) is qualified with its package's last path element.
func (d *Document) componentName(t reflect.Type) (string, bool) {
	if d.typeNames == nil {
		d.typeNames = map[reflect.Type]string{}
	}
	if name, ok := d.typeNames[t]; ok {
		return name, true
	}
	base := sanitizeComponentName(t.Name())
	name := base
	if _, taken := d.Components.Schemas[name]; taken {
		pkg := t.PkgPath()
		if i := strings.LastIndex(pkg, "/"); i >= 0 {
			pkg = pkg[i+1:]
		}
		name = sanitizeComponentName(pkg) + base
		for n := 2; ; n++ {
			if _, taken := d.Components.Schemas[name]; !taken {
				break
			}
			name = sanitizeComponentName(pkg) + base + strconv.Itoa(n)
		}
	}
	d.typeNames[t] = name
	return name, false
}

// sanitizeComponentName keeps a component name inside the characters
// OpenAPI allows (^[a-zA-Z0-9.\-_]+$): a generic instantiation such as
// Page[example.com/shop.Article] becomes PageArticle.
func sanitizeComponentName(name string) string {
	if i := strings.IndexByte(name, '['); i >= 0 {
		args := name[i+1 : len(name)-1]
		name = name[:i]
		for _, arg := range strings.Split(args, ",") {
			arg = strings.TrimSpace(arg)
			if j := strings.LastIndexAny(arg, "./"); j >= 0 {
				arg = arg[j+1:]
			}
			name += upperFirst(arg)
		}
	}
	var b strings.Builder
	for _, r := range name {
		if r == '.' || r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "Object"
	}
	return b.String()
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// objectSchema lists the properties encoding/json writes for a struct.
func (g *schemaGen) objectSchema(t reflect.Type) Schema {
	s := Schema{Type: "object", Properties: map[string]Schema{}}
	g.addFields(&s, t, map[string]bool{})
	if len(s.Properties) == 0 {
		s.Properties = nil
	}
	return s
}

// addFields walks t's fields into s. seen holds the names already taken by
// a shallower field: encoding/json lets the shallower one win, and so does
// this.
func (g *schemaGen) addFields(s *Schema, t reflect.Type, seen map[string]bool) {
	var embedded []reflect.Type
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts := parseJSONTag(tag)
		if f.Anonymous && name == "" {
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				embedded = append(embedded, ft)
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		if seen[name] {
			continue
		}
		seen[name] = true

		prop, required := g.field(f, opts)
		s.Properties[name] = prop
		if required {
			s.Required = append(s.Required, name)
		}
	}
	for _, et := range embedded {
		g.addFields(s, et, seen)
	}
}

// FieldSchema returns the schema of one struct field as SchemaFor writes
// it inside its struct, and whether the field is required there. It is for
// fields that are not part of a JSON body — a query parameter, a header —
// whose schema and constraints come from the same Go type and tags.
func (d *Document) FieldSchema(f reflect.StructField) (Schema, bool) {
	g := schemaGen{doc: d}
	_, opts := parseJSONTag(f.Tag.Get("json"))
	return g.field(f, opts)
}

// field is the schema of one exported field and whether it is required.
func (g *schemaGen) field(f reflect.StructField, opts map[string]bool) (Schema, bool) {
	prop := g.schema(f.Type, false)
	vtag := f.Tag.Get("validate")
	required, optionalByValidate := applyValidate(&prop, f.Type, vtag)
	if d := f.Tag.Get("doc"); d != "" {
		// On a $ref too: 3.1 allows siblings of $ref, and the
		// description then reads as the field's, which is the point.
		prop.Description = d
	}
	if opts["string"] && (prop.Type == "integer" || prop.Type == "number" || prop.Type == "boolean") {
		// `json:",string"` writes the number inside a string.
		prop = Schema{Type: "string", Description: prop.Description}
	}
	omitted := opts["omitempty"] || opts["omitzero"] || optionalByValidate || f.Type.Kind() == reflect.Pointer
	if f.Type.Kind() == reflect.Slice || f.Type.Kind() == reflect.Map {
		if !opts["omitempty"] && !opts["omitzero"] && prop.Ref == "" && !(prop.Type == "string" && prop.Format == "byte") {
			prop.Nullable = true
		}
	}
	return prop, required || !omitted
}

func parseJSONTag(tag string) (string, map[string]bool) {
	parts := strings.Split(tag, ",")
	opts := map[string]bool{}
	for _, p := range parts[1:] {
		opts[strings.TrimSpace(p)] = true
	}
	return parts[0], opts
}

// applyValidate turns the go-playground validate tag of a field into schema
// constraints. It reports whether the tag requires the field and whether it
// marks it optional (omitempty). Rules before `dive` describe the field
// itself; rules after it describe each element.
func applyValidate(s *Schema, t reflect.Type, tag string) (required, optional bool) {
	if tag == "" || tag == "-" {
		return false, false
	}
	target, tt := s, t
	for tt.Kind() == reflect.Pointer {
		tt = tt.Elem()
	}
	for _, rule := range splitValidate(tag) {
		key, arg, _ := strings.Cut(rule, "=")
		switch key {
		case "dive":
			if target.Items == nil {
				return required, optional
			}
			target = target.Items
			tt = tt.Elem()
			for tt.Kind() == reflect.Pointer {
				tt = tt.Elem()
			}
			continue
		case "required":
			if target == s {
				required = true
			}
		case "omitempty":
			if target == s {
				optional = true
			}
		case "min", "max", "len":
			n, err := strconv.ParseFloat(arg, 64)
			if err != nil {
				continue
			}
			setBound(target, tt, key, n)
		case "gt", "gte", "lt", "lte":
			n, err := strconv.ParseFloat(arg, 64)
			if err != nil {
				continue
			}
			switch key {
			case "gt":
				target.ExclusiveMinimum = float(n)
			case "gte":
				target.Minimum = float(n)
			case "lt":
				target.ExclusiveMaximum = float(n)
			case "lte":
				target.Maximum = float(n)
			}
		case "oneof":
			target.Enum = nil
			for _, v := range splitOneOf(arg) {
				target.Enum = append(target.Enum, enumValue(target.Type, v))
			}
		case "email":
			target.Format = "email"
		case "url", "uri", "http_url":
			target.Format = "uri"
		case "uuid", "uuid3", "uuid4", "uuid5", "uuid_rfc4122":
			target.Format = "uuid"
		case "ipv4":
			target.Format = "ipv4"
		case "ipv6":
			target.Format = "ipv6"
		case "hostname", "hostname_rfc1123":
			target.Format = "hostname"
		case "datetime":
			// A layout, not RFC 3339: say what it is instead of claiming
			// format date-time.
			if target.Description == "" {
				target.Description = "time in the layout " + arg
			}
		}
	}
	return required, optional
}

func setBound(s *Schema, t reflect.Type, key string, n float64) {
	switch t.Kind() {
	case reflect.String:
		v := int(n)
		switch key {
		case "min":
			s.MinLength = &v
		case "max":
			s.MaxLength = &v
		case "len":
			s.MinLength, s.MaxLength = &v, &v
		}
	case reflect.Slice, reflect.Array, reflect.Map:
		if t.Kind() == reflect.Map {
			return
		}
		v := int(n)
		switch key {
		case "min":
			s.MinItems = &v
		case "max":
			s.MaxItems = &v
		case "len":
			s.MinItems, s.MaxItems = &v, &v
		}
	default:
		switch key {
		case "min":
			s.Minimum = float(n)
		case "max":
			s.Maximum = float(n)
		case "len":
			s.Minimum, s.Maximum = float(n), float(n)
		}
	}
}

// splitValidate splits a validate tag on the commas that separate rules
// (a comma escaped as 0x2C inside an argument is not one of them).
func splitValidate(tag string) []string {
	var out []string
	for _, r := range strings.Split(tag, ",") {
		if r = strings.TrimSpace(r); r != "" {
			out = append(out, r)
		}
	}
	return out
}

// splitOneOf splits a oneof argument: values separated by spaces, a value
// with spaces in single quotes.
func splitOneOf(arg string) []string {
	var out []string
	for arg = strings.TrimSpace(arg); arg != ""; arg = strings.TrimSpace(arg) {
		if arg[0] == '\'' {
			if end := strings.IndexByte(arg[1:], '\''); end >= 0 {
				out = append(out, arg[1:end+1])
				arg = arg[end+2:]
				continue
			}
		}
		v, rest, _ := strings.Cut(arg, " ")
		out = append(out, v)
		arg = rest
	}
	return out
}

func enumValue(typ, v string) any {
	switch typ {
	case "integer":
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	case "number":
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return v
}
