package model

import (
	"fmt"
	"strings"
)

// BuildPostgresMigrationScaffold renders deterministic CREATE/DROP
// migration SQL from extracted model metadata, in PostgreSQL dialect.
//
// Differences from the SQLite scaffold:
//
//   - Identifiers stay double-quoted (`"name"`) — same as SQLite, so
//     case-folding is avoided.
//   - Integer auto-increment primary keys use `BIGSERIAL` rather than
//     SQLite's `INTEGER PRIMARY KEY AUTOINCREMENT`.
//   - Type mapping: `BYTEA` (not `BLOB`), `TIMESTAMPTZ` (not
//     `DATETIME`), `BOOLEAN` (native), `DOUBLE PRECISION` for float64.
//   - `CREATE INDEX IF NOT EXISTS` is supported natively (since 9.5).
//   - `DROP TABLE IF EXISTS table CASCADE` to remove dependent
//     foreign keys deterministically.
func BuildPostgresMigrationScaffold(meta *ModelMeta) (string, string, error) {
	if meta == nil {
		return "", "", fmt.Errorf("model.BuildPostgresMigrationScaffold: meta is nil")
	}

	table := strings.TrimSpace(meta.Table)
	if !isValidIdentifierLike(table) {
		return "", "", fmt.Errorf("model.BuildPostgresMigrationScaffold: invalid table name %q", table)
	}
	if len(meta.Fields) == 0 {
		return "", "", fmt.Errorf("model.BuildPostgresMigrationScaffold: model %s has no fields", meta.Name)
	}

	columnDefs := make([]string, 0, len(meta.Fields)+len(meta.ForeignKeys))
	for _, f := range meta.Fields {
		column := strings.TrimSpace(f.Column)
		if column == "" {
			column = toSnakeCase(f.Name)
		}
		if !isValidIdentifierLike(column) {
			return "", "", fmt.Errorf("model.BuildPostgresMigrationScaffold: invalid column %q in field %s", column, f.Name)
		}

		var def string
		if f.IsPK {
			if postgresAutoIncrementPKType(f.GoType) {
				def = quotePostgresIdentifier(column) + " BIGSERIAL PRIMARY KEY"
			} else {
				def = quotePostgresIdentifier(column) + " " + postgresTypeForField(f) + " PRIMARY KEY"
			}
		} else {
			def = quotePostgresIdentifier(column) + " " + postgresTypeForField(f)
			if f.IsRequired {
				def += " NOT NULL"
			}
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
			return "", "", fmt.Errorf("model.BuildPostgresMigrationScaffold: invalid foreign key identifiers for column %q", column)
		}

		constraintName := deterministicForeignKeyName(table, column, foreignTable, foreignColumn)
		if _, exists := seenConstraints[constraintName]; exists {
			continue
		}
		seenConstraints[constraintName] = struct{}{}

		columnDefs = append(columnDefs,
			fmt.Sprintf(
				"CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)",
				quotePostgresIdentifier(constraintName),
				quotePostgresIdentifier(column),
				quotePostgresIdentifier(foreignTable),
				quotePostgresIdentifier(foreignColumn),
			),
		)
	}

	up := &strings.Builder{}
	up.WriteString("CREATE TABLE IF NOT EXISTS ")
	up.WriteString(quotePostgresIdentifier(table))
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
			return "", "", fmt.Errorf("model.BuildPostgresMigrationScaffold: invalid index name %q", name)
		}
		quotedCols := make([]string, 0, len(idx.Columns))
		for _, col := range idx.Columns {
			col = strings.TrimSpace(col)
			if !isValidIdentifierLike(col) {
				return "", "", fmt.Errorf("model.BuildPostgresMigrationScaffold: invalid index column %q for index %q", col, name)
			}
			quotedCols = append(quotedCols, quotePostgresIdentifier(col))
		}
		prefix := "CREATE INDEX"
		if idx.Unique {
			prefix = "CREATE UNIQUE INDEX"
		}
		up.WriteString(fmt.Sprintf(
			"%s IF NOT EXISTS %s ON %s (%s);\n",
			prefix,
			quotePostgresIdentifier(name),
			quotePostgresIdentifier(table),
			strings.Join(quotedCols, ", "),
		))
		indexNames = append(indexNames, name)
	}

	down := &strings.Builder{}
	for i := len(indexNames) - 1; i >= 0; i-- {
		down.WriteString("DROP INDEX IF EXISTS ")
		down.WriteString(quotePostgresIdentifier(indexNames[i]))
		down.WriteString(";\n")
	}
	down.WriteString("DROP TABLE IF EXISTS ")
	down.WriteString(quotePostgresIdentifier(table))
	down.WriteString(" CASCADE;\n")

	return up.String(), down.String(), nil
}

func quotePostgresIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func postgresTypeForField(f FieldMeta) string {
	base := strings.TrimPrefix(strings.TrimSpace(f.GoType), "*")
	switch strings.ToLower(base) {
	case "bool":
		return "BOOLEAN"
	case "int", "int8", "int16", "int32", "uint", "uint8", "uint16", "uint32":
		return "INTEGER"
	case "int64", "uint64":
		return "BIGINT"
	case "float32":
		return "REAL"
	case "float64":
		return "DOUBLE PRECISION"
	case "[]byte":
		return "BYTEA"
	case "time.time":
		return "TIMESTAMPTZ"
	default:
		return "TEXT"
	}
}

func postgresAutoIncrementPKType(goType string) bool {
	base := strings.TrimPrefix(strings.TrimSpace(goType), "*")
	switch strings.ToLower(base) {
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64":
		return true
	default:
		return false
	}
}
