package model

import (
	"fmt"
	"reflect"
	"strings"
	"unicode"
)

// ModelMeta holds all metadata extracted from a registered model struct.
type ModelMeta struct {
	Name          string       // Go struct name (e.g. "User")
	Plural        string       // Plural name (e.g. "Users")
	Table         string       // SQL table name (e.g. "users")
	Fields        []FieldMeta  // Extracted field metadata
	PrimaryKey    string       // Name of the PK field (e.g. "ID")
	ForeignKeys   []ForeignKey // Detected foreign key relationships
	Indexes       []IndexMeta  // Declared simple/composite indexes
	Config        ModelConfig  // User-provided configuration
	DatabaseAlias string       // Database alias affinity
	Type          reflect.Type // The reflect.Type of the struct
}

// ForeignKey describes a detected foreign key relationship.
type ForeignKey struct {
	FieldName     string // The FK field (e.g. "UserID")
	Column        string // The FK column (e.g. "user_id")
	ForeignModel  string // The related model name (e.g. "User")
	ForeignTable  string // The related table name (e.g. "users")
	ForeignColumn string // The related column name (e.g. "id")
}

// IndexMeta describes an index extracted from one or more model fields.
type IndexMeta struct {
	Name    string   // SQL index name
	Columns []string // Ordered indexed columns
	Unique  bool     // Unique index/constraint
}

// tabler is the interface models can implement to define a custom table name.
type tabler interface {
	TableName() string
}

// ExtractMeta uses reflection to extract metadata from a model struct.
// It reads storage tags (db), json, validate, and admin tags to populate FieldMeta.
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

	// Extract fields recursively (handles embedded structs).
	fields, err := extractFields(t)
	if err != nil {
		return nil, err
	}
	meta.Fields = fields

	pk, err := resolvePrimaryKey(meta.Fields)
	if err != nil {
		return nil, err
	}
	meta.PrimaryKey = pk

	if err := resolveForeignKeys(meta, t); err != nil {
		return nil, err
	}

	indexes, err := resolveIndexes(meta.Table, meta.Fields)
	if err != nil {
		return nil, err
	}
	meta.Indexes = indexes

	return meta, nil
}

func resolvePrimaryKey(fields []FieldMeta) (string, error) {
	explicit := make([]string, 0, 1)
	for _, f := range fields {
		if f.IsPK {
			explicit = append(explicit, f.Name)
		}
	}

	switch len(explicit) {
	case 0:
		for i := range fields {
			if fields[i].Name == "ID" {
				fields[i].IsPK = true
				return "ID", nil
			}
		}
		return "", nil
	case 1:
		return explicit[0], nil
	default:
		return "", fmt.Errorf("model.ExtractMeta: multiple primary keys declared: %s", strings.Join(explicit, ", "))
	}
}

func resolveForeignKeys(meta *ModelMeta, t reflect.Type) error {
	explicitFields := make(map[string]struct{}, len(meta.Fields))

	for i := range meta.Fields {
		f := &meta.Fields[i]
		if !f.IsForeignKey {
			continue
		}
		if err := normalizeExplicitForeignKey(f); err != nil {
			return fmt.Errorf("model.ExtractMeta: field %s: %w", f.Name, err)
		}
		meta.ForeignKeys = append(meta.ForeignKeys, ForeignKey{
			FieldName:     f.Name,
			Column:        f.Column,
			ForeignModel:  f.ForeignModel,
			ForeignTable:  f.ForeignTable,
			ForeignColumn: f.ForeignColumn,
		})
		explicitFields[f.Name] = struct{}{}
	}

	// Backward-compatible implicit FK detection: fields ending in "ID" with a
	// corresponding relation struct field.
	for i := range meta.Fields {
		f := &meta.Fields[i]
		if _, ok := explicitFields[f.Name]; ok {
			continue
		}
		if strings.HasSuffix(f.Name, "ID") && f.Name != "ID" {
			related := strings.TrimSuffix(f.Name, "ID")
			if hasStructField(t, related) {
				f.IsForeignKey = true
				f.ForeignModel = related
				f.ForeignTable = toSnakeCase(toPlural(related))
				f.ForeignColumn = "id"
				meta.ForeignKeys = append(meta.ForeignKeys, ForeignKey{
					FieldName:     f.Name,
					Column:        f.Column,
					ForeignModel:  related,
					ForeignTable:  f.ForeignTable,
					ForeignColumn: "id",
				})
			}
		}
	}
	return nil
}

func normalizeExplicitForeignKey(f *FieldMeta) error {
	if f.ForeignColumn == "" {
		f.ForeignColumn = "id"
	}
	if f.ForeignModel != "" && f.ForeignTable == "" {
		f.ForeignTable = toSnakeCase(toPlural(f.ForeignModel))
	}
	if f.ForeignModel == "" && f.ForeignTable == "" {
		if strings.HasSuffix(f.Name, "ID") && f.Name != "ID" {
			related := strings.TrimSuffix(f.Name, "ID")
			f.ForeignModel = related
			f.ForeignTable = toSnakeCase(toPlural(related))
		} else {
			return fmt.Errorf("fk requires target model/table when field does not follow <Name>ID convention")
		}
	}
	if !isValidIdentifierLike(f.ForeignTable) {
		return fmt.Errorf("invalid fk table %q", f.ForeignTable)
	}
	if !isValidIdentifierLike(f.ForeignColumn) {
		return fmt.Errorf("invalid fk column %q", f.ForeignColumn)
	}
	return nil
}

func resolveIndexes(table string, fields []FieldMeta) ([]IndexMeta, error) {
	if len(fields) == 0 {
		return nil, nil
	}

	orderedNames := make([]string, 0)
	byName := make(map[string]*IndexMeta)

	for _, f := range fields {
		for _, ref := range f.IndexRefs {
			name := strings.TrimSpace(ref.Name)
			if name == "" {
				name = buildDefaultIndexName(table, f.Column, ref.Unique)
			}
			if !isValidIdentifierLike(name) {
				return nil, fmt.Errorf("model.ExtractMeta: field %s: invalid index name %q", f.Name, name)
			}

			idx, exists := byName[name]
			if !exists {
				idx = &IndexMeta{Name: name, Unique: ref.Unique, Columns: make([]string, 0, 2)}
				byName[name] = idx
				orderedNames = append(orderedNames, name)
			}
			if idx.Unique != ref.Unique {
				return nil, fmt.Errorf("model.ExtractMeta: index %q mixes unique and non-unique declarations", name)
			}
			if containsString(idx.Columns, f.Column) {
				continue
			}
			idx.Columns = append(idx.Columns, f.Column)
		}
	}

	indexes := make([]IndexMeta, 0, len(orderedNames))
	for _, name := range orderedNames {
		idx := byName[name]
		indexes = append(indexes, *idx)
	}
	return indexes, nil
}

func buildDefaultIndexName(table, column string, unique bool) string {
	prefix := "idx"
	if unique {
		prefix = "uq"
	}
	return fmt.Sprintf("%s_%s_%s", prefix, sanitizeIdentifierPart(table), sanitizeIdentifierPart(column))
}

func sanitizeIdentifierPart(in string) string {
	if in == "" {
		return "x"
	}
	var b strings.Builder
	for _, r := range in {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "x"
	}
	return out
}

func containsString(values []string, needle string) bool {
	for _, v := range values {
		if v == needle {
			return true
		}
	}
	return false
}

// isValidIdentifierLike is the allowlist that gates every identifier before it
// is interpolated into scaffold DDL — it is the SQL-injection barrier (quoting
// is not; see ADR-011). It permits letters, digits, `_`, and `.`.
//
// TODO(ADR-011 follow-up): two known gaps live here, both tracked for a later
// iteration and neither introduced by ADR-011:
//   - Oracle reserved words (a column named `comment`, `number`, `date`, …)
//     are accepted but break unquoted Oracle DDL/queries. Selective quoting
//     would land at the `oracleIdentifier` choke point AND the CRUD layer.
//   - `.` is allowed for FK target specs (`orders.id`) but also lets a dotted
//     table name through as schema-qualified DDL. Splitting this into a
//     name-identifier check (no dot) and an FK-reference check (dot allowed)
//     is the clean fix.
func isValidIdentifierLike(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	for _, r := range value {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '_', r == '.':
			continue
		default:
			return false
		}
	}
	return true
}

// extractFields recursively extracts FieldMeta from a struct type,
// flattening embedded structs.
func extractFields(t reflect.Type) ([]FieldMeta, error) {
	var fields []FieldMeta

	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)

		// Skip unexported fields.
		if !sf.IsExported() {
			continue
		}

		// Flatten embedded structs (e.g. BaseModel).
		if sf.Anonymous {
			ft := sf.Type
			if ft.Kind() == reflect.Ptr {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct && ft.Name() != "Time" && ft.Name() != "DeletedAt" {
				embedded, err := extractFields(ft)
				if err != nil {
					return nil, err
				}
				fields = append(fields, embedded...)
				continue
			}
		}

		// Skip struct/pointer-to-struct fields that represent relations (not columns).
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

		// `db:"-"` excludes the field from the persistence layer entirely
		// (no column, no CRUD, no scaffold DDL, no admin) — the standard Go
		// ecosystem convention (encoding/json, sqlx).
		if sf.Tag.Get("db") == "-" {
			continue
		}

		field, err := extractFieldMeta(sf)
		if err != nil {
			return nil, fmt.Errorf("model.ExtractMeta: field %s: %w", sf.Name, err)
		}
		fields = append(fields, field)
	}

	return fields, nil
}

// extractFieldMeta builds a FieldMeta from a single struct field.
func extractFieldMeta(sf reflect.StructField) (FieldMeta, error) {
	f := FieldMeta{
		Name:   sf.Name,
		Column: toSnakeCase(sf.Name),
		Label:  toTitle(sf.Name),
		GoType: typeName(sf.Type),
	}

	// Parse storage tags.
	dbTag := sf.Tag.Get("db")
	if dbTag != "" {
		if err := parseDBTag(dbTag, &f); err != nil {
			return FieldMeta{}, err
		}
	}

	// Parse json tag for column name override in API responses.
	jsonTag := sf.Tag.Get("json")
	if jsonTag == "-" {
		f.IsExcluded = true
	}

	// Parse validate tag.
	validateTag := sf.Tag.Get("validate")
	if strings.Contains(validateTag, "required") {
		f.IsRequired = true
	}

	// Parse admin tag.
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

	// Infer HTML type.
	f.HTMLType = inferHTMLType(f.GoType, sf.Name)
	if len(f.Choices) > 0 {
		f.HTMLType = "select"
	}

	return f, nil
}

// parseDBTag extracts relevant settings from storage struct tags.
// It supports Nucleus's db conventions plus legacy aliases like primaryKey.
func parseDBTag(tag string, f *FieldMeta) error {
	parts := strings.Split(tag, ";")
	for _, part := range parts {
		raw := strings.TrimSpace(part)
		if raw == "" {
			continue
		}
		lower := strings.ToLower(raw)

		switch {
		case strings.HasPrefix(lower, "column:"):
			value := strings.TrimSpace(raw[len("column:"):])
			if value == "" {
				return fmt.Errorf("column tag cannot be empty")
			}
			// The column name is interpolated into DDL and queries, so it
			// must clear the same identifier allow-list that gates FK and
			// index targets — otherwise a hand-written `column:` tag is an
			// unvalidated injection vector into scaffold DDL (audit LOW-A,
			// ADR-011 barrier).
			if !isValidIdentifierLike(value) {
				return fmt.Errorf("invalid column name %q", value)
			}
			f.Column = value

		case lower == "primarykey" || lower == "primary_key" || lower == "pk":
			f.IsPK = true

		// Strict equality on purpose: a prefix match would half-apply a
		// directive like `db:"not null unique"` (missing semicolon) —
		// marking required while silently dropping the unique — which is
		// exactly the false negative the UnknownDBTokens WARN exists to
		// catch (NU6-4). Anything else starting with "not null" falls to
		// the default case and is reported at boot.
		case lower == "not null" || lower == "required":
			f.IsRequired = true

		case lower == "autocreatetime" || lower == "autoupdatetime" || lower == "readonly" || lower == "read_only" || lower == "ro":
			f.IsReadOnly = true

		case lower == "fk":
			f.IsForeignKey = true

		case lower == "tenant":
			f.IsTenantField = true

		case strings.HasPrefix(lower, "fk:"):
			spec := strings.TrimSpace(raw[len("fk:"):])
			if err := parseForeignKeySpec(spec, f); err != nil {
				return err
			}

		case lower == "index":
			f.IndexRefs = append(f.IndexRefs, IndexRef{Unique: false})

		case strings.HasPrefix(lower, "index:"):
			name := strings.TrimSpace(raw[len("index:"):])
			if name == "" {
				return fmt.Errorf("index name cannot be empty")
			}
			f.IndexRefs = append(f.IndexRefs, IndexRef{Name: name, Unique: false})

		case lower == "unique":
			f.IndexRefs = append(f.IndexRefs, IndexRef{Unique: true})

		case strings.HasPrefix(lower, "unique:"):
			name := strings.TrimSpace(raw[len("unique:"):])
			if name == "" {
				return fmt.Errorf("unique index name cannot be empty")
			}
			f.IndexRefs = append(f.IndexRefs, IndexRef{Name: name, Unique: true})

		default:
			// Not an error — a hard failure here would break existing apps
			// whose stray tokens were always ignored — but not silent either:
			// App.Run turns these into a boot-time WARN per field.
			f.UnknownDBTokens = append(f.UnknownDBTokens, raw)
		}
	}
	return nil
}

func parseForeignKeySpec(spec string, f *FieldMeta) error {
	f.IsForeignKey = true
	if spec == "" {
		return nil
	}

	if strings.Contains(spec, "=") {
		pairs := strings.Split(spec, ",")
		for _, pair := range pairs {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			k, v, ok := strings.Cut(pair, "=")
			if !ok {
				return fmt.Errorf("invalid fk spec %q", pair)
			}
			key := strings.ToLower(strings.TrimSpace(k))
			value := strings.TrimSpace(v)
			if value == "" {
				return fmt.Errorf("fk %s value cannot be empty", key)
			}
			switch key {
			case "model":
				f.ForeignModel = value
			case "table":
				f.ForeignTable = value
			case "column":
				f.ForeignColumn = value
			default:
				return fmt.Errorf("unknown fk key %q", key)
			}
		}
		if f.ForeignModel == "" && f.ForeignTable == "" {
			return fmt.Errorf("fk spec requires model or table")
		}
		return nil
	}

	if strings.Contains(spec, ".") {
		table, column, ok := strings.Cut(spec, ".")
		table = strings.TrimSpace(table)
		column = strings.TrimSpace(column)
		if !ok || table == "" || column == "" {
			return fmt.Errorf("invalid fk dotted syntax %q (expected table.column)", spec)
		}
		f.ForeignTable = table
		f.ForeignColumn = column
		return nil
	}

	// Short single-token form: treat CamelCase as model, otherwise as table.
	if startsWithUpper(spec) {
		f.ForeignModel = spec
		return nil
	}
	f.ForeignTable = spec
	return nil
}

func startsWithUpper(s string) bool {
	for _, r := range s {
		return unicode.IsUpper(r)
	}
	return false
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
