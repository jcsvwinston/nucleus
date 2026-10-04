// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package router

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/textproto"
	"reflect"
	"strings"
	"sync"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/validate"
)

// BindQuery binds the request's query string into v by its `query` tags,
// then validates v by its `validate` tags. v must be a non-nil pointer to a
// struct. It is the query-string counterpart of Bind: one input type per
// endpoint instead of a Query, a Param and a strconv per field in every
// handler.
//
//	type ListNotes struct {
//	    Org     string   `path:"org"`
//	    Page    int      `query:"page" validate:"omitempty,min=1"`
//	    Tags    []string `query:"tag"`              // ?tag=a&tag=b
//	    TraceID string   `header:"X-Trace-Id"`
//	}
//
// The tags the four binders read (BindQuery, BindPath, BindHeaders and
// BindRequest, which reads them all):
//
//   - `query:"name"` — the query parameter name. A slice field takes every
//     value of a repeated key; any other field the first.
//   - `path:"name"` — the wildcard of the route pattern ({name}), read with
//     r.PathValue.
//   - `header:"X-Name"` — the header name, canonicalised (X-Trace-Id and
//     x-trace-id are the same header). A slice field takes every value.
//   - `json:"name"` — the body, read by BindRequest only.
//
// `query:"-"` (and the same for path and header) skips the field. A field
// that carries none of the tags a binder reads is left alone by it.
//
// Field types are the ones BindForm converts — string, bool, signed and
// unsigned integers, floats, time.Time (RFC 3339, 2006-01-02T15:04 or
// 2006-01-02) and pointers to those — plus any type whose pointer
// implements encoding.TextUnmarshaler (a UUID type, a netip.Addr, a custom
// enum), and slices of all of them. Embedded value structs are flattened;
// pointer embeddings are not traversed. A parameter that is absent or
// present-but-empty leaves its field at the value it had.
//
// A value that does not convert is a 400 BAD_REQUEST DomainError whose
// message and details name the parameter as the client spelled it
// ({"page": "must be an integer that fits in 64 bits"}). A struct that does
// not validate is the 422 VALIDATION_FAILED DomainError BindJSON and
// BindForm return, with each failing field named the way the client sent
// it — the query parameter, the path parameter, the header — rather than by
// its Go name.
func BindQuery(r *http.Request, v any) error {
	return bindSources(r, v, srcQuery)
}

// BindPath binds the request's path parameters — the {name} wildcards of
// the route pattern — into v by its `path` tags, then validates v. The
// tags, the types and the errors are BindQuery's.
func BindPath(r *http.Request, v any) error {
	return bindSources(r, v, srcPath)
}

// BindHeaders binds the request's headers into v by its `header` tags,
// then validates v. The tags, the types and the errors are BindQuery's.
func BindHeaders(r *http.Request, v any) error {
	return bindSources(r, v, srcHeader)
}

// BindRequest binds the whole request into one struct: the path
// parameters, the query string and the headers by their tags, then the
// JSON body into the fields that carry a `json` tag, and validates v once,
// at the end.
//
// The body is read only when the request can carry one (not GET, HEAD,
// OPTIONS, TRACE or CONNECT) and its Content-Type is JSON
// (application/json or a +json type); an empty body is no body. It is
// decoded with Bind's discipline — capped at 1 MiB, 413 beyond it, 400 for
// malformed JSON — and it can set only the fields meant for it: a field
// with a `path`, `query` or `header` tag keeps the value its own source
// gave it (or its zero value) whatever the body says, so a body cannot
// overwrite the id the route was called with; and a field with no tag at
// all is left alone.
func BindRequest(r *http.Request, v any) error {
	return bindSources(r, v, srcPath, srcQuery, srcHeader, srcBody)
}

type bindSource int

const (
	srcNone bindSource = iota
	srcPath
	srcQuery
	srcHeader
	srcBody
)

func (s bindSource) label() string {
	switch s {
	case srcPath:
		return "path parameter"
	case srcQuery:
		return "query parameter"
	case srcHeader:
		return "header"
	case srcBody:
		return "body field"
	}
	return "field"
}

// boundField is one field of a bind target and where its value comes from.
type boundField struct {
	index []int
	src   bindSource
	// name is the parameter as the client spells it: the query or path
	// name, the canonical header name.
	name string
	// validName is the name the validator reports a failure under: the
	// json tag's name, else the Go field name.
	validName string
}

var bindPlans sync.Map // reflect.Type -> []boundField

func bindPlan(t reflect.Type) []boundField {
	if p, ok := bindPlans.Load(t); ok {
		return p.([]boundField)
	}
	var fields []boundField
	collectBindFields(t, nil, &fields)
	p, _ := bindPlans.LoadOrStore(t, fields)
	return p.([]boundField)
}

func collectBindFields(t reflect.Type, index []int, out *[]boundField) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		idx := append(append([]int(nil), index...), i)
		// An embedded value struct is flattened even when its type is
		// unexported — its exported fields are settable, as encoding/json
		// treats them.
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			collectBindFields(f.Type, idx, out)
			continue
		}
		if !f.IsExported() {
			continue
		}
		bf := boundField{index: idx, validName: jsonName(f)}
		for _, s := range []struct {
			tag string
			src bindSource
		}{{"path", srcPath}, {"query", srcQuery}, {"header", srcHeader}} {
			tag, ok := f.Tag.Lookup(s.tag)
			if !ok {
				continue
			}
			name, _, _ := strings.Cut(tag, ",")
			name = strings.TrimSpace(name)
			if name == "-" {
				bf.src = srcNone
				break
			}
			if name == "" {
				name = f.Name
			}
			if s.src == srcHeader {
				name = textproto.CanonicalMIMEHeaderKey(name)
			}
			bf.src, bf.name = s.src, name
			break
		}
		if bf.src == srcNone {
			if tag, ok := f.Tag.Lookup("json"); ok {
				if name, _, _ := strings.Cut(tag, ","); name != "-" {
					bf.src, bf.name = srcBody, bf.validName
				}
			}
		}
		*out = append(*out, bf)
	}
}

// jsonName is the name the validator reports a field under (pkg/validate
// registers the json tag's name, falling back to the Go name).
func jsonName(f reflect.StructField) string {
	name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
	if name == "" || name == "-" {
		return f.Name
	}
	return name
}

func bindSources(r *http.Request, v any, sources ...bindSource) error {
	if r == nil {
		return ErrNilContextRequest
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return gferrors.BadRequest("bind target must be a non-nil struct pointer")
	}
	sv := rv.Elem()
	plan := bindPlan(sv.Type())
	want := map[bindSource]bool{}
	for _, s := range sources {
		want[s] = true
	}

	var query map[string][]string
	if want[srcQuery] {
		query = r.URL.Query()
	}
	for _, f := range plan {
		if !want[f.src] || f.src == srcBody {
			continue
		}
		var raws []string
		switch f.src {
		case srcPath:
			if s := strings.TrimSpace(r.PathValue(f.name)); s != "" {
				raws = []string{s}
			}
		case srcQuery:
			raws = query[f.name]
		case srcHeader:
			raws = r.Header.Values(f.name)
		}
		if err := setBound(sv.FieldByIndex(f.index), raws); err != nil {
			msg := fmt.Sprintf("invalid value for %s %q: %s", f.src.label(), f.name, err.Error())
			if len(msg) > 256 {
				msg = msg[:256] + "…"
			}
			return gferrors.BadRequest(msg).WithDetails(map[string]string{f.name: err.Error()})
		}
	}

	if want[srcBody] && requestHasJSONBody(r) {
		if err := decodeBodyInto(r, sv, plan); err != nil {
			return err
		}
	}

	if err := validate.Validate(v); err != nil {
		var domErr *gferrors.DomainError
		if errors.As(err, &domErr) {
			return renameValidationFields(domErr, plan)
		}
		return gferrors.BadRequest(err.Error())
	}
	return nil
}

// setBound converts raws into fv: every non-empty value for a slice, the
// first for anything else. No value leaves fv as it was.
func setBound(fv reflect.Value, raws []string) error {
	var vals []string
	for _, s := range raws {
		if s != "" {
			vals = append(vals, s)
		}
	}
	if len(vals) == 0 {
		return nil
	}
	if fv.Kind() == reflect.Slice && fv.Type().Elem().Kind() != reflect.Uint8 {
		out := reflect.MakeSlice(fv.Type(), len(vals), len(vals))
		for i, s := range vals {
			if err := setBoundValue(out.Index(i), s); err != nil {
				return err
			}
		}
		fv.Set(out)
		return nil
	}
	return setBoundValue(fv, vals[0])
}

// setBoundValue converts one value: through encoding.TextUnmarshaler when
// the type implements it (time.Time excepted — BindForm's layouts are
// wider than its RFC 3339), else through BindForm's own converter.
func setBoundValue(fv reflect.Value, raw string) error {
	target := fv
	if target.Kind() == reflect.Pointer {
		if target.IsNil() {
			target.Set(reflect.New(target.Type().Elem()))
		}
		target = target.Elem()
	}
	if target.Type() != timeType && target.CanAddr() {
		if u, ok := target.Addr().Interface().(encoding.TextUnmarshaler); ok {
			if err := u.UnmarshalText([]byte(raw)); err != nil {
				return errors.New("is not a valid " + target.Type().String())
			}
			return nil
		}
	}
	if target.Kind() == reflect.Slice && target.Type().Elem().Kind() == reflect.Uint8 {
		target.SetBytes([]byte(raw))
		return nil
	}
	if err := setFormField(target, raw); err != nil {
		return errors.New(conversionReason(target, err))
	}
	return nil
}

// conversionReason says what the value had to be, without echoing it.
func conversionReason(fv reflect.Value, err error) string {
	if fv.Type() == timeType {
		return "must be a time (RFC 3339, 2006-01-02T15:04 or 2006-01-02)"
	}
	switch fv.Kind() {
	case reflect.Bool:
		return "must be true or false"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprintf("must be an integer that fits in %d bits", fv.Type().Bits())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fmt.Sprintf("must be a non-negative integer that fits in %d bits", fv.Type().Bits())
	case reflect.Float32, reflect.Float64:
		return "must be a number"
	}
	return err.Error()
}

// requestHasJSONBody reports whether BindRequest reads the body: a method
// that carries one and a JSON Content-Type.
func requestHasJSONBody(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace, http.MethodConnect:
		return false
	}
	if r.Body == nil || r.Body == http.NoBody {
		return false
	}
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return false
	}
	return mt == "application/json" || strings.HasSuffix(mt, "+json")
}

// decodeBodyInto decodes the JSON body into sv and then puts back every
// field the body is not meant to set — the path, query and header fields,
// and the untagged ones — so the body reaches only the json-tagged fields.
func decodeBodyInto(r *http.Request, sv reflect.Value, plan []boundField) error {
	type saved struct {
		index []int
		value reflect.Value
	}
	var keep []saved
	for _, f := range plan {
		if f.src == srcBody {
			continue
		}
		fv := sv.FieldByIndex(f.index)
		cp := reflect.New(fv.Type()).Elem()
		cp.Set(fv)
		keep = append(keep, saved{f.index, cp})
	}
	if err := decodeJSONBody(r, sv.Addr().Interface(), maxJSONBodyBytes); err != nil {
		var maxErr *http.MaxBytesError
		switch {
		case errors.Is(err, io.EOF):
			// An empty body is no body: the required fields fail validation.
		case errors.As(err, &maxErr):
			return payloadTooLarge(maxErr)
		default:
			return gferrors.BadRequest("invalid JSON: " + err.Error())
		}
	}
	for _, k := range keep {
		sv.FieldByIndex(k.index).Set(k.value)
	}
	return nil
}

// decodeJSONBody is BindMax's decoding discipline for BindRequest: the
// body capped at maxBytes before the decoder reads a byte (an unbounded
// json.Decoder would buffer an attacker-sized body), maxBytes <= 0 meaning
// the 1 MiB default — never "unlimited" — and one JSON value decoded into
// v. The decoder's error is returned as is; the caller maps it, a body over
// the cap to payloadTooLarge — the 413 BindMax answers.
func decodeJSONBody(r *http.Request, v interface{}, maxBytes int64) error {
	if maxBytes <= 0 {
		maxBytes = maxJSONBodyBytes
	}
	// nil ResponseWriter: MaxBytesReader only uses it to flag the
	// connection for closure; the error return is what matters here
	// (same pattern as BindForm and BindMax).
	r.Body = http.MaxBytesReader(nil, r.Body, maxBytes)
	return json.NewDecoder(r.Body).Decode(v)
}

// payloadTooLarge is the 413 BindMax answers for a body over its cap.
func payloadTooLarge(maxErr *http.MaxBytesError) *gferrors.DomainError {
	return &gferrors.DomainError{
		Code:       "PAYLOAD_TOO_LARGE",
		Message:    fmt.Sprintf("request body exceeds %d bytes", maxErr.Limit),
		StatusCode: http.StatusRequestEntityTooLarge,
	}
}

// renameValidationFields reports a validation failure under the names the
// client used: the validator names a field by its json tag or its Go name,
// and a query, path or header field is the client's "page", "id" or
// "X-Trace-Id".
func renameValidationFields(de *gferrors.DomainError, plan []boundField) *gferrors.DomainError {
	fields, ok := de.Details.(map[string]string)
	if !ok || len(fields) == 0 {
		return de
	}
	rename := map[string]string{}
	for _, f := range plan {
		if f.src == srcPath || f.src == srcQuery || f.src == srcHeader {
			rename[f.validName] = f.name
		}
	}
	if len(rename) == 0 {
		return de
	}
	out := make(map[string]string, len(fields))
	for k, msg := range fields {
		if n, ok := rename[k]; ok {
			k = n
		}
		out[k] = msg
	}
	return de.WithDetails(out)
}
