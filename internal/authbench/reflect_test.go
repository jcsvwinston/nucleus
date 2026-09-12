// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package authbench

import (
	"reflect"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// contextHasMethod reports whether the handler-facing Context exposes a
// method by that name. It measures the surface an application author has in
// a handler, which is where an authorization helper would be reached for.
func contextHasMethod(name string) bool {
	typ := reflect.TypeOf(&nucleus.Context{})
	_, ok := typ.MethodByName(name)
	return ok
}

// structFieldNames lists a struct's exported field names, for probes that
// ask what a value carries.
func structFieldNames(v any) string {
	typ := reflect.TypeOf(v)
	if typ == nil || typ.Kind() != reflect.Struct {
		return ""
	}
	var names []string
	for i := 0; i < typ.NumField(); i++ {
		if f := typ.Field(i); f.IsExported() {
			names = append(names, f.Name)
		}
	}
	return strings.Join(names, ", ")
}
