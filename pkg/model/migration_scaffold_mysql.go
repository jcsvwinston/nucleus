package model

import (
	"fmt"
	"strings"
)

// BuildMySQLMigrationScaffold renders deterministic CREATE/DROP
// migration SQL from extracted model metadata, in MySQL dialect.
//
// Differences from the SQLite scaffold:
//
//   - Identifier quoting uses backticks (`name`).
//   - Auto-increment PK uses `BIGINT AUTO_INCREMENT PRIMARY KEY`.
//   - Type mapping: `LONGBLOB` (not BLOB), `DATETIME(6)` (not DATETIME),
//     `TINYINT(1)` for booleans, `DOUBLE` for float64.
//   - `CREATE INDEX IF NOT EXISTS` is NOT supported on older MySQL
//     versions; this scaffold emits plain `CREATE INDEX`. Idempotency
//     comes from the surrounding `IF NOT EXISTS` on the table — if
//     the table already exists the migrator won't re-run the script.
//   - `DROP INDEX name ON table` (MySQL syntax) rather than the
//     stand-alone `DROP INDEX name`.
func BuildMySQLMigrationScaffold(meta *ModelMeta) (string, string, error) {
	if meta == nil {
		return "", "", fmt.Errorf("model.BuildMySQLMigrationScaffold: meta is nil")
	}

	table := strings.TrimSpace(meta.Table)
	if !isValidIdentifierLike(table) {
		return "", "", fmt.Errorf("model.BuildMySQLMigrationScaffold: invalid table name %q", table)
	}
	if len(meta.Fields) == 0 {
		return "", "", fmt.Errorf("model.BuildMySQLMigrationScaffold: model %s has no fields", meta.Name)
	}

	// Columns bound to a key need a bounded type on MySQL: a TEXT column
	// cannot be a PRIMARY KEY or indexed without a prefix length (Error 1170).
	// Collect the indexed columns so a string column in a key renders as
	// VARCHAR instead of TEXT.
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
			return "", "", fmt.Errorf("model.BuildMySQLMigrationScaffold: invalid column %q in field %s", column, f.Name)
		}

		var def string
		if f.IsPK {
			if mysqlAutoIncrementPKType(f.GoType) {
				def = quoteMySQLIdentifier(column) + " BIGINT AUTO_INCREMENT PRIMARY KEY"
			} else {
				// A PRIMARY KEY is key-bound — a string PK must be VARCHAR.
				def = quoteMySQLIdentifier(column) + " " + mysqlColumnType(f, true) + " PRIMARY KEY"
			}
		} else {
			def = quoteMySQLIdentifier(column) + " " + mysqlColumnType(f, indexedCols[column])
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
			return "", "", fmt.Errorf("model.BuildMySQLMigrationScaffold: invalid foreign key identifiers for column %q", column)
		}

		constraintName := deterministicForeignKeyName(table, column, foreignTable, foreignColumn)
		if _, exists := seenConstraints[constraintName]; exists {
			continue
		}
		seenConstraints[constraintName] = struct{}{}

		columnDefs = append(columnDefs,
			fmt.Sprintf(
				"CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)",
				quoteMySQLIdentifier(constraintName),
				quoteMySQLIdentifier(column),
				quoteMySQLIdentifier(foreignTable),
				quoteMySQLIdentifier(foreignColumn),
			),
		)
	}

	// Indexes are declared INLINE in the CREATE TABLE rather than as
	// separate CREATE INDEX statements. MySQL has no `CREATE INDEX IF NOT
	// EXISTS`, so a standalone CREATE INDEX fails with "Duplicate key name"
	// (Error 1061) the second time AutoMigrate runs the scaffold. Declaring
	// the index inline lets the surrounding `CREATE TABLE IF NOT EXISTS`
	// cover index idempotency too — a re-run is a complete no-op. (Postgres/
	// SQLite use `CREATE INDEX IF NOT EXISTS`; MSSQL guards with a sys.indexes
	// lookup; the inline form is MySQL's idiomatic equivalent.)
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
			return "", "", fmt.Errorf("model.BuildMySQLMigrationScaffold: invalid index name %q", name)
		}
		quotedCols := make([]string, 0, len(idx.Columns))
		for _, col := range idx.Columns {
			col = strings.TrimSpace(col)
			if !isValidIdentifierLike(col) {
				return "", "", fmt.Errorf("model.BuildMySQLMigrationScaffold: invalid index column %q for index %q", col, name)
			}
			quotedCols = append(quotedCols, quoteMySQLIdentifier(col))
		}
		keyword := "INDEX"
		if idx.Unique {
			keyword = "UNIQUE INDEX"
		}
		columnDefs = append(columnDefs, fmt.Sprintf(
			"%s %s (%s)",
			keyword,
			quoteMySQLIdentifier(name),
			strings.Join(quotedCols, ", "),
		))
		indexNames = append(indexNames, name)
	}

	up := &strings.Builder{}
	up.WriteString("CREATE TABLE IF NOT EXISTS ")
	up.WriteString(quoteMySQLIdentifier(table))
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

	down := &strings.Builder{}
	for i := len(indexNames) - 1; i >= 0; i-- {
		down.WriteString(fmt.Sprintf(
			"DROP INDEX %s ON %s;\n",
			quoteMySQLIdentifier(indexNames[i]),
			quoteMySQLIdentifier(table),
		))
	}
	down.WriteString("DROP TABLE IF EXISTS ")
	down.WriteString(quoteMySQLIdentifier(table))
	down.WriteString(";\n")

	return up.String(), down.String(), nil
}

func quoteMySQLIdentifier(value string) string {
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}

// mysqlColumnType picks the column type for a field, preferring a bounded
// VARCHAR over TEXT for key-bound (PRIMARY KEY / indexed / unique) string
// columns. MySQL rejects keying or indexing a TEXT column without a prefix
// length (Error 1170). VARCHAR(255) stays within InnoDB's index-prefix limit
// for utf8mb4 (255*4 = 1020 < 3072 bytes; innodb_large_prefix is the default
// since MySQL 5.7.7 / 8.0).
func mysqlColumnType(f FieldMeta, keyBound bool) string {
	if keyBound && strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(f.GoType), "*"), "string") {
		return "VARCHAR(255)"
	}
	return mysqlTypeForField(f)
}

func mysqlTypeForField(f FieldMeta) string {
	base := strings.TrimPrefix(strings.TrimSpace(f.GoType), "*")
	switch strings.ToLower(base) {
	case "bool":
		return "TINYINT(1)"
	case "int", "int8", "int16", "int32", "uint", "uint8", "uint16", "uint32":
		return "INT"
	case "int64", "uint64":
		return "BIGINT"
	case "float32":
		return "FLOAT"
	case "float64":
		return "DOUBLE"
	case "[]byte":
		return "LONGBLOB"
	case "time.time":
		return "DATETIME(6)"
	default:
		return "TEXT"
	}
}

func mysqlAutoIncrementPKType(goType string) bool {
	base := strings.TrimPrefix(strings.TrimSpace(goType), "*")
	switch strings.ToLower(base) {
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64":
		return true
	default:
		return false
	}
}
