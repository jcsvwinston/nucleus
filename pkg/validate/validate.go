// Package validate provides struct validation powered by go-playground/validator,
// with automatic conversion of validation errors to Nucleus DomainErrors.
package validate

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-playground/validator/v10"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
)

var (
	once     sync.Once
	instance *validator.Validate
)

func getValidator() *validator.Validate {
	once.Do(func() {
		instance = validator.New()
		// Use JSON tag names in error messages instead of Go field names.
		instance.RegisterTagNameFunc(func(fld reflect.StructField) string {
			name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
			if name == "-" || name == "" {
				return fld.Name
			}
			return name
		})
	})
	return instance
}

// Validate validates v using its `validate` tags. Returns a *DomainError of
// type VALIDATION_FAILED with per-field messages if validation fails, or nil.
//
// v is what a binder decoded a request into, so it is not always a struct:
// a JSON body can be an array. The shapes:
//
//   - A struct, or a pointer to one, is validated by its tags; each failure
//     is named by the field (its json name, else its Go name).
//   - A slice, an array or a map — or a pointer to one — is validated
//     element by element: every struct it holds (directly, through
//     pointers, or in nested slices and maps) is validated as above, and
//     each failure is named by the element's position followed by the
//     field, "[1].title" (a map element by its key, "[alice].title"). A nil
//     element has no fields to check and passes, as a nil pointer field
//     with no tag does. Elements that are not structs ([]string,
//     map[string]int) carry no tags and are not checked.
//   - Any other value (a string, a number, a bool) carries no tags and
//     passes.
//
// A nil v, or a nil pointer, is a 400 "invalid input", as it always was.
func Validate(v interface{}) error {
	rv := reflect.ValueOf(v)
	for (rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface) && !rv.IsNil() {
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		fields := map[string]string{}
		if err := collectElements(rv, "", fields); err != nil {
			return err
		}
		if len(fields) == 0 {
			return nil
		}
		return gferrors.ValidationFailed(fields)
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return nil
	}

	fields := map[string]string{}
	if err := collectStruct(v, "", fields); err != nil {
		return err
	}
	if len(fields) == 0 {
		return nil
	}
	return gferrors.ValidationFailed(fields)
}

// collectStruct validates one struct value and records each failure in
// fields under prefix + the field's name. A value the validator cannot take
// (nil, not a struct) is the 400 "invalid input".
func collectStruct(v any, prefix string, fields map[string]string) error {
	err := getValidator().Struct(v)
	if err == nil {
		return nil
	}
	var validationErrors validator.ValidationErrors
	if !errors.As(err, &validationErrors) {
		return gferrors.BadRequest("invalid input")
	}
	for _, fe := range validationErrors {
		name := fe.Field()
		if prefix != "" {
			name = prefix + "." + name
		}
		fields[name] = messageForTag(fe)
	}
	return nil
}

var timeType = reflect.TypeOf(time.Time{})

// collectElements validates every struct rv holds, rv being a slice, an
// array, a map, or one of their elements, and records each failure under
// the element's path from the top: "[1]", "[1][0]", "[alice]".
func collectElements(rv reflect.Value, path string, fields map[string]string) error {
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Struct:
		if rv.Type().ConvertibleTo(timeType) {
			return nil
		}
		return collectStruct(rv.Interface(), path, fields)
	case reflect.Slice, reflect.Array:
		if !mayHoldStruct(rv.Type().Elem(), nil) {
			return nil
		}
		for i := 0; i < rv.Len(); i++ {
			if err := collectElements(rv.Index(i), path+"["+strconv.Itoa(i)+"]", fields); err != nil {
				return err
			}
		}
	case reflect.Map:
		if !mayHoldStruct(rv.Type().Elem(), nil) {
			return nil
		}
		iter := rv.MapRange()
		for iter.Next() {
			if err := collectElements(iter.Value(), fmt.Sprintf("%s[%v]", path, iter.Key().Interface()), fields); err != nil {
				return err
			}
		}
	}
	return nil
}

// mayHoldStruct reports whether a value of type t can hold a struct with
// tags to check, so a []string or a map[string]int is not walked element by
// element. An interface type may hold anything. seen stops a recursive
// container type (type Tree []Tree) from recursing forever.
func mayHoldStruct(t reflect.Type, seen map[reflect.Type]bool) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		return !t.ConvertibleTo(timeType)
	case reflect.Interface:
		return true
	case reflect.Slice, reflect.Array, reflect.Map:
		if seen[t] {
			return false
		}
		if seen == nil {
			seen = map[reflect.Type]bool{}
		}
		seen[t] = true
		return mayHoldStruct(t.Elem(), seen)
	}
	return false
}

// RegisterRule adds a custom validation rule that can be used via struct tags.
func RegisterRule(tag string, fn validator.Func, message string) error {
	v := getValidator()
	if err := v.RegisterValidation(tag, fn); err != nil {
		return err
	}
	customMessagesMu.Lock()
	customMessages[tag] = message
	customMessagesMu.Unlock()
	return nil
}

var (
	customMessages   = map[string]string{}
	customMessagesMu sync.RWMutex // RegisterRule at init vs. Validate on every request (NU-34)
)

func messageForTag(fe validator.FieldError) string {
	customMessagesMu.RLock()
	msg, ok := customMessages[fe.Tag()]
	customMessagesMu.RUnlock()
	if ok {
		return msg
	}

	switch fe.Tag() {
	case "required":
		return "this field is required"
	case "email":
		return "must be a valid email address"
	case "min":
		return "must be at least " + fe.Param() + unitFor(fe)
	case "max":
		return "must be at most " + fe.Param() + unitFor(fe)
	case "len":
		return "must be exactly " + fe.Param() + unitFor(fe)
	case "url":
		return "must be a valid URL"
	case "oneof":
		return "must be one of: " + fe.Param()
	case "gt":
		return "must be greater than " + fe.Param()
	case "gte":
		return "must be greater than or equal to " + fe.Param()
	case "lt":
		return "must be less than " + fe.Param()
	case "lte":
		return "must be less than or equal to " + fe.Param()
	case "unique":
		return "must contain unique values"
	case "numeric":
		return "must be a numeric value"
	case "alpha":
		return "must contain only letters"
	case "alphanum":
		return "must contain only letters and numbers"
	default:
		return "failed validation: " + fe.Tag()
	}
}

// unitFor is what a min, max or len bound counts for the field's kind:
// characters of a string, items of a slice or a map, and nothing for a
// number — "must be at least 1", not "at least 1 characters", for a page
// number bound from the query string.
func unitFor(fe validator.FieldError) string {
	switch fe.Kind() {
	case reflect.String:
		return " characters"
	case reflect.Slice, reflect.Array, reflect.Map:
		return " items"
	}
	return ""
}
