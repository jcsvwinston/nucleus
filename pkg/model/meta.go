package model

import (
	"fmt"
	"reflect"
	"strings"
)

// ModelMeta holds all metadata extracted from a registered model struct.
type ModelMeta struct {
	Name        string       // Go struct name (e.g. "User")
	Plural      string       // Plural name (e.g. "Users")
	Table       string       // SQL table name (e.g. "users")
	Fields      []FieldMeta  // Extracted field metadata
	PrimaryKey  string       // Name of the PK field (e.g. "ID")
	ForeignKeys []ForeignKey // Detected foreign key relationships
	Config      ModelConfig  // User-provided configuration
	Type        reflect.Type // The reflect.Type of the struct
}

// ForeignKey describes a detected foreign key relationship.
type ForeignKey struct {
	FieldName    string // The FK field (e.g. "UserID")
	Column       string // The FK column (e.g. "user_id")
	ForeignModel string // The related model name (e.g. "User")
	ForeignTable string // The related table name (e.g. "users")
}

// tabler is the interface GORM uses to determine a custom table name.
type tabler interface {
	TableName() string
}

// ExtractMeta uses reflection to extract metadata from a model struct.
// It reads gorm, json, validate, and admin struct tags to populate FieldMeta.
// Embedded structs (like BaseModel) are flattened into the parent fields list.
func ExtractMeta(model interface{}) (*ModelMeta, error) {
	t := reflect.TypeOf(model)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("model.ExtractMeta: expected struct, got %s", t.Kind())
	}

	meta := &ModelMeta{
		Name:   t.Name(),
		Plural: toPlural(t.Name()),
		Type:   t,
	}

	// Determine table name: check if model implements TableName(), otherwise use convention.
	if tn, ok := model.(tabler); ok {
		meta.Table = tn.TableName()
	} else if pt, ok := reflect.New(t).Interface().(tabler); ok {
		meta.Table = pt.TableName()
	} else {
		meta.Table = toSnakeCase(meta.Plural)
	}

	// Extract fields recursively (handles embedded structs)
	meta.Fields = extractFields(t)

	// Find primary key
	for _, f := range meta.Fields {
		if f.IsPK {
			meta.PrimaryKey = f.Name
			break
		}
	}
	if meta.PrimaryKey == "" {
		// Default: look for field named "ID"
		for i := range meta.Fields {
			if meta.Fields[i].Name == "ID" {
				meta.Fields[i].IsPK = true
				meta.PrimaryKey = "ID"
				break
			}
		}
	}

	// Detect foreign keys: fields ending in "ID" that have a corresponding struct field.
	fieldNames := make(map[string]bool, len(meta.Fields))
	for _, f := range meta.Fields {
		fieldNames[f.Name] = true
	}
	for i, f := range meta.Fields {
		if strings.HasSuffix(f.Name, "ID") && f.Name != "ID" {
			related := strings.TrimSuffix(f.Name, "ID")
			// Check if there's a struct field with the related name (may have been
			// skipped during extraction because it's a pointer to struct).
			if hasStructField(t, related) {
				meta.Fields[i].IsForeignKey = true
				meta.Fields[i].ForeignModel = related
				fk := ForeignKey{
					FieldName:    f.Name,
					Column:       f.Column,
					ForeignModel: related,
					ForeignTable: toSnakeCase(toPlural(related)),
				}
				meta.ForeignKeys = append(meta.ForeignKeys, fk)
			}
		}
	}

	return meta, nil
}

// extractFields recursively extracts FieldMeta from a struct type,
// flattening embedded structs.
func extractFields(t reflect.Type) []FieldMeta {
	var fields []FieldMeta

	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)

		// Skip unexported fields
		if !sf.IsExported() {
			continue
		}

		// Flatten embedded structs (e.g. BaseModel)
		if sf.Anonymous {
			ft := sf.Type
			if ft.Kind() == reflect.Ptr {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct && ft.Name() != "Time" && ft.Name() != "DeletedAt" {
				fields = append(fields, extractFields(ft)...)
				continue
			}
		}

		// Skip struct/pointer-to-struct fields that represent relations (not columns)
		ft := sf.Type
		if ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct && ft.Name() != "Time" && ft.Name() != "DeletedAt" && !sf.Anonymous {
			continue
		}
		if ft.Kind() == reflect.Slice {
			continue
		}

		field := extractFieldMeta(sf)
		fields = append(fields, field)
	}

	return fields
}

// extractFieldMeta builds a FieldMeta from a single struct field.
func extractFieldMeta(sf reflect.StructField) FieldMeta {
	f := FieldMeta{
		Name:   sf.Name,
		Column: toSnakeCase(sf.Name),
		Label:  toTitle(sf.Name),
		GoType: typeName(sf.Type),
	}

	// Parse gorm tag
	gormTag := sf.Tag.Get("gorm")
	if gormTag != "" {
		parseGormTag(gormTag, &f)
	}

	// Parse json tag for column name override in API responses
	jsonTag := sf.Tag.Get("json")
	if jsonTag == "-" {
		f.IsExcluded = true
	}

	// Parse validate tag
	validateTag := sf.Tag.Get("validate")
	if strings.Contains(validateTag, "required") {
		f.IsRequired = true
	}

	// Parse admin tag
	adminTag := sf.Tag.Get("admin")
	if adminTag != "" {
		opts := parseAdminTag(adminTag)
		f.IsList = opts.IsList
		f.IsSearch = opts.IsSearch
		f.IsFilter = opts.IsFilter
		f.IsReadOnly = opts.IsReadOnly
		if opts.IsExcluded {
			f.IsExcluded = true
		}
		if opts.Label != "" {
			f.Label = opts.Label
		}
		if len(opts.Choices) > 0 {
			f.Choices = opts.Choices
		}
	}

	// Infer HTML type
	f.HTMLType = inferHTMLType(f.GoType, sf.Name)
	if len(f.Choices) > 0 {
		f.HTMLType = "select"
	}

	return f
}

// parseGormTag extracts relevant settings from the gorm struct tag.
func parseGormTag(tag string, f *FieldMeta) {
	parts := strings.Split(tag, ";")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		switch {
		case strings.HasPrefix(p, "column:"):
			f.Column = strings.TrimPrefix(p, "column:")
		case p == "primaryKey" || p == "primarykey":
			f.IsPK = true
		case p == "not null" || strings.HasPrefix(p, "not null"):
			f.IsRequired = true
		case p == "autoCreateTime" || p == "autoUpdateTime":
			f.IsReadOnly = true
		}
	}
}

// typeName returns a simplified string representation of a reflect.Type.
func typeName(t reflect.Type) string {
	if t.Kind() == reflect.Ptr {
		return typeName(t.Elem())
	}
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return t.Kind().String()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return t.Kind().String()
	case reflect.Float32, reflect.Float64:
		return t.Kind().String()
	case reflect.Bool:
		return "bool"
	default:
		if t.PkgPath() == "time" && t.Name() == "Time" {
			return "time.Time"
		}
		return t.Name()
	}
}

// hasStructField checks if a type has a field with the given name that is a struct
// or pointer to struct (representing a relation).
func hasStructField(t reflect.Type, name string) bool {
	sf, ok := t.FieldByName(name)
	if !ok {
		return false
	}
	ft := sf.Type
	if ft.Kind() == reflect.Ptr {
		ft = ft.Elem()
	}
	return ft.Kind() == reflect.Struct && ft.Name() != "Time" && ft.Name() != "DeletedAt"
}
