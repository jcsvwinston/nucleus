package cli

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

type inspectModel struct {
	TableName  string
	StructName string
	Columns    []inspectColumn
}

type inspectColumn struct {
	ColumnName string
	FieldName  string
	GoType     string
	Tag        string
}

func runInspectDB(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("inspectdb", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to goframe config file")
	tablesRaw := fs.String("tables", "", "Comma-separated table list to inspect (default: all user tables)")
	excludeRaw := fs.String("exclude", "", "Comma-separated table list to exclude")
	packageName := fs.String("package", "models", "Go package name for generated structs")
	outputPath := fs.String("output", "-", "Output Go file path ('-' for stdout)")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	positionalTables := normalizeTableList(fs.Args())
	if *tablesRaw != "" && len(positionalTables) > 0 {
		return fmt.Errorf("inspectdb accepts either positional table names or --tables, not both")
	}

	pkg := strings.TrimSpace(*packageName)
	if !isValidGoPackageName(pkg) {
		return fmt.Errorf("invalid package name %q", pkg)
	}

	cfg, database, cleanup, err := newDatabase(*configPath)
	if err != nil {
		return err
	}
	defer cleanup()

	sqlDB, err := database.SqlDB()
	if err != nil {
		return fmt.Errorf("open sql handle: %w", err)
	}
	flavor := detectDBFlavor(cfg.DatabaseURL)

	allTables, err := listUserTables(sqlDB, flavor)
	if err != nil {
		return err
	}
	includeTables := parseTableCSV(*tablesRaw)
	if len(positionalTables) > 0 {
		includeTables = positionalTables
	}
	selectedTables, err := selectTablesForDump(allTables, includeTables, parseTableCSV(*excludeRaw))
	if err != nil {
		return err
	}
	if len(selectedTables) == 0 {
		return fmt.Errorf("no tables selected for inspectdb")
	}

	models, err := buildInspectModels(sqlDB, flavor, selectedTables)
	if err != nil {
		return err
	}

	source, err := renderInspectModels(pkg, models)
	if err != nil {
		return err
	}

	target := strings.TrimSpace(*outputPath)
	switch target {
	case "", "-":
		_, _ = stdout.Write(source)
	default:
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
		if err := os.WriteFile(target, source, 0644); err != nil {
			return fmt.Errorf("write inspectdb output: %w", err)
		}
		fmt.Fprintf(stdout, "inspectdb output written: %s (%d table(s))\n", target, len(models))
	}
	return nil
}

func buildInspectModels(sqlDB *sql.DB, flavor dbFlavor, tables []string) ([]inspectModel, error) {
	structNameUsage := map[string]int{}
	models := make([]inspectModel, 0, len(tables))

	for _, table := range tables {
		columns, err := inspectTableColumns(sqlDB, flavor, table)
		if err != nil {
			return nil, err
		}
		if len(columns) == 0 {
			continue
		}

		structName := uniqueStructName(tableToStructName(table), structNameUsage)

		fieldNameUsage := map[string]int{}
		outColumns := make([]inspectColumn, 0, len(columns))
		for _, col := range columns {
			fieldName := uniqueFieldName(columnToFieldName(col.Name), fieldNameUsage)
			goType := mapInspectType(flavor, col.DBType, col.Nullable, col.PrimaryKey)
			tag := buildInspectTag(col.Name, col.Nullable, col.PrimaryKey)
			outColumns = append(outColumns, inspectColumn{
				ColumnName: col.Name,
				FieldName:  fieldName,
				GoType:     goType,
				Tag:        tag,
			})
		}

		models = append(models, inspectModel{
			TableName:  table,
			StructName: structName,
			Columns:    outColumns,
		})
	}
	return models, nil
}

type introspectedColumn struct {
	Name       string
	DBType     string
	Nullable   bool
	PrimaryKey bool
}

func inspectTableColumns(sqlDB *sql.DB, flavor dbFlavor, table string) ([]introspectedColumn, error) {
	if err := validateSQLIdentifier(table); err != nil {
		return nil, err
	}

	switch flavor {
	case dbFlavorSQLite:
		return inspectSQLiteColumns(sqlDB, table)
	case dbFlavorPostgres:
		return inspectPostgresColumns(sqlDB, table)
	case dbFlavorMySQL:
		return inspectMySQLColumns(sqlDB, table)
	case dbFlavorMSSQL:
		return inspectMSSQLColumns(sqlDB, table)
	case dbFlavorOracle:
		return inspectOracleColumns(sqlDB, table)
	default:
		return nil, fmt.Errorf("inspectdb: unsupported database engine")
	}
}

func inspectSQLiteColumns(sqlDB *sql.DB, table string) ([]introspectedColumn, error) {
	query := fmt.Sprintf("PRAGMA table_info(%s)", quoteIdentifier(dbFlavorSQLite, table))
	rows, err := sqlDB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("inspectdb sqlite table_info %s: %w", table, err)
	}
	defer rows.Close()

	columns := make([]introspectedColumn, 0, 16)
	for rows.Next() {
		var (
			cid       int
			name      string
			dbType    string
			notNull   int
			defaultV  sql.NullString
			primaryID int
		)
		if err := rows.Scan(&cid, &name, &dbType, &notNull, &defaultV, &primaryID); err != nil {
			return nil, fmt.Errorf("inspectdb sqlite scan %s: %w", table, err)
		}
		columns = append(columns, introspectedColumn{
			Name:       name,
			DBType:     dbType,
			Nullable:   notNull == 0,
			PrimaryKey: primaryID > 0,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspectdb sqlite rows %s: %w", table, err)
	}
	return columns, nil
}

func inspectPostgresColumns(sqlDB *sql.DB, table string) ([]introspectedColumn, error) {
	pkColumns := map[string]struct{}{}
	pkRows, err := sqlDB.Query(
		`SELECT kcu.column_name
		   FROM information_schema.table_constraints tc
		   JOIN information_schema.key_column_usage kcu
		     ON tc.constraint_name = kcu.constraint_name
		    AND tc.table_schema = kcu.table_schema
		  WHERE tc.table_schema='public'
		    AND tc.table_name = $1
		    AND tc.constraint_type='PRIMARY KEY'`,
		table,
	)
	if err != nil {
		return nil, fmt.Errorf("inspectdb postgres primary keys %s: %w", table, err)
	}
	for pkRows.Next() {
		var name string
		if err := pkRows.Scan(&name); err != nil {
			pkRows.Close()
			return nil, fmt.Errorf("inspectdb postgres pk scan %s: %w", table, err)
		}
		pkColumns[name] = struct{}{}
	}
	if err := pkRows.Err(); err != nil {
		pkRows.Close()
		return nil, fmt.Errorf("inspectdb postgres pk rows %s: %w", table, err)
	}
	pkRows.Close()

	rows, err := sqlDB.Query(
		`SELECT column_name, data_type, udt_name, is_nullable
		   FROM information_schema.columns
		  WHERE table_schema='public' AND table_name = $1
		  ORDER BY ordinal_position`,
		table,
	)
	if err != nil {
		return nil, fmt.Errorf("inspectdb postgres columns %s: %w", table, err)
	}
	defer rows.Close()

	out := make([]introspectedColumn, 0, 16)
	for rows.Next() {
		var (
			name       string
			dataType   string
			udtName    string
			isNullable string
		)
		if err := rows.Scan(&name, &dataType, &udtName, &isNullable); err != nil {
			return nil, fmt.Errorf("inspectdb postgres scan %s: %w", table, err)
		}
		dbType := dataType
		if strings.TrimSpace(udtName) != "" {
			dbType = udtName
		}
		_, isPK := pkColumns[name]
		out = append(out, introspectedColumn{
			Name:       name,
			DBType:     dbType,
			Nullable:   strings.EqualFold(isNullable, "YES"),
			PrimaryKey: isPK,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspectdb postgres rows %s: %w", table, err)
	}
	return out, nil
}

func inspectMySQLColumns(sqlDB *sql.DB, table string) ([]introspectedColumn, error) {
	rows, err := sqlDB.Query(
		`SELECT column_name, data_type, column_type, is_nullable, column_key
		   FROM information_schema.columns
		  WHERE table_schema = DATABASE() AND table_name = ?
		  ORDER BY ordinal_position`,
		table,
	)
	if err != nil {
		return nil, fmt.Errorf("inspectdb mysql columns %s: %w", table, err)
	}
	defer rows.Close()

	out := make([]introspectedColumn, 0, 16)
	for rows.Next() {
		var (
			name       string
			dataType   string
			columnType string
			isNullable string
			columnKey  string
		)
		if err := rows.Scan(&name, &dataType, &columnType, &isNullable, &columnKey); err != nil {
			return nil, fmt.Errorf("inspectdb mysql scan %s: %w", table, err)
		}
		dbType := strings.TrimSpace(dataType)
		if strings.TrimSpace(columnType) != "" {
			dbType = columnType
		}
		out = append(out, introspectedColumn{
			Name:       name,
			DBType:     dbType,
			Nullable:   strings.EqualFold(isNullable, "YES"),
			PrimaryKey: strings.EqualFold(columnKey, "PRI"),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspectdb mysql rows %s: %w", table, err)
	}
	return out, nil
}

func inspectMSSQLColumns(sqlDB *sql.DB, table string) ([]introspectedColumn, error) {
	pkColumns := map[string]struct{}{}
	pkRows, err := sqlDB.Query(
		`SELECT ku.COLUMN_NAME
		   FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc
		   JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE ku
		     ON tc.CONSTRAINT_NAME = ku.CONSTRAINT_NAME
		    AND tc.TABLE_SCHEMA = ku.TABLE_SCHEMA
		  WHERE tc.TABLE_SCHEMA = 'dbo'
		    AND tc.TABLE_NAME = @p1
		    AND tc.CONSTRAINT_TYPE = 'PRIMARY KEY'`,
		table,
	)
	if err != nil {
		return nil, fmt.Errorf("inspectdb mssql primary keys %s: %w", table, err)
	}
	for pkRows.Next() {
		var name string
		if err := pkRows.Scan(&name); err != nil {
			pkRows.Close()
			return nil, fmt.Errorf("inspectdb mssql pk scan %s: %w", table, err)
		}
		pkColumns[name] = struct{}{}
	}
	if err := pkRows.Err(); err != nil {
		pkRows.Close()
		return nil, fmt.Errorf("inspectdb mssql pk rows %s: %w", table, err)
	}
	pkRows.Close()

	rows, err := sqlDB.Query(
		`SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE
		   FROM INFORMATION_SCHEMA.COLUMNS
		  WHERE TABLE_SCHEMA = 'dbo' AND TABLE_NAME = @p1
		  ORDER BY ORDINAL_POSITION`,
		table,
	)
	if err != nil {
		return nil, fmt.Errorf("inspectdb mssql columns %s: %w", table, err)
	}
	defer rows.Close()

	out := make([]introspectedColumn, 0, 16)
	for rows.Next() {
		var (
			name       string
			dataType   string
			isNullable string
		)
		if err := rows.Scan(&name, &dataType, &isNullable); err != nil {
			return nil, fmt.Errorf("inspectdb mssql scan %s: %w", table, err)
		}
		_, isPK := pkColumns[name]
		out = append(out, introspectedColumn{
			Name:       name,
			DBType:     dataType,
			Nullable:   strings.EqualFold(isNullable, "YES"),
			PrimaryKey: isPK,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspectdb mssql rows %s: %w", table, err)
	}
	return out, nil
}

func inspectOracleColumns(sqlDB *sql.DB, table string) ([]introspectedColumn, error) {
	pkColumns := map[string]struct{}{}
	pkRows, err := sqlDB.Query(
		`SELECT cols.column_name
		   FROM all_constraints cons
		   JOIN all_cons_columns cols
		     ON cons.constraint_name = cols.constraint_name
		    AND cons.owner = cols.owner
		  WHERE cons.owner = USER
		    AND cons.table_name = UPPER(:1)
		    AND cons.constraint_type = 'P'`,
		table,
	)
	if err != nil {
		return nil, fmt.Errorf("inspectdb oracle primary keys %s: %w", table, err)
	}
	for pkRows.Next() {
		var name string
		if err := pkRows.Scan(&name); err != nil {
			pkRows.Close()
			return nil, fmt.Errorf("inspectdb oracle pk scan %s: %w", table, err)
		}
		pkColumns[name] = struct{}{}
	}
	if err := pkRows.Err(); err != nil {
		pkRows.Close()
		return nil, fmt.Errorf("inspectdb oracle pk rows %s: %w", table, err)
	}
	pkRows.Close()

	rows, err := sqlDB.Query(
		`SELECT column_name, data_type, nullable
		   FROM user_tab_columns
		  WHERE table_name = UPPER(:1)
		  ORDER BY column_id`,
		table,
	)
	if err != nil {
		return nil, fmt.Errorf("inspectdb oracle columns %s: %w", table, err)
	}
	defer rows.Close()

	out := make([]introspectedColumn, 0, 16)
	for rows.Next() {
		var (
			name       string
			dataType   string
			isNullable string
		)
		if err := rows.Scan(&name, &dataType, &isNullable); err != nil {
			return nil, fmt.Errorf("inspectdb oracle scan %s: %w", table, err)
		}
		_, isPK := pkColumns[name]
		out = append(out, introspectedColumn{
			Name:       strings.ToLower(name),
			DBType:     dataType,
			Nullable:   strings.EqualFold(isNullable, "Y"),
			PrimaryKey: isPK,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspectdb oracle rows %s: %w", table, err)
	}
	return out, nil
}

func renderInspectModels(packageName string, models []inspectModel) ([]byte, error) {
	var b strings.Builder
	b.WriteString("// Code generated by goframe inspectdb; DO NOT EDIT.\n")
	b.WriteString("\n")
	b.WriteString("package " + packageName + "\n")
	b.WriteString("\n")

	if inspectNeedsTimeImport(models) {
		b.WriteString("import \"time\"\n")
		b.WriteString("\n")
	}

	for i, model := range models {
		b.WriteString(fmt.Sprintf("type %s struct {\n", model.StructName))
		for _, col := range model.Columns {
			b.WriteString(fmt.Sprintf("\t%s %s `%s`\n", col.FieldName, col.GoType, col.Tag))
		}
		b.WriteString("}\n")
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("func (%s) TableName() string {\n", model.StructName))
		b.WriteString(fmt.Sprintf("\treturn %q\n", model.TableName))
		b.WriteString("}\n")
		if i < len(models)-1 {
			b.WriteString("\n")
		}
	}

	raw := []byte(b.String())
	formatted, err := format.Source(raw)
	if err != nil {
		return nil, fmt.Errorf("format inspectdb output: %w", err)
	}
	return formatted, nil
}

func inspectNeedsTimeImport(models []inspectModel) bool {
	for _, model := range models {
		for _, col := range model.Columns {
			if strings.Contains(col.GoType, "time.Time") {
				return true
			}
		}
	}
	return false
}

func buildInspectTag(column string, nullable, primaryKey bool) string {
	parts := []string{"column:" + column}
	if primaryKey {
		parts = append(parts, "primaryKey")
	}
	if !nullable {
		parts = append(parts, "required")
	}
	return fmt.Sprintf(`db:"%s"`, strings.Join(parts, ";"))
}

func tableToStructName(table string) string {
	normalized := strings.TrimSpace(table)
	if normalized == "" {
		return "Table"
	}
	normalized = singularizeTableName(normalized)
	name := toPascalCase(normalized)
	if name == "" {
		return "Table"
	}
	if !unicode.IsLetter([]rune(name)[0]) {
		name = "Table" + name
	}
	return name
}

func singularizeTableName(name string) string {
	switch {
	case strings.HasSuffix(name, "ies") && len(name) > 3:
		return name[:len(name)-3] + "y"
	case strings.HasSuffix(name, "ses") || strings.HasSuffix(name, "xes") || strings.HasSuffix(name, "zes") || strings.HasSuffix(name, "ches") || strings.HasSuffix(name, "shes"):
		return name[:len(name)-2]
	case strings.HasSuffix(name, "s") && len(name) > 1:
		return name[:len(name)-1]
	default:
		return name
	}
}

func columnToFieldName(column string) string {
	if strings.EqualFold(strings.TrimSpace(column), "id") {
		return "ID"
	}

	name := toPascalCase(column)
	if name == "" {
		name = "Field"
	}
	name = normalizeInitialismSuffix(name)
	if !unicode.IsLetter([]rune(name)[0]) {
		name = "Field" + name
	}
	if name == "TableName" {
		name = "TableNameField"
	}
	return name
}

func normalizeInitialismSuffix(name string) string {
	replacements := map[string]string{
		"Id":   "ID",
		"Url":  "URL",
		"Api":  "API",
		"Uuid": "UUID",
	}
	for suffix, replacement := range replacements {
		if strings.HasSuffix(name, suffix) {
			return strings.TrimSuffix(name, suffix) + replacement
		}
	}
	return name
}

func uniqueStructName(base string, used map[string]int) string {
	count := used[base]
	used[base] = count + 1
	if count == 0 {
		return base
	}
	return fmt.Sprintf("%s%d", base, count+1)
}

func uniqueFieldName(base string, used map[string]int) string {
	count := used[base]
	used[base] = count + 1
	if count == 0 {
		return base
	}
	return fmt.Sprintf("%s%d", base, count+1)
}

func mapInspectType(flavor dbFlavor, dbType string, nullable, primaryKey bool) string {
	base := strings.ToLower(strings.TrimSpace(dbType))

	var goType string
	switch flavor {
	case dbFlavorSQLite:
		goType = mapSQLiteType(base)
	case dbFlavorPostgres:
		goType = mapPostgresType(base)
	case dbFlavorMySQL:
		goType = mapMySQLType(base)
	case dbFlavorMSSQL:
		goType = mapMSSQLType(base)
	case dbFlavorOracle:
		goType = mapOracleType(base)
	default:
		goType = "string"
	}

	if nullable && !primaryKey && goType != "[]byte" {
		return "*" + goType
	}
	return goType
}

func mapSQLiteType(dbType string) string {
	switch {
	case strings.Contains(dbType, "int"):
		return "int64"
	case strings.Contains(dbType, "bool"):
		return "bool"
	case strings.Contains(dbType, "real"), strings.Contains(dbType, "floa"), strings.Contains(dbType, "doub"), strings.Contains(dbType, "numeric"), strings.Contains(dbType, "dec"):
		return "float64"
	case strings.Contains(dbType, "date"), strings.Contains(dbType, "time"):
		return "time.Time"
	case strings.Contains(dbType, "blob"), strings.Contains(dbType, "binary"):
		return "[]byte"
	default:
		return "string"
	}
}

func mapPostgresType(dbType string) string {
	switch {
	case strings.Contains(dbType, "int"), strings.Contains(dbType, "serial"):
		return "int64"
	case dbType == "bool" || strings.Contains(dbType, "boolean"):
		return "bool"
	case strings.Contains(dbType, "numeric"), strings.Contains(dbType, "decimal"), strings.Contains(dbType, "float"), strings.Contains(dbType, "double"), strings.Contains(dbType, "real"), strings.Contains(dbType, "money"):
		return "float64"
	case strings.Contains(dbType, "date"), strings.Contains(dbType, "time"):
		return "time.Time"
	case strings.Contains(dbType, "bytea"):
		return "[]byte"
	default:
		return "string"
	}
}

func mapMySQLType(dbType string) string {
	switch {
	case strings.HasPrefix(dbType, "tinyint(1"):
		return "bool"
	case strings.Contains(dbType, "int"):
		return "int64"
	case strings.Contains(dbType, "bool"):
		return "bool"
	case strings.Contains(dbType, "decimal"), strings.Contains(dbType, "float"), strings.Contains(dbType, "double"):
		return "float64"
	case strings.Contains(dbType, "date"), strings.Contains(dbType, "time"), strings.Contains(dbType, "year"):
		return "time.Time"
	case strings.Contains(dbType, "blob"), strings.Contains(dbType, "binary"):
		return "[]byte"
	default:
		return "string"
	}
}

func mapMSSQLType(dbType string) string {
	switch {
	case strings.Contains(dbType, "int"):
		return "int64"
	case dbType == "bit" || strings.Contains(dbType, "bool"):
		return "bool"
	case strings.Contains(dbType, "decimal"), strings.Contains(dbType, "numeric"), strings.Contains(dbType, "float"), strings.Contains(dbType, "real"), strings.Contains(dbType, "money"):
		return "float64"
	case strings.Contains(dbType, "date"), strings.Contains(dbType, "time"):
		return "time.Time"
	case strings.Contains(dbType, "binary"), strings.Contains(dbType, "varbinary"), strings.Contains(dbType, "image"), strings.Contains(dbType, "rowversion"), strings.Contains(dbType, "timestamp"):
		return "[]byte"
	default:
		return "string"
	}
}

func mapOracleType(dbType string) string {
	switch {
	case dbType == "number", dbType == "integer", dbType == "smallint":
		return "int64"
	case strings.Contains(dbType, "float"), strings.Contains(dbType, "binary_double"), strings.Contains(dbType, "binary_float"), strings.Contains(dbType, "decimal"), strings.Contains(dbType, "numeric"):
		return "float64"
	case strings.Contains(dbType, "date"), strings.Contains(dbType, "time"), strings.Contains(dbType, "timestamp"):
		return "time.Time"
	case strings.Contains(dbType, "blob"), strings.Contains(dbType, "raw"), strings.Contains(dbType, "long raw"):
		return "[]byte"
	default:
		return "string"
	}
}

func isValidGoPackageName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case i == 0 && (r == '_' || unicode.IsLetter(r)):
		case i > 0 && (r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)):
		default:
			return false
		}
	}
	return true
}
