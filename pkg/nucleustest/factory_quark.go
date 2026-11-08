// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleustest

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/model"
)

// factoryMeta decides how Make reads T (NU-137). A model the application
// registered — `Models` of a mounted module, or Register on the runtime —
// is read from the registry, pkg/model's grammar included. Any other
// struct is read from its own tags with Quark's grammar: the model the
// generators write, which registers nowhere because Quark needs no
// registry. A pkg/model struct that is not registered keeps its error: the
// registry is where its tags are parsed and its database alias lives.
func factoryMeta(srv *Server, typ reflect.Type) *model.ModelMeta {
	srv.tb.Helper()
	if meta, ok := srv.Runtime().Models().Get(typ.Name()); ok {
		if meta.Type != nil && meta.Type != typ {
			srv.tb.Fatalf("nucleustest: Make[%s]: the registered model %q is %s, not %s", typ, typ.Name(), meta.Type, typ)
		}
		return meta
	}
	if usesModelGrammar(typ) {
		srv.tb.Fatalf("nucleustest: Make[%s]: no model named %q is registered — mount the module that declares it in Models, or register it on the runtime (a model with pkg/model tags or model.BaseModel is read from the registry; a Quark model needs no registration)", typ, typ.Name())
	}
	meta, err := quarkModelMeta(typ)
	if err != nil {
		srv.tb.Fatalf("nucleustest: Make[%s]: %v", typ, err)
	}
	return meta
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

// quarkModelMeta builds the metadata Make persists through from a struct's
// Quark tags, the way Quark itself reads them (its internal/schema
// package): a field is a column when its `db` tag names one (the text
// before the first comma, trimmed; "" and "-" are not columns; a `rel`
// tag is a relation, not a column; an embedded struct is not flattened);
// the key is the field tagged pk:"true", else the one whose column is
// "id"; the table is TableName() when the type has it, else the type name
// pluralised and in snake case. created_at and updated_at are the
// conventional columns CRUD stamps; deleted_at stays nil.
func quarkModelMeta(typ reflect.Type) (*model.ModelMeta, error) {
	meta := &model.ModelMeta{
		Name:          typ.Name(),
		Table:         quarkTableName(typ),
		DatabaseAlias: "default",
		Type:          typ,
	}
	var pks []string
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.PkgPath != "" || f.Tag.Get("rel") != "" {
			continue
		}
		column := quarkColumn(f.Tag.Get("db"))
		if column == "" || column == "-" {
			continue
		}
		fm := model.FieldMeta{Name: f.Name, Column: column, GoType: quarkGoType(f.Type)}
		if v, ok := f.Tag.Lookup("pk"); ok && strings.EqualFold(v, "true") {
			fm.IsPK = true
			pks = append(pks, f.Name)
		}
		meta.Fields = append(meta.Fields, fm)
	}
	if len(meta.Fields) == 0 {
		return nil, fmt.Errorf("%s has no db-tagged field: Make reads a Quark model from its tags (db:\"<column>\", pk:\"true\"), or a registered pkg/model model from the registry", typ)
	}
	if len(pks) > 1 {
		return nil, fmt.Errorf("%s declares a composite primary key (%s); Make needs a single key the database generates", typ, strings.Join(pks, ", "))
	}
	if len(pks) == 0 {
		for i := range meta.Fields {
			if meta.Fields[i].Column == "id" {
				meta.Fields[i].IsPK = true
				pks = append(pks, meta.Fields[i].Name)
				break
			}
		}
	}
	if len(pks) == 1 {
		meta.PrimaryKey = pks[0]
	}
	return meta, nil
}

// quarkTableNamer is Quark's TableNamer: a model names its own table.
type quarkTableNamer interface {
	TableName() string
}

// quarkTableName mirrors Quark's rule: TableName() on a zero *T (a value or
// a pointer receiver both count), else the type name pluralised, then in
// snake case (UserProfile → user_profiles).
func quarkTableName(typ reflect.Type) string {
	if tn, ok := reflect.New(typ).Interface().(quarkTableNamer); ok {
		return tn.TableName()
	}
	return quarkSnakeCase(quarkPluralize(typ.Name()))
}

// quarkColumn is the column-name portion of a Quark `db` tag: the text
// before the first comma (db:"name,size=512" → name), trimmed.
func quarkColumn(tag string) string {
	if i := strings.IndexByte(tag, ','); i >= 0 {
		tag = tag[:i]
	}
	return strings.TrimSpace(tag)
}

// quarkGoType names the field's type the way CRUD's key read-back wants it:
// an integer kind by its kind (a named integer type is still an integer
// key), anything else by its Go type.
func quarkGoType(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return t.Kind().String()
	}
	return t.String()
}

// quarkPluralize is Quark's pluralisation (internal/schema.Pluralize).
func quarkPluralize(s string) string {
	if strings.HasSuffix(s, "s") || strings.HasSuffix(s, "x") ||
		strings.HasSuffix(s, "ch") || strings.HasSuffix(s, "sh") {
		return s + "es"
	}
	if strings.HasSuffix(s, "y") && len(s) > 1 && !strings.ContainsRune("aeiouAEIOU", rune(s[len(s)-2])) {
		return s[:len(s)-1] + "ies"
	}
	return s + "s"
}

// quarkSnakeCase is Quark's CamelCase → snake_case (internal/schema
// .ToSnakeCase), acronyms kept together (HTTPServer → http_server).
func quarkSnakeCase(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			prev := s[i-1]
			if (prev >= 'a' && prev <= 'z') || (i+1 < len(s) && s[i+1] >= 'a' && s[i+1] <= 'z') {
				b.WriteByte('_')
			}
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}
