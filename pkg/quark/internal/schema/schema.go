// Package schema provides struct reflection and model metadata caching for Quark ORM.
// It parses Go struct tags (db, pk, rel, join) and caches the result using sync.Map
// to ensure O(1) lookups after the first access per model type.
package schema

import (
	"reflect"
	"strings"
	"sync"
)

// RelationMeta holds metadata about a model relation.
type RelationMeta struct {
	Type    string       // "has_one", "has_many", "belongs_to"
	Field   string       // struct field name
	JoinCol string       // foreign key column
	RefType reflect.Type // type of the related model (the struct type)
	IsSlice bool         // true for has_many
}

// PKMeta holds primary key metadata.
type PKMeta struct {
	Column string
	Index  int
	Kind   reflect.Kind
}

// FindPK finds the primary key field in a struct value.
// It first looks for a pk:"true" tag, then falls back to db:"id".
func FindPK(v reflect.Value) (PKMeta, bool) {
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.Tag.Get("pk") == "true" {
			dbTag := field.Tag.Get("db")
			if dbTag == "" || dbTag == "-" {
				dbTag = ToSnakeCase(field.Name)
			}
			return PKMeta{Column: dbTag, Index: i, Kind: field.Type.Kind()}, true
		}
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.Tag.Get("db") == "id" {
			return PKMeta{Column: "id", Index: i, Kind: field.Type.Kind()}, true
		}
	}

	return PKMeta{}, false
}

// ModelMeta holds cached metadata about a model struct.
// Computed once per type and stored in a global registry.
type ModelMeta struct {
	Table      string
	PK         PKMeta
	HasPK      bool
	Fields     []FieldMeta
	FieldByCol map[string]*FieldMeta  // lookup by db column name
	Relations  map[string]*RelationMeta // lookup by field name
}

// FieldMeta holds metadata about a single struct field.
type FieldMeta struct {
	Index  int
	Column string // value of the db:"" tag
	Kind   reflect.Kind
	Type   reflect.Type
	IsPK   bool
}

// modelRegistry caches ModelMeta by reflect.Type.
var modelRegistry sync.Map // map[reflect.Type]*ModelMeta

// GetModelMeta returns the cached metadata for model type T.
// If not cached, it computes and stores it.
func GetModelMeta[T any]() *ModelMeta {
	var zero T
	t := reflect.TypeOf(zero)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// Fast path: already cached
	if cached, ok := modelRegistry.Load(t); ok {
		return cached.(*ModelMeta)
	}

	// Slow path: compute metadata
	meta := computeModelMeta(t)
	actual, _ := modelRegistry.LoadOrStore(t, meta)
	return actual.(*ModelMeta)
}

// GetModelMetaByType returns the cached metadata for a reflect.Type.
func GetModelMetaByType(t reflect.Type) *ModelMeta {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if cached, ok := modelRegistry.Load(t); ok {
		return cached.(*ModelMeta)
	}

	meta := computeModelMeta(t)
	actual, _ := modelRegistry.LoadOrStore(t, meta)
	return actual.(*ModelMeta)
}

// computeModelMeta builds ModelMeta from a reflect.Type.
func computeModelMeta(t reflect.Type) *ModelMeta {
	meta := &ModelMeta{
		Table:      ToSnakeCase(Pluralize(t.Name())),
		FieldByCol: make(map[string]*FieldMeta),
		Relations:  make(map[string]*RelationMeta),
	}

	// Find PK: first look for pk:"true", then fall back to db:"id"
	pkIndex := -1
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.Tag.Get("pk") == "true" {
			pkIndex = i
			break
		}
	}
	if pkIndex == -1 {
		for i := 0; i < t.NumField(); i++ {
			if t.Field(i).Tag.Get("db") == "id" {
				pkIndex = i
				break
			}
		}
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Parse relations
		relTag := field.Tag.Get("rel")
		if relTag != "" {
			joinCol := field.Tag.Get("join")
			isSlice := field.Type.Kind() == reflect.Slice

			refType := field.Type
			if isSlice {
				refType = refType.Elem()
			}
			if refType.Kind() == reflect.Ptr {
				refType = refType.Elem()
			}

			meta.Relations[field.Name] = &RelationMeta{
				Type:    relTag,
				Field:   field.Name,
				JoinCol: joinCol,
				RefType: refType,
				IsSlice: isSlice,
			}
			continue
		}

		dbTag := field.Tag.Get("db")
		if dbTag == "" || dbTag == "-" {
			continue
		}

		isPK := i == pkIndex
		fm := FieldMeta{
			Index:  i,
			Column: dbTag,
			Kind:   field.Type.Kind(),
			Type:   field.Type,
			IsPK:   isPK,
		}
		meta.Fields = append(meta.Fields, fm)
		meta.FieldByCol[dbTag] = &meta.Fields[len(meta.Fields)-1]

		if isPK {
			meta.PK = PKMeta{Column: dbTag, Index: i, Kind: field.Type.Kind()}
			meta.HasPK = true
		}
	}

	return meta
}

// Pluralize applies simple English pluralization rules.
func Pluralize(s string) string {
	if strings.HasSuffix(s, "s") || strings.HasSuffix(s, "x") ||
		strings.HasSuffix(s, "ch") || strings.HasSuffix(s, "sh") {
		return s + "es"
	}
	if strings.HasSuffix(s, "y") && len(s) > 1 && !isVowel(s[len(s)-2]) {
		return s[:len(s)-1] + "ies"
	}
	return s + "s"
}

func isVowel(c byte) bool {
	return c == 'a' || c == 'e' || c == 'i' || c == 'o' || c == 'u' ||
		c == 'A' || c == 'E' || c == 'I' || c == 'O' || c == 'U'
}

// ToSnakeCase converts CamelCase to snake_case.
func ToSnakeCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteByte('_')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}
