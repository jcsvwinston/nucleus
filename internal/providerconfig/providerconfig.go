// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

// Package providerconfig binds the configuration subtree that belongs to a
// registered provider — a storage backend, a session store, an
// authentication backend — into the provider's own typed struct.
//
// It exists because a provider is NOT a mounted module, and until now only
// modules could carry typed configuration. A third-party storage backend
// could be selected by name and then had nowhere to read its endpoint from:
// `storage.ceph.endpoint` died as an unknown key before the provider ever
// ran. That is the gap this closes.
//
// It lives under internal/ for the same reason internal/configbind does:
// the binding uses a third-party decoder, and exposing that type on a
// public surface would force every plugin author to depend on it
// (ADR-015). Providers reach this through a public method on their
// subsystem's config — storage.Config.BindProvider and friends — so the
// decoder never appears in an exported signature.
package providerconfig

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
)

// Bind decodes raw into dst, fills still-zero fields from their `default:`
// tags, and returns an error naming the provider when either step fails.
//
// The order matters and mirrors module config binding (ADR-010 §2 layer 5):
// the file wins over the tag default, because a default is what you get
// when you said nothing — not something that overrides what you said.
func Bind(provider string, raw map[string]any, dst any) error {
	if dst == nil {
		return fmt.Errorf("providerconfig: %s: destination must not be nil", provider)
	}
	rv := reflect.ValueOf(dst)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf("providerconfig: %s: destination must be a non-nil pointer to a struct", provider)
	}

	if len(raw) > 0 {
		decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
			Result:           dst,
			TagName:          "koanf",
			WeaklyTypedInput: true,
			DecodeHook: mapstructure.ComposeDecodeHookFunc(
				mapstructure.StringToTimeDurationHookFunc(),
				mapstructure.StringToSliceHookFunc(","),
			),
			ErrorUnused: true,
		})
		if err != nil {
			return fmt.Errorf("providerconfig: %s: %w", provider, err)
		}
		if err := decoder.Decode(raw); err != nil {
			// ErrorUnused is on deliberately: a key the provider does not
			// know is a typo, and the whole point of this package is that
			// provider configuration stops being a place where typos go
			// unnoticed.
			return fmt.Errorf("providerconfig: %s: %w", provider, err)
		}
	}

	return applyDefaults(dst)
}

// applyDefaults fills zero-valued settable fields from their `default:`
// tag. Same contract, and same documented limitation, as the module-config
// pass: a field deliberately set to its zero value cannot be told apart
// from one that was never set.
func applyDefaults(ptr any) error {
	v := reflect.ValueOf(ptr)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return nil
	}
	return applyDefaultsValue(v.Elem())
}

func applyDefaultsValue(v reflect.Value) error {
	if v.Kind() != reflect.Struct {
		return nil
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue
		}
		fv := v.Field(i)

		if fv.Kind() == reflect.Struct && fv.Type() != reflect.TypeOf(time.Time{}) {
			if err := applyDefaultsValue(fv); err != nil {
				return err
			}
			continue
		}
		if fv.Kind() == reflect.Ptr && !fv.IsNil() && fv.Elem().Kind() == reflect.Struct {
			if err := applyDefaultsValue(fv.Elem()); err != nil {
				return err
			}
			continue
		}

		tag, ok := field.Tag.Lookup("default")
		if !ok || !fv.IsZero() || !fv.CanSet() {
			continue
		}
		if err := setFromString(fv, tag); err != nil {
			return fmt.Errorf("providerconfig: field %s: default %q: %w", field.Name, tag, err)
		}
	}
	return nil
}

func setFromString(fv reflect.Value, raw string) error {
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(raw)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		fv.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		// time.Duration is an int64 with its own textual form.
		if fv.Type() == reflect.TypeOf(time.Duration(0)) {
			d, err := time.ParseDuration(raw)
			if err != nil {
				return err
			}
			fv.SetInt(int64(d))
			return nil
		}
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return err
		}
		fv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return err
		}
		fv.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return err
		}
		fv.SetFloat(f)
	case reflect.Slice:
		if fv.Type().Elem().Kind() != reflect.String {
			return fmt.Errorf("unsupported slice element %s", fv.Type().Elem().Kind())
		}
		parts := strings.Split(raw, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		fv.Set(reflect.ValueOf(parts))
	default:
		return fmt.Errorf("unsupported kind %s", fv.Kind())
	}
	return nil
}
