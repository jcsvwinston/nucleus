// Package validate provides struct validation powered by go-playground/validator,
// with automatic conversion of validation errors to GoFrame DomainErrors.
package validate

import (
	"reflect"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"
	gferrors "github.com/jcsvwinston/GoFrame/pkg/errors"
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

// Validate validates a struct using its `validate` tags. Returns a *DomainError
// of type VALIDATION_FAILED with per-field messages if validation fails, or nil.
func Validate(v interface{}) error {
	err := getValidator().Struct(v)
	if err == nil {
		return nil
	}

	validationErrors, ok := err.(validator.ValidationErrors)
	if !ok {
		return gferrors.BadRequest("invalid input")
	}

	fields := make(map[string]string, len(validationErrors))
	for _, fe := range validationErrors {
		fields[fe.Field()] = messageForTag(fe)
	}

	return gferrors.ValidationFailed(fields)
}

// RegisterRule adds a custom validation rule that can be used via struct tags.
func RegisterRule(tag string, fn validator.Func, message string) error {
	v := getValidator()
	if err := v.RegisterValidation(tag, fn); err != nil {
		return err
	}
	customMessages[tag] = message
	return nil
}

var customMessages = map[string]string{}

func messageForTag(fe validator.FieldError) string {
	if msg, ok := customMessages[fe.Tag()]; ok {
		return msg
	}

	switch fe.Tag() {
	case "required":
		return "this field is required"
	case "email":
		return "must be a valid email address"
	case "min":
		return "must be at least " + fe.Param() + " characters"
	case "max":
		return "must be at most " + fe.Param() + " characters"
	case "len":
		return "must be exactly " + fe.Param() + " characters"
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
