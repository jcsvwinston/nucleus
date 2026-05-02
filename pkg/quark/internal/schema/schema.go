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
	Type           string       // "has_one", "has_many", "belongs_to", "m2m", "polymorphic"
	Field          string       // struct field name
	JoinCol        string       // foreign key column (for belongs_to, has_one, has_many)
	JoinTable      string       // join table name (for m2m)
	JoinFK         string       // foreign key in join table pointing to this model (for m2m)
	JoinRefFK      string       // foreign key in join table pointing to related model (for m2m)
	PolyType       string       // polymorphic type identifier (for polymorphic)
	PolyTypeColumn string       // column storing the polymorphic type (for polymorphic)
	PolyIDColumn   string       // column storing the polymorphic foreign key (for polymorphic)
	RefType        reflect.Type // type of the related model (the struct type)
	IsSlice        bool         // true for has_many, m2m
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
	FieldByCol map[string]*FieldMeta    // lookup by db column name
	Relations  map[string]*RelationMeta // lookup by field name
}

// FieldMeta holds metadata about a single struct field.
type FieldMeta struct {
	Index  int
	Column string // value of the db:"" tag
	Kind   reflect.Kind
	Type   reflect.Type
	IsPK      bool
	OldColumn string // for renames
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

// TableNamer interface for custom table names.
type TableNamer interface {
	TableName() string
}

// computeModelMeta builds ModelMeta from a reflect.Type.
func computeModelMeta(t reflect.Type) *ModelMeta {
	tableName := ToSnakeCase(Pluralize(t.Name()))
	
	// Check if type implements TableName() string
	// We create a zero value of the type to check for methods
	zero := reflect.New(t).Interface()
	if tn, ok := zero.(TableNamer); ok {
		tableName = tn.TableName()
	}

	meta := &ModelMeta{
		Table:      tableName,
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

			relMeta := &RelationMeta{
				Type:    relTag,
				Field:   field.Name,
				JoinCol: joinCol,
				RefType: refType,
				IsSlice: isSlice,
			}

			// Infer JoinCol if missing for standard relations
			if relMeta.JoinCol == "" {
				if relMeta.Type == "belongs_to" {
					// For belongs_to, the FK is in THIS model (pointing to related model)
					relMeta.JoinCol = ToSnakeCase(refType.Name()) + "_id"
				} else if relMeta.Type == "has_one" || relMeta.Type == "has_many" {
					// For has_one/has_many, the FK is in the RELATED model (pointing to this model)
					relMeta.JoinCol = ToSnakeCase(t.Name()) + "_id"
				}
			}

			// Parse m2m (many-to-many) tag: m2m:"join_table" or m2m:"join_table:this_fk:ref_fk"
			if m2mTag := field.Tag.Get("m2m"); m2mTag != "" {
				parts := strings.Split(m2mTag, ":")
				relMeta.JoinTable = parts[0]
				if len(parts) >= 3 {
					relMeta.JoinFK = parts[1]
					relMeta.JoinRefFK = parts[2]
				}
				// Auto-generate fk names if not specified
				if relMeta.JoinFK == "" {
					relMeta.JoinFK = ToSnakeCase(t.Name()) + "_id"
				}
				if relMeta.JoinRefFK == "" {
					relMeta.JoinRefFK = ToSnakeCase(refType.Name()) + "_id"
				}
			}

			// Parse polymorphic tag: polymorphic:"type_col:poly_type" or polymorphic:"poly_type"
			if polyTag := field.Tag.Get("polymorphic"); polyTag != "" {
				parts := strings.Split(polyTag, ":")
				if len(parts) == 2 {
					relMeta.PolyTypeColumn = parts[0]
					relMeta.PolyType = parts[1]
				} else {
					relMeta.PolyType = parts[0]
					relMeta.PolyTypeColumn = "poly_type"
				}
				relMeta.PolyIDColumn = ToSnakeCase(field.Name) + "_id"
			}

			meta.Relations[field.Name] = relMeta
			continue
		}

		dbTag := field.Tag.Get("db")
		if dbTag == "" || dbTag == "-" {
			continue
		}

		isPK := i == pkIndex
		oldCol := ""
		if quarkTag := field.Tag.Get("quark"); strings.HasPrefix(quarkTag, "rename:") {
			oldCol = strings.TrimPrefix(quarkTag, "rename:")
		}

		fm := FieldMeta{
			Index:     i,
			Column:    dbTag,
			Kind:      field.Type.Kind(),
			Type:      field.Type,
			IsPK:      isPK,
			OldColumn: oldCol,
		}
		meta.Fields = append(meta.Fields, fm)
		meta.FieldByCol[strings.ToLower(dbTag)] = &meta.Fields[len(meta.Fields)-1]

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

// ToSnakeCase converts CamelCase to snake_case, intelligently handling acronyms.
func ToSnakeCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			prev := s[i-1]
			// Add underscore if transitioning from lower to upper,
			// or if transitioning from upper to lower (end of acronym).
			if (prev >= 'a' && prev <= 'z') || (i+1 < len(s) && s[i+1] >= 'a' && s[i+1] <= 'z') {
				result.WriteByte('_')
			}
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}
