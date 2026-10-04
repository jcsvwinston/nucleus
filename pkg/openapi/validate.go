// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package openapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// The validator checks values, requests and responses against the
// document. It is written here, in Go, against the subset of JSON Schema
// this package models — type (with the 3.1 null), enum, properties,
// required, additionalProperties, items, the numeric bounds, the length
// bounds, pattern, the item counts, $ref into components, and the formats
// SchemaFor writes (date-time, date, email, uri, uuid, ipv4, ipv6,
// hostname, int32, int64, byte). A keyword outside that subset cannot be
// carried by this model at all, so nothing silently half-understands it:
// a document that needs oneOf or allOf is not one this package describes.

// ValidationError is one way a value departs from the document: where (a
// JSON pointer into the value, or the parameter's location and name) and
// what.
type ValidationError struct {
	// Field is the JSON pointer of the offending value inside the body
	// (/author/name, /tags/2), or "query.page", "path.id", "header.X-Tenant"
	// for a parameter, or "body" for the body as a whole.
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e ValidationError) String() string {
	if e.Field == "" {
		return e.Message
	}
	return e.Field + ": " + e.Message
}

// ValidationErrors is a list of departures; its Error joins them.
type ValidationErrors []ValidationError

func (es ValidationErrors) Error() string {
	parts := make([]string, len(es))
	for i, e := range es {
		parts[i] = e.String()
	}
	return strings.Join(parts, "; ")
}

// Validate checks a decoded JSON value (what encoding/json produces into an
// any, numbers preferably as json.Number) against schema, resolving $ref in
// d's components. It returns every departure, not just the first.
func (d *Document) Validate(schema Schema, value any) ValidationErrors {
	v := validator{doc: d}
	v.value(schema, value, "")
	return v.errs
}

// ValidateJSON decodes raw and validates it. A body that is not JSON is one
// error at "body".
func (d *Document) ValidateJSON(schema Schema, raw []byte) ValidationErrors {
	value, err := decodeJSON(raw)
	if err != nil {
		return ValidationErrors{{Field: "body", Message: "not valid JSON: " + err.Error()}}
	}
	return d.Validate(schema, value)
}

func decodeJSON(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, fmt.Errorf("trailing data after the JSON value")
	}
	return value, nil
}

type validator struct {
	doc   *Document
	errs  ValidationErrors
	depth int
}

func (v *validator) fail(at, format string, args ...any) {
	if at == "" {
		at = "/"
	}
	v.errs = append(v.errs, ValidationError{Field: at, Message: fmt.Sprintf(format, args...)})
}

// resolve follows a $ref into the document's components. An unresolvable
// reference is reported and the empty schema (anything) is used, so one
// broken reference does not hide the rest of the value's errors.
func (v *validator) resolve(s Schema, at string) (Schema, bool) {
	for i := 0; s.Ref != "" && i < 32; i++ {
		name, ok := strings.CutPrefix(s.Ref, "#/components/schemas/")
		if !ok || v.doc == nil {
			v.fail(at, "unresolvable reference %q", s.Ref)
			return Schema{}, false
		}
		target, ok := v.doc.Components.Schemas[name]
		if !ok {
			v.fail(at, "reference to an undeclared schema %q", name)
			return Schema{}, false
		}
		s = target
	}
	return s, true
}

func (v *validator) value(s Schema, value any, at string) {
	v.depth++
	defer func() { v.depth-- }()
	if v.depth > 64 {
		v.fail(at, "value nested too deep to validate")
		return
	}
	s, ok := v.resolve(s, at)
	if !ok {
		return
	}
	if value == nil {
		if s.Type != "" && !s.Nullable {
			v.fail(at, "must be %s, got null", article(s.Type))
		}
		return
	}
	if len(s.Enum) > 0 && !enumContains(s.Enum, value) {
		v.fail(at, "must be one of %s", enumList(s.Enum))
	}
	switch s.Type {
	case "":
		// The empty schema admits anything; a typeless schema with
		// properties still checks them when the value is an object.
		if obj, ok := value.(map[string]any); ok && (len(s.Properties) > 0 || len(s.Required) > 0) {
			v.object(s, obj, at)
		}
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			v.fail(at, "must be an object, got %s", jsonKind(value))
			return
		}
		v.object(s, obj, at)
	case "array":
		arr, ok := value.([]any)
		if !ok {
			v.fail(at, "must be an array, got %s", jsonKind(value))
			return
		}
		if s.MinItems != nil && len(arr) < *s.MinItems {
			v.fail(at, "must have at least %d items, has %d", *s.MinItems, len(arr))
		}
		if s.MaxItems != nil && len(arr) > *s.MaxItems {
			v.fail(at, "must have at most %d items, has %d", *s.MaxItems, len(arr))
		}
		if s.Items != nil {
			for i, item := range arr {
				v.value(*s.Items, item, at+"/"+strconv.Itoa(i))
			}
		}
	case "string":
		str, ok := value.(string)
		if !ok {
			v.fail(at, "must be a string, got %s", jsonKind(value))
			return
		}
		v.str(s, str, at)
	case "integer", "number":
		n, ok := number(value)
		if !ok {
			v.fail(at, "must be %s, got %s", article(s.Type), jsonKind(value))
			return
		}
		if s.Type == "integer" && n != math.Trunc(n) {
			v.fail(at, "must be an integer, got %v", value)
			return
		}
		v.num(s, n, at)
	case "boolean":
		if _, ok := value.(bool); !ok {
			v.fail(at, "must be a boolean, got %s", jsonKind(value))
		}
	default:
		// A type name outside JSON Schema: say so rather than pass it.
		v.fail(at, "the document declares an unknown type %q", s.Type)
	}
}

func (v *validator) object(s Schema, obj map[string]any, at string) {
	for _, name := range s.Required {
		if _, ok := obj[name]; !ok {
			v.fail(at+"/"+escapePointer(name), "is required")
		}
	}
	names := make([]string, 0, len(obj))
	for name := range obj {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		child := at + "/" + escapePointer(name)
		if ps, ok := s.Properties[name]; ok {
			v.value(ps, obj[name], child)
			continue
		}
		if s.AdditionalProperties != nil {
			v.value(*s.AdditionalProperties, obj[name], child)
		}
	}
}

var (
	patternCache sync.Map // string → *regexp.Regexp (or error)
	uuidPattern  = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	hostPattern  = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)
)

func (v *validator) str(s Schema, str, at string) {
	n := utf8.RuneCountInString(str)
	if s.MinLength != nil && n < *s.MinLength {
		v.fail(at, "must be at least %d characters, is %d", *s.MinLength, n)
	}
	if s.MaxLength != nil && n > *s.MaxLength {
		v.fail(at, "must be at most %d characters, is %d", *s.MaxLength, n)
	}
	if s.Pattern != "" {
		re, err := compiledPattern(s.Pattern)
		switch {
		case err != nil:
			v.fail(at, "the document's pattern %q does not compile: %v", s.Pattern, err)
		case !re.MatchString(str):
			v.fail(at, "must match the pattern %s", s.Pattern)
		}
	}
	if msg := checkFormat(s.Format, str); msg != "" {
		v.fail(at, "%s", msg)
	}
}

func compiledPattern(p string) (*regexp.Regexp, error) {
	if cached, ok := patternCache.Load(p); ok {
		if re, ok := cached.(*regexp.Regexp); ok {
			return re, nil
		}
		return nil, cached.(error)
	}
	re, err := regexp.Compile(p)
	if err != nil {
		patternCache.Store(p, err)
		return nil, err
	}
	patternCache.Store(p, re)
	return re, nil
}

// checkFormat answers "" when str is in the format, else what is wrong. An
// unknown format is an annotation, as JSON Schema says it is by default.
func checkFormat(format, str string) string {
	switch format {
	case "date-time":
		if _, err := time.Parse(time.RFC3339Nano, str); err != nil {
			return "must be an RFC 3339 date-time"
		}
	case "date":
		if _, err := time.Parse(time.DateOnly, str); err != nil {
			return "must be a date (YYYY-MM-DD)"
		}
	case "email":
		if a, err := mail.ParseAddress(str); err != nil || a.Address != str {
			return "must be an email address"
		}
	case "uri":
		if u, err := url.Parse(str); err != nil || u.Scheme == "" {
			return "must be an absolute URI"
		}
	case "uuid":
		if !uuidPattern.MatchString(str) {
			return "must be a UUID"
		}
	case "ipv4":
		if ip := net.ParseIP(str); ip == nil || ip.To4() == nil || strings.Contains(str, ":") {
			return "must be an IPv4 address"
		}
	case "ipv6":
		if ip := net.ParseIP(str); ip == nil || !strings.Contains(str, ":") {
			return "must be an IPv6 address"
		}
	case "hostname":
		if len(str) > 253 || !hostPattern.MatchString(str) {
			return "must be a hostname"
		}
	case "byte":
		if _, err := base64.StdEncoding.DecodeString(str); err != nil {
			return "must be base64"
		}
	}
	return ""
}

func (v *validator) num(s Schema, n float64, at string) {
	if s.Minimum != nil && n < *s.Minimum {
		v.fail(at, "must be ≥ %v", *s.Minimum)
	}
	if s.Maximum != nil && n > *s.Maximum {
		v.fail(at, "must be ≤ %v", *s.Maximum)
	}
	if s.ExclusiveMinimum != nil && n <= *s.ExclusiveMinimum {
		v.fail(at, "must be > %v", *s.ExclusiveMinimum)
	}
	if s.ExclusiveMaximum != nil && n >= *s.ExclusiveMaximum {
		v.fail(at, "must be < %v", *s.ExclusiveMaximum)
	}
	switch s.Format {
	case "int32":
		if n < math.MinInt32 || n > math.MaxInt32 {
			v.fail(at, "must fit in 32 bits")
		}
	case "int64":
		if n < math.MinInt64 || n > math.MaxInt64 {
			v.fail(at, "must fit in 64 bits")
		}
	}
}

func number(value any) (float64, bool) {
	switch n := value.(type) {
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

func enumContains(enum []any, value any) bool {
	for _, e := range enum {
		if jsonEqual(e, value) {
			return true
		}
	}
	return false
}

// jsonEqual compares two values as JSON: numbers by value whatever their
// Go type (an enum decoded from a document holds float64, a value decoded
// with UseNumber holds json.Number, SchemaFor writes int64).
func jsonEqual(a, b any) bool {
	if na, ok := number(a); ok {
		nb, ok := number(b)
		return ok && na == nb
	}
	if na, ok := toInt64(a); ok {
		if nb, ok := number(b); ok {
			return float64(na) == nb
		}
	}
	return fmt.Sprint(a) == fmt.Sprint(b) && jsonKind(a) == jsonKind(b)
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int32:
		return int64(n), true
	case uint64:
		return int64(n), true
	}
	return 0, false
}

func enumList(enum []any) string {
	parts := make([]string, len(enum))
	for i, e := range enum {
		raw, _ := json.Marshal(e)
		parts[i] = string(raw)
	}
	return strings.Join(parts, ", ")
}

func jsonKind(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case map[string]any:
		return "an object"
	case []any:
		return "an array"
	case string:
		return "a string"
	case bool:
		return "a boolean"
	case json.Number, float64, float32, int, int64:
		return "a number"
	}
	return fmt.Sprintf("%T", v)
}

func article(typ string) string {
	switch typ {
	case "integer", "object", "array":
		return "an " + typ
	}
	return "a " + typ
}

func escapePointer(s string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(s)
}

// Route is one operation of the document matched against a concrete
// request path.
type Route struct {
	// Template is the document's path ("/articles/{id}").
	Template  string
	Operation *Operation
	// Params holds the values the template's {name} segments captured.
	Params map[string]string
}

// FindOperation matches a method and a concrete path ("/articles/42")
// against the document's paths. A path with fewer parameters wins over one
// with more, so /articles/new is found before /articles/{id}.
func (d *Document) FindOperation(method, path string) (Route, bool) {
	if d == nil {
		return Route{}, false
	}
	type candidate struct {
		route  Route
		params int
	}
	var best *candidate
	for tmpl, item := range d.Paths {
		op := item.Operation(method)
		if op == nil {
			continue
		}
		params, ok := matchTemplate(tmpl, path)
		if !ok {
			continue
		}
		c := candidate{route: Route{Template: tmpl, Operation: op, Params: params}, params: len(params)}
		if best == nil || c.params < best.params || (c.params == best.params && tmpl < best.route.Template) {
			best = &c
		}
	}
	if best == nil {
		return Route{}, false
	}
	return best.route, true
}

func matchTemplate(tmpl, path string) (map[string]string, bool) {
	ts := strings.Split(strings.Trim(tmpl, "/"), "/")
	ps := strings.Split(strings.Trim(path, "/"), "/")
	if len(ts) != len(ps) {
		return nil, false
	}
	params := map[string]string{}
	for i, seg := range ts {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			if ps[i] == "" {
				return nil, false
			}
			v, err := url.PathUnescape(ps[i])
			if err != nil {
				v = ps[i]
			}
			params[seg[1:len(seg)-1]] = v
			continue
		}
		if seg != ps[i] {
			return nil, false
		}
	}
	return params, true
}

// ValidateRequest checks a request against the operation route describes:
// its path, query and header parameters (converted from text to the
// parameter's type first) and its JSON body. The body is read and put
// back, so the handler still reads it whole.
func (d *Document) ValidateRequest(r *http.Request, route Route) ValidationErrors {
	v := validator{doc: d}
	op := route.Operation
	if op == nil {
		return nil
	}
	query := r.URL.Query()
	for _, p := range op.Parameters {
		at := p.In + "." + p.Name
		var raw []string
		switch p.In {
		case "path":
			if val, ok := route.Params[p.Name]; ok {
				raw = []string{val}
			}
		case "query":
			raw = query[p.Name]
		case "header":
			raw = r.Header.Values(p.Name)
		case "cookie":
			if c, err := r.Cookie(p.Name); err == nil {
				raw = []string{c.Value}
			}
		}
		if len(raw) == 0 {
			if p.Required {
				v.fail(at, "is required")
			}
			continue
		}
		schema, _ := v.resolve(p.Schema, at)
		v.value(schema, paramValue(schema, raw), at)
	}

	if op.RequestBody == nil {
		return v.errs
	}
	var body []byte
	if r.Body != nil {
		var err error
		body, err = readAndRestore(r)
		if err != nil {
			v.fail("body", "could not be read: %v", err)
			return v.errs
		}
	}
	if len(bytes.TrimSpace(body)) == 0 {
		if op.RequestBody.Required {
			v.fail("body", "is required")
		}
		return v.errs
	}
	media, ok := mediaFor(op.RequestBody.Content, r.Header.Get("Content-Type"))
	if !ok {
		v.fail("body", "content type %q is not one the operation accepts (%s)", r.Header.Get("Content-Type"), contentTypes(op.RequestBody.Content))
		return v.errs
	}
	if !isJSONMedia(media) {
		// Only JSON bodies are described by schema here; a form or a
		// file is the handler's to read.
		return v.errs
	}
	value, err := decodeJSON(body)
	if err != nil {
		v.fail("body", "not valid JSON: %v", err)
		return v.errs
	}
	v.value(op.RequestBody.Content[media].Schema, value, "")
	return v.errs
}

// ValidateResponse checks a response the operation produced: that its
// status is declared (exactly, as a range such as 2XX, or by default),
// that its content type is one the response declares, and that its JSON
// body conforms to the schema.
func (d *Document) ValidateResponse(route Route, status int, header http.Header, body []byte) ValidationErrors {
	v := validator{doc: d}
	op := route.Operation
	if op == nil {
		return nil
	}
	resp, key, ok := responseFor(op.Responses, status)
	if !ok {
		v.fail("status", "%d is not a response the operation declares (%s)", status, responseKeys(op.Responses))
		return v.errs
	}
	if len(resp.Content) == 0 {
		if len(bytes.TrimSpace(body)) > 0 {
			v.fail("body", "response %s declares no content, got %d bytes", key, len(body))
		}
		return v.errs
	}
	media, ok := mediaFor(resp.Content, header.Get("Content-Type"))
	if !ok {
		v.fail("body", "content type %q is not one response %s declares (%s)", header.Get("Content-Type"), key, contentTypes(resp.Content))
		return v.errs
	}
	if !isJSONMedia(media) {
		return v.errs
	}
	value, err := decodeJSON(body)
	if err != nil {
		v.fail("body", "not valid JSON: %v", err)
		return v.errs
	}
	v.value(resp.Content[media].Schema, value, "")
	return v.errs
}

// responseFor picks the declared response for a status: the exact code,
// then its range (2XX), then default.
func responseFor(responses map[string]Response, status int) (Response, string, bool) {
	code := strconv.Itoa(status)
	if r, ok := responses[code]; ok {
		return r, code, true
	}
	rng := code[:1] + "XX"
	for _, k := range []string{rng, strings.ToLower(rng)} {
		if r, ok := responses[k]; ok {
			return r, k, true
		}
	}
	if r, ok := responses["default"]; ok {
		return r, "default", true
	}
	return Response{}, "", false
}

func responseKeys(responses map[string]Response) string {
	keys := make([]string, 0, len(responses))
	for k := range responses {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// mediaFor finds the declared media type a Content-Type header names
// (parameters such as charset ignored); an absent header matches a lone
// declared type.
func mediaFor(content map[string]MediaType, header string) (string, bool) {
	if header == "" {
		if len(content) == 1 {
			for k := range content {
				return k, true
			}
		}
		return "", false
	}
	mt, _, err := mime.ParseMediaType(header)
	if err != nil {
		return "", false
	}
	if _, ok := content[mt]; ok {
		return mt, true
	}
	for k := range content {
		if k == "*/*" || (strings.HasSuffix(k, "/*") && strings.HasPrefix(mt, strings.TrimSuffix(k, "*"))) {
			return k, true
		}
	}
	return "", false
}

func contentTypes(content map[string]MediaType) string {
	keys := make([]string, 0, len(content))
	for k := range content {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

func isJSONMedia(mt string) bool {
	return mt == "application/json" || strings.HasSuffix(mt, "+json")
}

// paramValue converts the text of a parameter to the JSON value its schema
// describes, so the same validator checks it: "42" is the number 42 for an
// integer, "true" a boolean, repeated query keys an array. Text that does
// not convert stays a string, and the type check reports it.
func paramValue(s Schema, raw []string) any {
	if s.Type == "array" {
		items := Schema{}
		if s.Items != nil {
			items = *s.Items
		}
		var vals []string
		for _, r := range raw {
			vals = append(vals, strings.Split(r, ",")...)
		}
		out := make([]any, len(vals))
		for i, r := range vals {
			out[i] = scalarValue(items.Type, r)
		}
		return out
	}
	return scalarValue(s.Type, raw[0])
}

func scalarValue(typ, raw string) any {
	switch typ {
	case "integer", "number":
		if _, err := strconv.ParseFloat(raw, 64); err == nil {
			return json.Number(raw)
		}
	case "boolean":
		if b, err := strconv.ParseBool(raw); err == nil {
			return b
		}
	}
	return raw
}

func readAndRestore(r *http.Request) ([]byte, error) {
	var buf bytes.Buffer
	_, err := buf.ReadFrom(r.Body)
	_ = r.Body.Close()
	r.Body = readCloser{bytes.NewReader(buf.Bytes())}
	return buf.Bytes(), err
}

type readCloser struct{ *bytes.Reader }

func (readCloser) Close() error { return nil }
