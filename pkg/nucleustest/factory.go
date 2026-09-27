// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleustest

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/model"
)

// Make persists one record of a registered model with sensible defaults and
// returns it, primary key filled — the factory every test suite ends up
// writing by hand, done once from the model's own metadata:
//
//	w := nucleustest.Make[Widget](srv)                                  // name "name-1", size 1
//	w := nucleustest.Make[Widget](srv, func(w *Widget) { w.Name = "gear" })
//
// Defaults come from the field's kind and a per-process sequence, so two
// records never collide on a value a test did not choose: strings become
// "<field>-<n>", numbers n, times now; booleans, pointers, slices and
// foreign keys stay zero for the test to set when the model needs them. The
// primary key and the read-only fields are the database's. The record is
// written through model.CRUD against the model's database, with the
// application's dialect, so what the test reads back is what a handler
// would have written.
func Make[T any](srv *Server, overrides ...func(*T)) *T {
	srv.tb.Helper()
	entity := new(T)
	typ := reflect.TypeOf(entity).Elem()
	if typ.Kind() != reflect.Struct {
		srv.tb.Fatalf("nucleustest: Make[%s]: a model is a struct", typ)
	}
	rt := srv.Runtime()
	meta, ok := rt.Models().Get(typ.Name())
	if !ok {
		srv.tb.Fatalf("nucleustest: Make[%s]: no model named %q is registered — mount the module that declares it in Models, or register it on the runtime", typ, typ.Name())
	}
	if meta.Type != nil && meta.Type != typ {
		srv.tb.Fatalf("nucleustest: Make[%s]: the registered model %q is %s, not %s", typ, typ.Name(), meta.Type, typ)
	}
	n := factorySeq.Add(1)
	fillDefaults(reflect.ValueOf(entity).Elem(), meta, n)
	for _, o := range overrides {
		o(entity)
	}
	sqlDB, dialect := factoryDatabase(srv, meta)
	crud := model.NewCRUD(sqlDB, meta, nil)
	crud.SetDialect(dialect)
	if err := crud.Create(context.Background(), entity); err != nil {
		srv.tb.Fatalf("nucleustest: Make[%s]: %v", typ, err)
	}
	return entity
}

// MakeN is Make, n times; the overrides apply to every record.
func MakeN[T any](srv *Server, n int, overrides ...func(*T)) []*T {
	srv.tb.Helper()
	out := make([]*T, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, Make[T](srv, overrides...))
	}
	return out
}

var factorySeq atomic.Int64

// factoryDatabase picks the database the model lives in and its dialect.
func factoryDatabase(srv *Server, meta *model.ModelMeta) (*sql.DB, string) {
	srv.tb.Helper()
	rt := srv.Runtime()
	alias := strings.TrimSpace(meta.DatabaseAlias)
	if alias == "" || alias == "default" {
		handle := rt.DatabaseHandle()
		if handle == nil {
			srv.tb.Fatalf("nucleustest: Make: the application has no default database")
		}
		return rt.DB(), handle.System()
	}
	handles := rt.DatabaseHandles()
	handle, ok := handles[alias]
	if !ok || handle == nil {
		srv.tb.Fatalf("nucleustest: Make: model %s lives in database %q, which the application does not configure", meta.Name, alias)
	}
	sqlDB, err := handle.SqlDB()
	if err != nil {
		srv.tb.Fatalf("nucleustest: Make: database %q: %v", alias, err)
	}
	return sqlDB, handle.System()
}

var timeType = reflect.TypeOf(time.Time{})

// fillDefaults gives every zero, writable, non-key field a value derived
// from its kind and the sequence number.
func fillDefaults(v reflect.Value, meta *model.ModelMeta, n int64) {
	for _, f := range meta.Fields {
		if f.IsPK || f.IsReadOnly || f.IsForeignKey || f.IsExcluded {
			continue
		}
		field := v.FieldByName(f.Name)
		if !field.IsValid() || !field.CanSet() || !field.IsZero() {
			continue
		}
		switch field.Kind() {
		case reflect.String:
			field.SetString(fmt.Sprintf("%s-%d", strings.ToLower(f.Name), n))
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			field.SetInt(n)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			field.SetUint(uint64(n))
		case reflect.Float32, reflect.Float64:
			field.SetFloat(float64(n))
		case reflect.Struct:
			if field.Type() == timeType {
				field.Set(reflect.ValueOf(time.Now().UTC().Truncate(time.Second)))
			}
		}
	}
}
