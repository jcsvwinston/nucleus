// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package quarkmake persists Quark models for nucleustest.Make, through
// Quark itself.
//
// nucleustest.Make reads a model the application registered from the
// registry; any other struct is a Quark model — the one the generators
// write, which registers nowhere because Quark needs no registry — and goes
// to the Factory the test installs with Server.UseFactory. This package is
// that Factory. It lives in a module of its own so the framework's go.mod
// keeps Quark out of its dependency graph; a test suite adds it when it
// persists Quark models with Make:
//
//	go get github.com/jcsvwinston/nucleus/pkg/nucleustest/quarkmake
//
//	srv := nucleustest.Start(t, app)
//	client, err := quark.NewWithDB("sqlite", srv.DB())   // the test's database, Quark's client
//	…
//	srv.UseFactory(quarkmake.New(client, quarkmake.Model[Gear](), quarkmake.Model[Cog]()))
//	g := nucleustest.Make[Gear](srv)                      // name "name-1", teeth 1, id from the database
//
// Each model Make will persist is named once, with Model[T]: Quark writes a
// record with quark.For[T], which needs the type at compile time, and Make
// reaches this package with a value. Defaults come from Quark's own
// metadata for the struct (quark.GetModelMetaByType: the same reading of
// `db`, `pk` and TableName() the query builder uses), and the record is
// written with quark.For[T](ctx, client).Create, so what the row carries —
// created_at and updated_at stamped, deleted_at NULL, the key read back,
// the dialect's placeholders, the model's hooks and validation — is what
// the application's own code would have written.
package quarkmake

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/jcsvwinston/quark"
)

// Spec names one model type the Factory persists; Model[T] builds it.
type Spec struct {
	typ    reflect.Type
	create func(ctx context.Context, provider quark.ClientProvider, record any) error
}

// Model names T as a model the Factory persists: quarkmake.Model[Gear]().
// T is the struct, not a pointer to it.
func Model[T any]() Spec {
	typ := reflect.TypeOf((*T)(nil)).Elem()
	return Spec{
		typ: typ,
		create: func(ctx context.Context, provider quark.ClientProvider, record any) error {
			entity, ok := record.(*T)
			if !ok {
				return fmt.Errorf("quarkmake: %s was registered as %s, got %T", typ, typ, record)
			}
			return quark.For[T](ctx, provider).Create(entity)
		},
	}
}

// Factory is the nucleustest.Factory for Quark models: it fills a record
// from Quark's metadata and writes it through Quark. Build it with New.
type Factory struct {
	provider quark.ClientProvider
	models   map[reflect.Type]Spec
	// err is what New was given wrong. New returns one value so that
	// installing the Factory stays one line; a nil provider or a zero Spec
	// is reported from Fill and Persist instead, as an error Make fails the
	// test with — never a panic.
	err error
}

// New returns a Factory over provider — a *quark.Client, or a
// quark.TenantRouter — for the models named. Install it with
// srv.UseFactory. The Factory holds the provider and never closes it. A
// nil provider, or a Spec that is not from Model[T], is reported by the
// first Fill or Persist.
func New(provider quark.ClientProvider, models ...Spec) *Factory {
	f := &Factory{provider: provider, models: make(map[reflect.Type]Spec, len(models))}
	if provider == nil {
		f.err = errors.New("quarkmake.New was given a nil ClientProvider — open the client first (quark.NewWithDB on srv.DB())")
		return f
	}
	for i, m := range models {
		if m.typ == nil {
			f.err = fmt.Errorf("quarkmake.New was given a zero Spec at position %d — build each one with quarkmake.Model[T]()", i+1)
			return f
		}
		f.models[m.typ] = m
	}
	return f
}

// Fill gives every zero, writable column of record a default derived from
// its kind and n: strings "<field>-<n>", numbers n, times now. The primary
// key, the version column and the created_at, updated_at and deleted_at
// columns are left to Quark, which stamps the first two on Create and
// keeps the third NULL. A field without a db tag, or tagged db:"-", is not
// a column and is left alone; a relation is not a column either.
func (f *Factory) Fill(record any, n int64) error {
	v, spec, err := f.record(record)
	if err != nil {
		return err
	}
	meta := quark.GetModelMetaByType(spec.typ)
	if meta.TagError != nil {
		return fmt.Errorf("quarkmake: %s: %w", spec.typ, meta.TagError)
	}
	for i := range meta.Fields {
		fm := &meta.Fields[i]
		if fm.IsPK || fm.IsVersion || stampedByQuark(fm.Column) {
			continue
		}
		field := v.Field(fm.Index)
		if !field.CanSet() || !field.IsZero() {
			continue
		}
		name := strings.ToLower(spec.typ.Field(fm.Index).Name)
		switch field.Kind() {
		case reflect.String:
			field.SetString(fmt.Sprintf("%s-%d", name, n))
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
	return nil
}

// Persist writes record with quark.For[T](ctx, provider).Create: Quark's
// validation and hooks run, created_at and updated_at are stamped, and the
// primary key comes back from the database.
func (f *Factory) Persist(ctx context.Context, record any) error {
	_, spec, err := f.record(record)
	if err != nil {
		return err
	}
	if err := spec.create(ctx, f.provider, record); err != nil {
		return fmt.Errorf("quarkmake: create %s: %w", spec.typ, err)
	}
	return nil
}

// record checks that record is a non-nil pointer to a struct the Factory
// was given, and returns the struct value and its Spec.
func (f *Factory) record(record any) (reflect.Value, Spec, error) {
	if f.err != nil {
		return reflect.Value{}, Spec{}, fmt.Errorf("quarkmake: %w", f.err)
	}
	v := reflect.ValueOf(record)
	if !v.IsValid() || v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return reflect.Value{}, Spec{}, fmt.Errorf("quarkmake: a record is a non-nil pointer to a struct, got %T", record)
	}
	typ := v.Elem().Type()
	spec, ok := f.models[typ]
	if !ok {
		return reflect.Value{}, Spec{}, fmt.Errorf("quarkmake: %s is not one of the models this Factory was given — add quarkmake.Model[%s]() to quarkmake.New", typ, typ.Name())
	}
	return v.Elem(), spec, nil
}

var timeType = reflect.TypeOf(time.Time{})

// stampedByQuark reports a column Quark writes itself on Create (the
// created_at/updated_at convention) or keeps NULL (deleted_at, the
// soft-delete marker).
func stampedByQuark(column string) bool {
	switch column {
	case "created_at", "updated_at", "deleted_at":
		return true
	}
	return false
}
