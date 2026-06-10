package router

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/validate"
)

// maxFormBodyBytes caps the request body BindForm is willing to read — form
// pages bind scalar fields, not file uploads, so the limit is deliberately
// tight. It also bounds the in-memory portion of a multipart body.
//
// The cap applies when BindForm performs the first parse of the request. If
// an earlier layer already parsed the form (for example the CSRF middleware
// reading a form-field token), net/http's parse is idempotent and that
// layer's limit governs instead.
const maxFormBodyBytes = 10 << 20 // 10 MiB

// BindForm decodes an application/x-www-form-urlencoded or multipart/form-data
// request into v, then validates it using struct validate tags — the form
// counterpart of Bind.
//
// Field resolution order: a `form:"name"` tag wins, then `json:"name"`, then
// the case-insensitive field name (first match wins if two form keys differ
// only by case); `form:"-"` skips a field. Supported field kinds: string,
// bool, signed and unsigned integers, floats, time.Time (RFC 3339, the
// datetime-local format 2006-01-02T15:04 — parsed as UTC since the wire
// format carries no offset — or 2006-01-02) and pointers to those. Embedded
// value structs are flattened; pointer embeddings are not traversed. Form
// keys without a matching field are ignored; present-but-empty values leave
// the field at its zero value so optional numeric inputs submit cleanly.
// Checkbox values "on" bind as true. Bodies are capped at 10 MiB.
//
// Returns a *DomainError if parsing, conversion, or validation fails.
func BindForm(r *http.Request, v interface{}) error {
	if r == nil {
		return ErrNilContextRequest
	}
	if r.Body != nil {
		r.Body = http.MaxBytesReader(nil, r.Body, maxFormBodyBytes)
	}

	var err error
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		err = r.ParseMultipartForm(maxFormBodyBytes)
	} else {
		err = r.ParseForm()
	}
	if err != nil {
		return gferrors.BadRequest("invalid form body: " + err.Error())
	}

	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return gferrors.BadRequest("bind target must be a non-nil struct pointer")
	}
	if err := bindFormStruct(r.Form, rv.Elem()); err != nil {
		return err
	}

	if err := validate.Validate(v); err != nil {
		var domErr *gferrors.DomainError
		if errors.As(err, &domErr) {
			return domErr
		}
		return gferrors.BadRequest(err.Error())
	}
	return nil
}

func bindFormStruct(form url.Values, sv reflect.Value) error {
	st := sv.Type()
	for i := 0; i < st.NumField(); i++ {
		field := st.Field(i)
		if !field.IsExported() {
			continue
		}
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			if err := bindFormStruct(form, sv.Field(i)); err != nil {
				return err
			}
			continue
		}
		name := formFieldName(field)
		if name == "-" {
			continue
		}
		raw, ok := lookupFormValue(form, name)
		if !ok || raw == "" {
			continue
		}
		if err := setFormField(sv.Field(i), raw); err != nil {
			// Conversion errors echo user input — keep responses bounded.
			msg := fmt.Sprintf("invalid value for field %q: %v", name, err)
			if len(msg) > 256 {
				msg = msg[:256] + "…"
			}
			return gferrors.BadRequest(msg)
		}
	}
	return nil
}

func formFieldName(field reflect.StructField) string {
	for _, tag := range []string{"form", "json"} {
		if v, ok := field.Tag.Lookup(tag); ok {
			if name, _, _ := strings.Cut(v, ","); name != "" {
				return name
			}
		}
	}
	return field.Name
}

func lookupFormValue(form url.Values, name string) (string, bool) {
	if vals, ok := form[name]; ok && len(vals) > 0 {
		return vals[0], true
	}
	for k, vals := range form {
		if strings.EqualFold(k, name) && len(vals) > 0 {
			return vals[0], true
		}
	}
	return "", false
}

var timeType = reflect.TypeOf(time.Time{})

// formTimeLayouts are tried in order: full RFC 3339, the HTML
// <input type="datetime-local"> wire format, then a bare date.
var formTimeLayouts = []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02"}

func setFormField(fv reflect.Value, raw string) error {
	if fv.Kind() == reflect.Pointer {
		if fv.IsNil() {
			fv.Set(reflect.New(fv.Type().Elem()))
		}
		fv = fv.Elem()
	}

	if fv.Type() == timeType {
		for _, layout := range formTimeLayouts {
			if t, err := time.Parse(layout, raw); err == nil {
				fv.Set(reflect.ValueOf(t))
				return nil
			}
		}
		return fmt.Errorf("unsupported time format %q", raw)
	}

	switch fv.Kind() {
	case reflect.String:
		fv.SetString(raw)
	case reflect.Bool:
		if raw == "on" { // unvalued HTML checkbox convention
			fv.SetBool(true)
			return nil
		}
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		fv.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, fv.Type().Bits())
		if err != nil {
			return err
		}
		fv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, fv.Type().Bits())
		if err != nil {
			return err
		}
		fv.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(raw, fv.Type().Bits())
		if err != nil {
			return err
		}
		fv.SetFloat(f)
	default:
		return fmt.Errorf("unsupported field kind %s", fv.Kind())
	}
	return nil
}
