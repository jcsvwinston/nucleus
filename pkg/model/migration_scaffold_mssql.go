package model

import (
	"fmt"
	"strings"
)

// BuildMSSQLMigrationScaffold renders deterministic CREATE/DROP
// migration SQL from extracted model metadata, in SQL Server (T-SQL)
// dialect.
//
// Differences from the Postgres scaffold:
//
//   - Identifier quoting uses square brackets (`[name]`) — the SQL
//     Server convention. The bracketed form is independent of the
//     QUOTED_IDENTIFIER session setting.
//   - Auto-increment PK uses `BIGINT IDENTITY(1,1) PRIMARY KEY`.
//   - Type mapping: `BIT` (booleans), `NVARCHAR(MAX)` (unbounded text),
//     `VARBINARY(MAX)` ([]byte), `DATETIME2` (time.Time, no time zone),
//     `FLOAT(53)` (float64), `REAL` (float32). Integers map directly to
//     `INT` / `BIGINT`.
//   - SQL Server has no `CREATE TABLE IF NOT EXISTS`. The UP wraps the
//     CREATE in `IF OBJECT_ID('table','U') IS NULL CREATE TABLE …`.
//   - SQL Server has no `CREATE INDEX IF NOT EXISTS`. Each index is
//     guarded by `IF NOT EXISTS (SELECT 1 FROM sys.indexes …)`.
//   - DROP TABLE uses the 2016+ `DROP TABLE IF EXISTS [name]` form.
//   - DROP INDEX uses `DROP INDEX [name] ON [table]`.
//
// The output targets SQL Server 2016 or newer. Earlier versions lack
// `DROP TABLE IF EXISTS`; operators on legacy versions need to edit
// the generated DROP section.
func BuildMSSQLMigrationScaffold(meta *ModelMeta) (string, string, error) {
	if meta == nil {
		return "", "", fmt.Errorf("model.BuildMSSQLMigrationScaffold: meta is nil")
	}

	table := strings.TrimSpace(meta.Table)
	if !isValidIdentifierLike(table) {
		return "", "", fmt.Errorf("model.BuildMSSQLMigrationScaffold: invalid table name %q", table)
	}
	if len(meta.Fields) == 0 {
		return "", "", fmt.Errorf("model.BuildMSSQLMigrationScaffold: model %s has no fields", meta.Name)
	}

	// Columns bound to a key need a bounded type on MSSQL: an NVARCHAR(MAX)
	// column is invalid as an index/PK key column. Collect indexed columns so
	// a string column in a key renders as a bounded NVARCHAR.
	indexedCols := make(map[string]bool)
	for _, idx := range meta.Indexes {
		for _, col := range idx.Columns {
			indexedCols[strings.TrimSpace(col)] = true
		}
	}

	columnDefs := make([]string, 0, len(meta.Fields)+len(meta.ForeignKeys))
	for _, f := range meta.Fields {
		column := strings.TrimSpace(f.Column)
		if column == "" {
			column = toSnakeCase(f.Name)
		}
		if !isValidIdentifierLike(column) {
			return "", "", fmt.Errorf("model.BuildMSSQLMigrationScaffold: invalid column %q in field %s", column, f.Name)
		}

		var def string
		if f.IsPK {
			if mssqlAutoIncrementPKType(f.GoType) {
				def = quoteMSSQLIdentifier(column) + " BIGINT IDENTITY(1,1) PRIMARY KEY"
			} else {
				// A PRIMARY KEY is key-bound — a string PK must be a bounded NVARCHAR.
				def = quoteMSSQLIdentifier(column) + " " + mssqlColumnType(f, true) + " PRIMARY KEY"
			}
		} else {
			def = quoteMSSQLIdentifier(column) + " " + mssqlColumnType(f, indexedCols[column])
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
			return "", "", fmt.Errorf("model.BuildMSSQLMigrationScaffold: invalid foreign key identifiers for column %q", column)
		}

		constraintName := deterministicForeignKeyName(table, column, foreignTable, foreignColumn)
		if _, exists := seenConstraints[constraintName]; exists {
			continue
		}
		seenConstraints[constraintName] = struct{}{}

		columnDefs = append(columnDefs,
			fmt.Sprintf(
				"CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)",
				quoteMSSQLIdentifier(constraintName),
				quoteMSSQLIdentifier(column),
				quoteMSSQLIdentifier(foreignTable),
				quoteMSSQLIdentifier(foreignColumn),
			),
		)
	}

	up := &strings.Builder{}
	fmt.Fprintf(up, "IF OBJECT_ID('%s', 'U') IS NULL\nCREATE TABLE %s (\n", table, quoteMSSQLIdentifier(table))
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
			return "", "", fmt.Errorf("model.BuildMSSQLMigrationScaffold: invalid index name %q", name)
		}
		quotedCols := make([]string, 0, len(idx.Columns))
		for _, col := range idx.Columns {
			col = strings.TrimSpace(col)
			if !isValidIdentifierLike(col) {
				return "", "", fmt.Errorf("model.BuildMSSQLMigrationScaffold: invalid index column %q for index %q", col, name)
			}
			quotedCols = append(quotedCols, quoteMSSQLIdentifier(col))
		}
		prefix := "CREATE INDEX"
		if idx.Unique {
			prefix = "CREATE UNIQUE INDEX"
		}
		// SQL Server has no IF NOT EXISTS on CREATE INDEX. Guard with
		// sys.indexes lookup so the migration is idempotent when the
		// migrator re-runs against an existing table.
		fmt.Fprintf(up,
			"IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = '%s' AND object_id = OBJECT_ID('%s'))\n%s %s ON %s (%s);\n",
			name,
			table,
			prefix,
			quoteMSSQLIdentifier(name),
			quoteMSSQLIdentifier(table),
			strings.Join(quotedCols, ", "),
		)
		indexNames = append(indexNames, name)
	}

	down := &strings.Builder{}
	for i := len(indexNames) - 1; i >= 0; i-- {
		fmt.Fprintf(down, "DROP INDEX %s ON %s;\n",
			quoteMSSQLIdentifier(indexNames[i]),
			quoteMSSQLIdentifier(table),
		)
	}
	down.WriteString("DROP TABLE IF EXISTS ")
	down.WriteString(quoteMSSQLIdentifier(table))
	down.WriteString(";\n")

	return up.String(), down.String(), nil
}

func quoteMSSQLIdentifier(value string) string {
	return "[" + strings.ReplaceAll(value, "]", "]]") + "]"
}

// mssqlColumnType picks the column type for a field, preferring a bounded
// NVARCHAR over NVARCHAR(MAX) for key-bound (PRIMARY KEY / indexed / unique)
// string columns. MSSQL rejects an NVARCHAR(MAX) column as a key column in an
// index. NVARCHAR(255) = 510 bytes, within MSSQL's 1700-byte nonclustered
// index-key limit (and the 900-byte clustered limit).
func mssqlColumnType(f FieldMeta, keyBound bool) string {
	if keyBound && strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(f.GoType), "*"), "string") {
		return "NVARCHAR(255)"
	}
	return mssqlTypeForField(f)
}

func mssqlTypeForField(f FieldMeta) string {
	base := strings.TrimPrefix(strings.TrimSpace(f.GoType), "*")
	switch strings.ToLower(base) {
	case "bool":
		return "BIT"
	case "int", "int8", "int16", "int32", "uint", "uint8", "uint16", "uint32":
		return "INT"
	case "int64", "uint64":
		return "BIGINT"
	case "float32":
		return "REAL"
	case "float64":
		return "FLOAT(53)"
	case "[]byte":
		return "VARBINARY(MAX)"
	case "time.time":
		return "DATETIME2"
	default:
		return "NVARCHAR(MAX)"
	}
}

func mssqlAutoIncrementPKType(goType string) bool {
	base := strings.TrimPrefix(strings.TrimSpace(goType), "*")
	switch strings.ToLower(base) {
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64":
		return true
	default:
		return false
	}
}
