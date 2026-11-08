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

// Make persists one record of a model with sensible defaults and returns
// it, primary key filled — the factory every test suite ends up writing by
// hand, done once from the model's own metadata:
//
//	w := nucleustest.Make[Widget](srv)                                  // name "name-1", size 1
//	w := nucleustest.Make[Widget](srv, func(w *Widget) { w.Name = "gear" })
//
// Defaults come from the field's kind and a per-process sequence, so two
// records never collide on a value a test did not choose: strings become
// "<field>-<n>", numbers n, times now; booleans, pointers, slices and
// foreign keys stay zero for the test to set when the model needs them. The
// primary key and the read-only fields are the database's. Overrides run
// after the defaults and before the write, so what a test sets wins.
//
// Two kinds of model, one call. A model the application registered — in a
// module's Models, or on the runtime — is read from the registry, as
// pkg/model declares it (model.BaseModel, `db:"column:…"`), and written
// through the framework's own insert in the database its registration
// names. Any other struct is a Quark model, the one the generators write,
// and goes to the Factory the test installed with Server.UseFactory, which
// fills it from Quark's view of its tags and writes it through Quark
// itself. The quarkmake module provides that Factory:
//
//	client, _ := quark.NewWithDB("sqlite", srv.DB())
//	srv.UseFactory(quarkmake.New(client, quarkmake.Model[Gear]()))
//	g := nucleustest.Make[Gear](srv)
//
// Without a Factory, Make on an unregistered struct fails the test and
// says so. Either way the table has to exist (srv.MigrateDir, the
// runtime's AutoMigrate for a registered model, Quark's Migrate for a
// Quark model).
func Make[T any](srv *Server, overrides ...func(*T)) *T {
	srv.tb.Helper()
	entity := new(T)
	typ := reflect.TypeOf(entity).Elem()
	if typ.Kind() != reflect.Struct {
		srv.tb.Fatalf("nucleustest: Make[%s]: a model is a struct", typ)
	}
	n := factorySeq.Add(1)
	if meta, ok := registeredModel(srv, typ); ok {
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
	if srv.factory == nil {
		srv.tb.Fatalf("nucleustest: Make[%s]: no model named %q is registered and no Factory is installed. "+
			"A Quark model is persisted through Quark by the quarkmake module: "+
			"`go get github.com/jcsvwinston/nucleus/pkg/nucleustest/quarkmake` and, after Start, "+
			"`srv.UseFactory(quarkmake.New(client, quarkmake.Model[%s]()))` with a *quark.Client on the test's database "+
			"(quark.NewWithDB(\"sqlite\", srv.DB()) for TempSQLite). "+
			"A pkg/model model is read from the registry instead: mount the module that declares it in Models, or register it on the runtime",
			typ, typ.Name(), typ.Name())
	}
	if err := srv.factory.Fill(entity, n); err != nil {
		srv.tb.Fatalf("nucleustest: Make[%s]: %v", typ, err)
	}
	for _, o := range overrides {
		o(entity)
	}
	if err := srv.factory.Persist(context.Background(), entity); err != nil {
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

// Factory fills and persists the models the application did not register:
// the Quark models of an application built on Quark, whose grammar the kit
// does not read — the framework's own module keeps Quark out of its
// dependency graph. Make calls Fill, applies the test's overrides, then
// Persist, so a Factory fills only what is zero and the test's values win.
//
// The record is a non-nil pointer to a struct, the one Make returns. The
// quarkmake module (github.com/jcsvwinston/nucleus/pkg/nucleustest/quarkmake)
// implements it over a *quark.Client; a test suite with another data layer
// can implement its own.
type Factory interface {
	// Fill gives every zero, writable column of record a default derived from
	// its kind and n, the per-process sequence number Make draws for this
	// record: strings "<field>-<n>", numbers n, times now. The primary key and
	// the columns the data layer stamps are left to it.
	Fill(record any, n int64) error
	// Persist writes record through the data layer and fills its primary key
	// from the database.
	Persist(ctx context.Context, record any) error
}

// UseFactory installs the Factory Make delegates unregistered models to —
// quarkmake.New(client, models…) for an application on Quark. Installing a
// second one replaces the first; nil uninstalls.
func (s *Server) UseFactory(f Factory) {
	s.factory = f
}

var factorySeq atomic.Int64

// registeredModel looks T up in the application's registry. A registered
// name that resolves to another type is a test mistake and fails fast; a
// struct written for pkg/model (BaseModel or its tag grammar) that is not
// registered is refused by name rather than handed to a Factory, because
// the registry is where its tags are parsed and its database alias lives.
func registeredModel(srv *Server, typ reflect.Type) (*model.ModelMeta, bool) {
	srv.tb.Helper()
	if meta, ok := srv.Runtime().Models().Get(typ.Name()); ok {
		if meta.Type != nil && meta.Type != typ {
			srv.tb.Fatalf("nucleustest: Make[%s]: the registered model %q is %s, not %s", typ, typ.Name(), meta.Type, typ)
		}
		return meta, true
	}
	if usesModelGrammar(typ) {
		srv.tb.Fatalf("nucleustest: Make[%s]: no model named %q is registered — mount the module that declares it in Models, or register it on the runtime (a model with pkg/model tags or model.BaseModel is read from the registry; a Quark model goes to the Factory installed with UseFactory)", typ, typ.Name())
	}
	return nil, false
}

var baseModelType = reflect.TypeOf(model.BaseModel{})

// usesModelGrammar reports a struct written for pkg/model: it embeds
// model.BaseModel, or a `db` tag speaks that package's directive grammar
// (`column:<name>;required`, a bare `pk`, `readonly`…) rather than Quark's
// `db:"<column>[,option…]"`.
func usesModelGrammar(typ reflect.Type) bool {
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Anonymous && (f.Type == baseModelType || (f.Type.Kind() == reflect.Struct && usesModelGrammar(f.Type))) {
			return true
		}
		tag, ok := f.Tag.Lookup("db")
		if !ok {
			continue
		}
		if strings.ContainsAny(tag, ":;") {
			return true
		}
		switch strings.TrimSpace(tag) {
		case "pk", "readonly", "required", "exclude", "tenant", "fk":
			return true
		}
	}
	return false
}

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
