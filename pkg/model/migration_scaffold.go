package model

import (
	"fmt"
	"strings"
)

// BuildSQLiteMigrationScaffold renders deterministic CREATE/DROP migration SQL
// from extracted model metadata.
func BuildSQLiteMigrationScaffold(meta *ModelMeta) (string, string, error) {
	if meta == nil {
		return "", "", fmt.Errorf("model.BuildSQLiteMigrationScaffold: meta is nil")
	}

	table := strings.TrimSpace(meta.Table)
	if !isValidIdentifierLike(table) {
		return "", "", fmt.Errorf("model.BuildSQLiteMigrationScaffold: invalid table name %q", table)
	}
	if len(meta.Fields) == 0 {
		return "", "", fmt.Errorf("model.BuildSQLiteMigrationScaffold: model %s has no fields", meta.Name)
	}

	columnDefs := make([]string, 0, len(meta.Fields)+len(meta.ForeignKeys))
	for _, f := range meta.Fields {
		column := strings.TrimSpace(f.Column)
		if column == "" {
			column = toSnakeCase(f.Name)
		}
		if !isValidIdentifierLike(column) {
			return "", "", fmt.Errorf("model.BuildSQLiteMigrationScaffold: invalid column %q in field %s", column, f.Name)
		}

		def := quoteSQLiteIdentifier(column) + " " + sqliteTypeForField(f)
		if f.IsPK {
			if sqliteAutoIncrementPKType(f.GoType) {
				def = quoteSQLiteIdentifier(column) + " INTEGER PRIMARY KEY AUTOINCREMENT"
			} else {
				def += " PRIMARY KEY"
			}
		} else if f.IsRequired {
			def += " NOT NULL"
		}
		columnDefs = append(columnDefs, def)
	}

	seenConstraints := map[string]struct{}{}
	for _, fk := range meta.ForeignKeys {
		column := strings.TrimSpace(fk.Column)
		foreignTable := strings.TrimSpace(fk.ForeignTable)
		foreignColumn := strings.TrimSpace(fk.ForeignColumn)

		if column == "" || foreignTable == "" {
			continue
		}
		if foreignColumn == "" {
			foreignColumn = "id"
		}

		if !isValidIdentifierLike(column) || !isValidIdentifierLike(foreignTable) || !isValidIdentifierLike(foreignColumn) {
			return "", "", fmt.Errorf("model.BuildSQLiteMigrationScaffold: invalid foreign key identifiers for column %q", column)
		}

		constraintName := deterministicForeignKeyName(table, column, foreignTable, foreignColumn)
		if _, exists := seenConstraints[constraintName]; exists {
			continue
		}
		seenConstraints[constraintName] = struct{}{}

		columnDefs = append(columnDefs,
			fmt.Sprintf(
				"CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)",
				quoteSQLiteIdentifier(constraintName),
				quoteSQLiteIdentifier(column),
				quoteSQLiteIdentifier(foreignTable),
				quoteSQLiteIdentifier(foreignColumn),
			),
		)
	}

	up := &strings.Builder{}
	up.WriteString("CREATE TABLE IF NOT EXISTS ")
	up.WriteString(quoteSQLiteIdentifier(table))
	up.WriteString(" (\n")
	for i, def := range columnDefs {
		sep := ","
		if i == len(columnDefs)-1 {
			sep = ""
		}
		up.WriteString("\t")
		up.WriteString(def)
		up.WriteString(sep)
		up.WriteString("\n")
	}
	up.WriteString(");\n")

	indexNames := make([]string, 0, len(meta.Indexes))
	for _, idx := range meta.Indexes {
		if len(idx.Columns) == 0 {
			continue
		}
		name := strings.TrimSpace(idx.Name)
		if name == "" {
			name = buildDefaultIndexName(table, idx.Columns[0], idx.Unique)
		}
		if !isValidIdentifierLike(name) {
			return "", "", fmt.Errorf("model.BuildSQLiteMigrationScaffold: invalid index name %q", name)
		}

		quotedCols := make([]string, 0, len(idx.Columns))
		for _, col := range idx.Columns {
			col = strings.TrimSpace(col)
			if !isValidIdentifierLike(col) {
				return "", "", fmt.Errorf("model.BuildSQLiteMigrationScaffold: invalid index column %q for index %q", col, name)
			}
			quotedCols = append(quotedCols, quoteSQLiteIdentifier(col))
		}
		prefix := "CREATE INDEX"
		if idx.Unique {
			prefix = "CREATE UNIQUE INDEX"
		}
		up.WriteString(fmt.Sprintf(
			"%s IF NOT EXISTS %s ON %s (%s);\n",
			prefix,
			quoteSQLiteIdentifier(name),
			quoteSQLiteIdentifier(table),
			strings.Join(quotedCols, ", "),
		))
		indexNames = append(indexNames, name)
	}

	down := &strings.Builder{}
	for i := len(indexNames) - 1; i >= 0; i-- {
		down.WriteString("DROP INDEX IF EXISTS ")
		down.WriteString(quoteSQLiteIdentifier(indexNames[i]))
		down.WriteString(";\n")
	}
	down.WriteString("DROP TABLE IF EXISTS ")
	down.WriteString(quoteSQLiteIdentifier(table))
	down.WriteString(";\n")

	return up.String(), down.String(), nil
}

func deterministicForeignKeyName(table, column, foreignTable, foreignColumn string) string {
	return fmt.Sprintf(
		"fk_%s_%s__%s_%s",
		sanitizeIdentifierPart(table),
		sanitizeIdentifierPart(column),
		sanitizeIdentifierPart(foreignTable),
		sanitizeIdentifierPart(foreignColumn),
	)
}

func quoteSQLiteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func sqliteTypeForField(f FieldMeta) string {
	base := strings.TrimPrefix(strings.TrimSpace(f.GoType), "*")
	switch strings.ToLower(base) {
	case "bool":
		return "BOOLEAN"
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64":
		return "INTEGER"
	case "float32", "float64":
		return "REAL"
	case "[]byte":
		return "BLOB"
	case "time.time":
		return "DATETIME"
	default:
		return "TEXT"
	}
}

func sqliteAutoIncrementPKType(goType string) bool {
	base := strings.TrimPrefix(strings.TrimSpace(goType), "*")
	switch strings.ToLower(base) {
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64":
		return true
	default:
		return false
	}
}
