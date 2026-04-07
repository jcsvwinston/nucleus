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

type introspectedForeignKey struct {
	Column        string
	ForeignTable  string
	ForeignColumn string
}

type introspectedIndex struct {
	Name    string
	Unique  bool
	Columns []string
}

type introspectedIndexRef struct {
	Name   string
	Unique bool
}

func runInspectDB(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("inspectdb", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to goframe config file")
	databaseAlias := fs.String("database", "", "Database alias to use (defaults to database_default)")
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

	cfg, database, resolvedAlias, cleanup, err := newDatabaseWithAlias(*configPath, *databaseAlias)
	if err != nil {
		return err
	}
	defer cleanup()

	sqlDB, err := database.SqlDB()
	if err != nil {
		return fmt.Errorf("open sql handle: %w", err)
	}
	flavor := detectDBFlavor(databaseURLByAlias(cfg, resolvedAlias))

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
		foreignKeys, err := inspectTableForeignKeys(sqlDB, flavor, table)
		if err != nil {
			return nil, err
		}
		indexes, err := inspectTableIndexes(sqlDB, flavor, table)
		if err != nil {
			return nil, err
		}
		indexRefsByColumn := buildIndexRefsByColumn(indexes)

		structName := uniqueStructName(tableToStructName(table), structNameUsage)

		fieldNameUsage := map[string]int{}
		outColumns := make([]inspectColumn, 0, len(columns))
		for _, col := range columns {
			fieldName := uniqueFieldName(columnToFieldName(col.Name), fieldNameUsage)
			goType := mapInspectType(flavor, col.DBType, col.Nullable, col.PrimaryKey)
			key := inspectColumnKey(col.Name)
			fk := foreignKeys[key]
			tag := buildInspectTag(col.Name, col.Nullable, col.PrimaryKey, fk, indexRefsByColumn[key])
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

func inspectTableForeignKeys(sqlDB *sql.DB, flavor dbFlavor, table string) (map[string]*introspectedForeignKey, error) {
	switch flavor {
	case dbFlavorSQLite:
		return inspectSQLiteForeignKeys(sqlDB, table)
	case dbFlavorPostgres:
		return inspectPostgresForeignKeys(sqlDB, table)
	case dbFlavorMySQL:
		return inspectMySQLForeignKeys(sqlDB, table)
	case dbFlavorMSSQL:
		return inspectMSSQLForeignKeys(sqlDB, table)
	case dbFlavorOracle:
		return inspectOracleForeignKeys(sqlDB, table)
	default:
		return map[string]*introspectedForeignKey{}, nil
	}
}

func inspectTableIndexes(sqlDB *sql.DB, flavor dbFlavor, table string) ([]introspectedIndex, error) {
	switch flavor {
	case dbFlavorSQLite:
		return inspectSQLiteIndexes(sqlDB, table)
	case dbFlavorPostgres:
		return inspectPostgresIndexes(sqlDB, table)
	case dbFlavorMySQL:
		return inspectMySQLIndexes(sqlDB, table)
	case dbFlavorMSSQL:
		return inspectMSSQLIndexes(sqlDB, table)
	case dbFlavorOracle:
		return inspectOracleIndexes(sqlDB, table)
	default:
		return nil, nil
	}
}

func inspectSQLiteForeignKeys(sqlDB *sql.DB, table string) (map[string]*introspectedForeignKey, error) {
	query := fmt.Sprintf("PRAGMA foreign_key_list(%s)", quoteIdentifier(dbFlavorSQLite, table))
	rows, err := sqlDB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("inspectdb sqlite foreign keys %s: %w", table, err)
	}
	defer rows.Close()

	out := map[string]*introspectedForeignKey{}
	for rows.Next() {
		var (
			id       int
			seq      int
			refTable string
			fromCol  string
			toCol    string
			onUpdate string
			onDelete string
			match    string
		)
		if err := rows.Scan(&id, &seq, &refTable, &fromCol, &toCol, &onUpdate, &onDelete, &match); err != nil {
			return nil, fmt.Errorf("inspectdb sqlite foreign key scan %s: %w", table, err)
		}
		if strings.TrimSpace(toCol) == "" {
			toCol = "id"
		}
		out[inspectColumnKey(fromCol)] = &introspectedForeignKey{
			Column:        fromCol,
			ForeignTable:  refTable,
			ForeignColumn: toCol,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspectdb sqlite foreign key rows %s: %w", table, err)
	}
	return out, nil
}

func inspectPostgresForeignKeys(sqlDB *sql.DB, table string) (map[string]*introspectedForeignKey, error) {
	rows, err := sqlDB.Query(
		`SELECT kcu.column_name, ccu.table_name, ccu.column_name
		   FROM information_schema.table_constraints tc
		   JOIN information_schema.key_column_usage kcu
		     ON tc.constraint_name = kcu.constraint_name
		    AND tc.table_schema = kcu.table_schema
		   JOIN information_schema.constraint_column_usage ccu
		     ON ccu.constraint_name = tc.constraint_name
		    AND ccu.constraint_schema = tc.table_schema
		  WHERE tc.table_schema='public'
		    AND tc.table_name = $1
		    AND tc.constraint_type='FOREIGN KEY'`,
		table,
	)
	if err != nil {
		return nil, fmt.Errorf("inspectdb postgres foreign keys %s: %w", table, err)
	}
	defer rows.Close()

	out := map[string]*introspectedForeignKey{}
	for rows.Next() {
		var col, refTable, refCol string
		if err := rows.Scan(&col, &refTable, &refCol); err != nil {
			return nil, fmt.Errorf("inspectdb postgres foreign key scan %s: %w", table, err)
		}
		out[inspectColumnKey(col)] = &introspectedForeignKey{
			Column:        col,
			ForeignTable:  refTable,
			ForeignColumn: refCol,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspectdb postgres foreign key rows %s: %w", table, err)
	}
	return out, nil
}

func inspectMySQLForeignKeys(sqlDB *sql.DB, table string) (map[string]*introspectedForeignKey, error) {
	rows, err := sqlDB.Query(
		`SELECT column_name, referenced_table_name, referenced_column_name
		   FROM information_schema.key_column_usage
		  WHERE table_schema = DATABASE()
		    AND table_name = ?
		    AND referenced_table_name IS NOT NULL`,
		table,
	)
	if err != nil {
		return nil, fmt.Errorf("inspectdb mysql foreign keys %s: %w", table, err)
	}
	defer rows.Close()

	out := map[string]*introspectedForeignKey{}
	for rows.Next() {
		var col, refTable, refCol string
		if err := rows.Scan(&col, &refTable, &refCol); err != nil {
			return nil, fmt.Errorf("inspectdb mysql foreign key scan %s: %w", table, err)
		}
		out[inspectColumnKey(col)] = &introspectedForeignKey{
			Column:        col,
			ForeignTable:  refTable,
			ForeignColumn: refCol,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspectdb mysql foreign key rows %s: %w", table, err)
	}
	return out, nil
}

func inspectMSSQLForeignKeys(sqlDB *sql.DB, table string) (map[string]*introspectedForeignKey, error) {
	rows, err := sqlDB.Query(
		`SELECT ku.COLUMN_NAME, ccu.TABLE_NAME, ccu.COLUMN_NAME
		   FROM INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS rc
		   JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE ku
		     ON rc.CONSTRAINT_NAME = ku.CONSTRAINT_NAME
		    AND rc.CONSTRAINT_SCHEMA = ku.CONSTRAINT_SCHEMA
		   JOIN INFORMATION_SCHEMA.CONSTRAINT_COLUMN_USAGE ccu
		     ON rc.UNIQUE_CONSTRAINT_NAME = ccu.CONSTRAINT_NAME
		    AND rc.UNIQUE_CONSTRAINT_SCHEMA = ccu.CONSTRAINT_SCHEMA
		  WHERE ku.TABLE_SCHEMA = 'dbo'
		    AND ku.TABLE_NAME = @p1`,
		table,
	)
	if err != nil {
		return nil, fmt.Errorf("inspectdb mssql foreign keys %s: %w", table, err)
	}
	defer rows.Close()

	out := map[string]*introspectedForeignKey{}
	for rows.Next() {
		var col, refTable, refCol string
		if err := rows.Scan(&col, &refTable, &refCol); err != nil {
			return nil, fmt.Errorf("inspectdb mssql foreign key scan %s: %w", table, err)
		}
		out[inspectColumnKey(col)] = &introspectedForeignKey{
			Column:        col,
			ForeignTable:  refTable,
			ForeignColumn: refCol,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspectdb mssql foreign key rows %s: %w", table, err)
	}
	return out, nil
}

func inspectOracleForeignKeys(sqlDB *sql.DB, table string) (map[string]*introspectedForeignKey, error) {
	rows, err := sqlDB.Query(
		`SELECT acc.column_name, rcons.table_name, rcols.column_name
		   FROM user_constraints cons
		   JOIN user_cons_columns acc
		     ON cons.constraint_name = acc.constraint_name
		   JOIN user_constraints rcons
		     ON cons.r_constraint_name = rcons.constraint_name
		   JOIN user_cons_columns rcols
		     ON rcons.constraint_name = rcols.constraint_name
		    AND acc.position = rcols.position
		  WHERE cons.constraint_type = 'R'
		    AND cons.table_name = UPPER(:1)`,
		table,
	)
	if err != nil {
		return nil, fmt.Errorf("inspectdb oracle foreign keys %s: %w", table, err)
	}
	defer rows.Close()

	out := map[string]*introspectedForeignKey{}
	for rows.Next() {
		var col, refTable, refCol string
		if err := rows.Scan(&col, &refTable, &refCol); err != nil {
			return nil, fmt.Errorf("inspectdb oracle foreign key scan %s: %w", table, err)
		}
		col = strings.ToLower(col)
		out[inspectColumnKey(col)] = &introspectedForeignKey{
			Column:        col,
			ForeignTable:  strings.ToLower(refTable),
			ForeignColumn: strings.ToLower(refCol),
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspectdb oracle foreign key rows %s: %w", table, err)
	}
	return out, nil
}

func inspectSQLiteIndexes(sqlDB *sql.DB, table string) ([]introspectedIndex, error) {
	listQuery := fmt.Sprintf("PRAGMA index_list(%s)", quoteIdentifier(dbFlavorSQLite, table))
	rows, err := sqlDB.Query(listQuery)
	if err != nil {
		return nil, fmt.Errorf("inspectdb sqlite indexes %s: %w", table, err)
	}
	defer rows.Close()

	indexes := make([]introspectedIndex, 0, 8)
	for rows.Next() {
		var (
			seq     int
			name    string
			unique  int
			origin  string
			partial int
		)
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			return nil, fmt.Errorf("inspectdb sqlite index scan %s: %w", table, err)
		}
		if strings.EqualFold(origin, "pk") {
			continue
		}

		colsQuery := fmt.Sprintf("PRAGMA index_info(%s)", quoteIdentifier(dbFlavorSQLite, name))
		colRows, err := sqlDB.Query(colsQuery)
		if err != nil {
			return nil, fmt.Errorf("inspectdb sqlite index columns %s.%s: %w", table, name, err)
		}
		cols := make([]string, 0, 4)
		for colRows.Next() {
			var (
				seqno int
				cid   int
				col   string
			)
			if err := colRows.Scan(&seqno, &cid, &col); err != nil {
				colRows.Close()
				return nil, fmt.Errorf("inspectdb sqlite index column scan %s.%s: %w", table, name, err)
			}
			cols = append(cols, col)
		}
		if err := colRows.Err(); err != nil {
			colRows.Close()
			return nil, fmt.Errorf("inspectdb sqlite index column rows %s.%s: %w", table, name, err)
		}
		colRows.Close()
		if len(cols) == 0 {
			continue
		}
		indexes = append(indexes, introspectedIndex{
			Name:    name,
			Unique:  unique == 1,
			Columns: cols,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspectdb sqlite index rows %s: %w", table, err)
	}
	return indexes, nil
}

func inspectPostgresIndexes(sqlDB *sql.DB, table string) ([]introspectedIndex, error) {
	rows, err := sqlDB.Query(
		`SELECT idx.relname, i.indisunique, a.attname
		   FROM pg_class tbl
		   JOIN pg_namespace ns
		     ON ns.oid = tbl.relnamespace
		   JOIN pg_index i
		     ON i.indrelid = tbl.oid
		   JOIN pg_class idx
		     ON idx.oid = i.indexrelid
		   JOIN LATERAL unnest(i.indkey) WITH ORDINALITY k(attnum, ord)
		     ON TRUE
		   JOIN pg_attribute a
		     ON a.attrelid = tbl.oid AND a.attnum = k.attnum
		  WHERE ns.nspname = 'public'
		    AND tbl.relname = $1
		    AND i.indisprimary = false
		  ORDER BY idx.relname, k.ord`,
		table,
	)
	if err != nil {
		return nil, fmt.Errorf("inspectdb postgres indexes %s: %w", table, err)
	}
	defer rows.Close()

	return collectOrderedIndexes(rows, func() (string, bool, string, error) {
		var (
			name   string
			unique bool
			column string
		)
		err := rows.Scan(&name, &unique, &column)
		return name, unique, column, err
	}, "inspectdb postgres index scan "+table)
}

func inspectMySQLIndexes(sqlDB *sql.DB, table string) ([]introspectedIndex, error) {
	rows, err := sqlDB.Query(
		`SELECT index_name, non_unique, column_name
		   FROM information_schema.statistics
		  WHERE table_schema = DATABASE()
		    AND table_name = ?
		    AND index_name <> 'PRIMARY'
		  ORDER BY index_name, seq_in_index`,
		table,
	)
	if err != nil {
		return nil, fmt.Errorf("inspectdb mysql indexes %s: %w", table, err)
	}
	defer rows.Close()

	return collectOrderedIndexes(rows, func() (string, bool, string, error) {
		var (
			name      string
			nonUnique int
			column    string
		)
		err := rows.Scan(&name, &nonUnique, &column)
		return name, nonUnique == 0, column, err
	}, "inspectdb mysql index scan "+table)
}

func inspectMSSQLIndexes(sqlDB *sql.DB, table string) ([]introspectedIndex, error) {
	rows, err := sqlDB.Query(
		`SELECT i.name, i.is_unique, c.name
		   FROM sys.indexes i
		   JOIN sys.index_columns ic
		     ON i.object_id = ic.object_id AND i.index_id = ic.index_id
		   JOIN sys.columns c
		     ON ic.object_id = c.object_id AND ic.column_id = c.column_id
		   JOIN sys.tables t
		     ON i.object_id = t.object_id
		   JOIN sys.schemas s
		     ON t.schema_id = s.schema_id
		  WHERE s.name = 'dbo'
		    AND t.name = @p1
		    AND i.is_primary_key = 0
		    AND i.is_hypothetical = 0
		  ORDER BY i.name, ic.key_ordinal`,
		table,
	)
	if err != nil {
		return nil, fmt.Errorf("inspectdb mssql indexes %s: %w", table, err)
	}
	defer rows.Close()

	return collectOrderedIndexes(rows, func() (string, bool, string, error) {
		var (
			name   string
			unique bool
			column string
		)
		err := rows.Scan(&name, &unique, &column)
		return name, unique, column, err
	}, "inspectdb mssql index scan "+table)
}

func inspectOracleIndexes(sqlDB *sql.DB, table string) ([]introspectedIndex, error) {
	rows, err := sqlDB.Query(
		`SELECT idx.index_name, idx.uniqueness, col.column_name
		   FROM user_indexes idx
		   JOIN user_ind_columns col
		     ON idx.index_name = col.index_name
		  WHERE idx.table_name = UPPER(:1)
		    AND idx.generated = 'N'
		  ORDER BY idx.index_name, col.column_position`,
		table,
	)
	if err != nil {
		return nil, fmt.Errorf("inspectdb oracle indexes %s: %w", table, err)
	}
	defer rows.Close()

	return collectOrderedIndexes(rows, func() (string, bool, string, error) {
		var (
			name       string
			uniqueness string
			column     string
		)
		err := rows.Scan(&name, &uniqueness, &column)
		return strings.ToLower(name), strings.EqualFold(uniqueness, "UNIQUE"), strings.ToLower(column), err
	}, "inspectdb oracle index scan "+table)
}

func collectOrderedIndexes(
	rows *sql.Rows,
	scan func() (name string, unique bool, column string, err error),
	context string,
) ([]introspectedIndex, error) {
	ordered := make([]string, 0, 8)
	byName := map[string]*introspectedIndex{}

	for rows.Next() {
		name, unique, column, err := scan()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", context, err)
		}
		name = strings.TrimSpace(name)
		column = strings.TrimSpace(column)
		if name == "" || column == "" {
			continue
		}
		idx, exists := byName[name]
		if !exists {
			idx = &introspectedIndex{Name: name, Unique: unique, Columns: make([]string, 0, 2)}
			byName[name] = idx
			ordered = append(ordered, name)
		}
		if idx.Unique != unique {
			idx.Unique = idx.Unique && unique
		}
		idx.Columns = append(idx.Columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s rows: %w", context, err)
	}

	out := make([]introspectedIndex, 0, len(ordered))
	for _, name := range ordered {
		out = append(out, *byName[name])
	}
	return out, nil
}

func buildIndexRefsByColumn(indexes []introspectedIndex) map[string][]introspectedIndexRef {
	refsByColumn := map[string][]introspectedIndexRef{}
	for _, idx := range indexes {
		if len(idx.Columns) == 0 {
			continue
		}
		for _, column := range idx.Columns {
			ref := introspectedIndexRef{Unique: idx.Unique}
			if len(idx.Columns) > 1 {
				ref.Name = idx.Name
			}
			key := inspectColumnKey(column)
			if containsIndexRef(refsByColumn[key], ref) {
				continue
			}
			refsByColumn[key] = append(refsByColumn[key], ref)
		}
	}
	return refsByColumn
}

func containsIndexRef(values []introspectedIndexRef, needle introspectedIndexRef) bool {
	for _, v := range values {
		if v.Name == needle.Name && v.Unique == needle.Unique {
			return true
		}
	}
	return false
}

func inspectColumnKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
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

func buildInspectTag(
	column string,
	nullable, primaryKey bool,
	foreignKey *introspectedForeignKey,
	indexRefs []introspectedIndexRef,
) string {
	parts := []string{"column:" + column}
	if primaryKey {
		parts = append(parts, "pk")
	}
	if !nullable {
		parts = append(parts, "required")
	}
	if foreignKey != nil {
		foreignTable := strings.TrimSpace(foreignKey.ForeignTable)
		foreignColumn := strings.TrimSpace(foreignKey.ForeignColumn)
		if foreignTable != "" {
			if foreignColumn == "" {
				foreignColumn = "id"
			}
			parts = append(parts, "fk:"+foreignTable+"."+foreignColumn)
		}
	}
	for _, ref := range indexRefs {
		key := "index"
		if ref.Unique {
			key = "unique"
		}
		if strings.TrimSpace(ref.Name) != "" {
			parts = append(parts, key+":"+ref.Name)
			continue
		}
		parts = append(parts, key)
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
