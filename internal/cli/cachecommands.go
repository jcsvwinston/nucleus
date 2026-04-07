package cli

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

const (
	defaultCacheTableName    = "goframe_cache_entries"
	defaultSessionsTableName = "goframe_sessions"
)

func runCreateCacheTable(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("createcachetable", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to goframe config file")
	table := fs.String("table", defaultCacheTableName, "Cache table name")
	dryRun := fs.Bool("dry-run", false, "Print SQL and exit without executing")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("createcachetable does not accept positional arguments")
	}

	targetTable := strings.TrimSpace(*table)
	if err := validateSQLIdentifier(targetTable); err != nil {
		return err
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
	statements, err := buildCreateCacheTableStatements(flavor, targetTable)
	if err != nil {
		return err
	}
	sqlScript := renderSQLStatements(statements)

	if *dryRun {
		fmt.Fprint(stdout, sqlScript)
		return nil
	}

	if err := executeSQLScript(sqlDB, sqlScript); err != nil {
		return fmt.Errorf("create cache table: %w", err)
	}
	fmt.Fprintf(stdout, "Cache table ready: %s\n", targetTable)
	return nil
}

func runClearSessions(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("clearsessions", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to goframe config file")
	table := fs.String("table", defaultSessionsTableName, "Sessions table name")
	all := fs.Bool("all", false, "Delete all sessions instead of only expired sessions")
	dryRun := fs.Bool("dry-run", false, "Print SQL and exit without executing")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("clearsessions does not accept positional arguments")
	}

	targetTable := strings.TrimSpace(*table)
	if err := validateSQLIdentifier(targetTable); err != nil {
		return err
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
	exists, err := sqlTableExists(sqlDB, flavor, targetTable)
	if err != nil {
		return err
	}
	if !exists {
		fmt.Fprintf(stdout, "Session table %s not found; nothing to clear\n", targetTable)
		return nil
	}

	statement, mode, err := buildClearSessionsStatement(sqlDB, flavor, targetTable, *all)
	if err != nil {
		return err
	}

	if *dryRun {
		fmt.Fprint(stdout, renderSQLStatements([]string{statement}))
		return nil
	}

	res, err := sqlDB.Exec(statement)
	if err != nil {
		return fmt.Errorf("clear sessions: %w", err)
	}
	affected, _ := res.RowsAffected()
	fmt.Fprintf(stdout, "Sessions cleared (%s): table=%s rows=%d\n", mode, targetTable, affected)
	return nil
}

func buildCreateCacheTableStatements(flavor dbFlavor, table string) ([]string, error) {
	if err := validateSQLIdentifier(table); err != nil {
		return nil, err
	}
	indexName := table + "_expires_idx"
	if err := validateSQLIdentifier(indexName); err != nil {
		return nil, err
	}

	q := func(name string) string { return quoteIdentifier(flavor, name) }
	qt := q(table)
	qi := q(indexName)

	switch flavor {
	case dbFlavorSQLite:
		return []string{
			fmt.Sprintf(
				"CREATE TABLE IF NOT EXISTS %s (%s TEXT PRIMARY KEY, %s BLOB NOT NULL, %s TEXT NOT NULL, %s TEXT NOT NULL, %s TEXT NOT NULL)",
				qt, q("cache_key"), q("value"), q("expires_at"), q("created_at"), q("updated_at"),
			),
			fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s (%s)", qi, qt, q("expires_at")),
		}, nil

	case dbFlavorPostgres:
		return []string{
			fmt.Sprintf(
				"CREATE TABLE IF NOT EXISTS %s (%s VARCHAR(255) PRIMARY KEY, %s BYTEA NOT NULL, %s TIMESTAMPTZ NOT NULL, %s TIMESTAMPTZ NOT NULL DEFAULT NOW(), %s TIMESTAMPTZ NOT NULL DEFAULT NOW())",
				qt, q("cache_key"), q("value"), q("expires_at"), q("created_at"), q("updated_at"),
			),
			fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s (%s)", qi, qt, q("expires_at")),
		}, nil

	case dbFlavorMySQL:
		return []string{
			fmt.Sprintf(
				"CREATE TABLE IF NOT EXISTS %s (%s VARCHAR(255) PRIMARY KEY, %s LONGBLOB NOT NULL, %s DATETIME NOT NULL, %s DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, %s DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP, INDEX %s (%s))",
				qt, q("cache_key"), q("value"), q("expires_at"), q("created_at"), q("updated_at"), qi, q("expires_at"),
			),
		}, nil

	case dbFlavorMSSQL:
		return []string{
			fmt.Sprintf(
				"IF OBJECT_ID(N'%s', N'U') IS NULL CREATE TABLE %s (%s NVARCHAR(255) PRIMARY KEY, %s VARBINARY(MAX) NOT NULL, %s DATETIME2 NOT NULL, %s DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(), %s DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME())",
				table, qt, q("cache_key"), q("value"), q("expires_at"), q("created_at"), q("updated_at"),
			),
			fmt.Sprintf(
				"IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'%s' AND object_id = OBJECT_ID(N'%s')) CREATE INDEX %s ON %s (%s)",
				indexName, table, qi, qt, q("expires_at"),
			),
		}, nil

	case dbFlavorOracle:
		createTable := fmt.Sprintf(
			"CREATE TABLE %s (%s VARCHAR2(255 CHAR) PRIMARY KEY, %s BLOB NOT NULL, %s TIMESTAMP WITH TIME ZONE NOT NULL, %s TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP NOT NULL, %s TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP NOT NULL)",
			qt, q("cache_key"), q("value"), q("expires_at"), q("created_at"), q("updated_at"),
		)
		createIndex := fmt.Sprintf("CREATE INDEX %s ON %s (%s)", qi, qt, q("expires_at"))
		return []string{
			oracleDDLBlock(createTable),
			oracleDDLBlock(createIndex),
		}, nil

	default:
		return nil, fmt.Errorf("unsupported database engine for createcachetable")
	}
}

func oracleDDLBlock(statement string) string {
	escaped := strings.ReplaceAll(statement, "'", "''")
	return "BEGIN EXECUTE IMMEDIATE '" + escaped + "'; EXCEPTION WHEN OTHERS THEN IF SQLCODE != -955 THEN RAISE; END IF; END;"
}

func buildClearSessionsStatement(sqlDB *sql.DB, flavor dbFlavor, table string, all bool) (string, string, error) {
	quotedTable := quoteIdentifier(flavor, table)
	if all {
		return fmt.Sprintf("DELETE FROM %s", quotedTable), "all", nil
	}

	columns, err := listTableColumns(sqlDB, flavor, table)
	if err != nil {
		return "", "", err
	}
	expiryColumn := selectSessionExpiryColumn(columns)
	if expiryColumn == "" {
		return "", "", fmt.Errorf("sessions table %q does not have an expiry column (tried: expires_at, expiry, expires, expires_at_utc)", table)
	}

	return fmt.Sprintf(
		"DELETE FROM %s WHERE %s <= %s",
		quotedTable,
		quoteIdentifier(flavor, expiryColumn),
		currentTimestampExpr(flavor),
	), "expired", nil
}

func currentTimestampExpr(flavor dbFlavor) string {
	switch flavor {
	case dbFlavorPostgres, dbFlavorMySQL:
		return "NOW()"
	case dbFlavorMSSQL:
		return "SYSUTCDATETIME()"
	default:
		return "CURRENT_TIMESTAMP"
	}
}

func selectSessionExpiryColumn(columns []string) string {
	lookup := make(map[string]string, len(columns))
	for _, raw := range columns {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		lookup[strings.ToLower(trimmed)] = trimmed
	}

	candidates := []string{"expires_at", "expiry", "expires", "expires_at_utc"}
	for _, candidate := range candidates {
		if col, ok := lookup[candidate]; ok {
			return col
		}
	}
	return ""
}

func listTableColumns(sqlDB *sql.DB, flavor dbFlavor, table string) ([]string, error) {
	switch flavor {
	case dbFlavorSQLite:
		return listSQLiteColumns(sqlDB, table)
	case dbFlavorPostgres:
		query := fmt.Sprintf(
			"SELECT column_name FROM information_schema.columns WHERE table_schema = 'public' AND table_name = %s ORDER BY ordinal_position",
			quoteSQLString(table),
		)
		return scanSingleTextColumn(sqlDB, query)
	case dbFlavorMySQL:
		query := fmt.Sprintf(
			"SELECT column_name FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = %s ORDER BY ordinal_position",
			quoteSQLString(table),
		)
		return scanSingleTextColumn(sqlDB, query)
	case dbFlavorMSSQL:
		query := fmt.Sprintf(
			"SELECT COLUMN_NAME FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = 'dbo' AND TABLE_NAME = %s ORDER BY ORDINAL_POSITION",
			quoteSQLString(table),
		)
		return scanSingleTextColumn(sqlDB, query)
	case dbFlavorOracle:
		query := fmt.Sprintf(
			"SELECT column_name FROM user_tab_columns WHERE table_name = UPPER(%s) ORDER BY column_id",
			quoteSQLString(table),
		)
		return scanSingleTextColumn(sqlDB, query)
	default:
		return nil, fmt.Errorf("unsupported database engine for clearsessions")
	}
}

func listSQLiteColumns(sqlDB *sql.DB, table string) ([]string, error) {
	query := fmt.Sprintf("PRAGMA table_info(%s)", quoteIdentifier(dbFlavorSQLite, table))
	rows, err := sqlDB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query table columns: %w", err)
	}
	defer rows.Close()

	out := make([]string, 0, 8)
	for rows.Next() {
		var (
			cid       int
			name      string
			colType   string
			notNull   int
			defaultV  sql.NullString
			primaryID int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultV, &primaryID); err != nil {
			return nil, fmt.Errorf("scan sqlite column info: %w", err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read sqlite columns: %w", err)
	}
	return out, nil
}

func scanSingleTextColumn(sqlDB *sql.DB, query string) ([]string, error) {
	rows, err := sqlDB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query table columns: %w", err)
	}
	defer rows.Close()

	out := make([]string, 0, 8)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan table column: %w", err)
		}
		out = append(out, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read table columns: %w", err)
	}
	return out, nil
}

func sqlTableExists(sqlDB *sql.DB, flavor dbFlavor, table string) (bool, error) {
	switch flavor {
	case dbFlavorSQLite:
		query := fmt.Sprintf("SELECT count(*) FROM sqlite_master WHERE type='table' AND name = %s", quoteSQLString(table))
		return queryCountGreaterThanZero(sqlDB, query)
	case dbFlavorPostgres:
		query := fmt.Sprintf("SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = %s", quoteSQLString(table))
		return queryCountGreaterThanZero(sqlDB, query)
	case dbFlavorMySQL:
		query := fmt.Sprintf("SELECT count(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = %s", quoteSQLString(table))
		return queryCountGreaterThanZero(sqlDB, query)
	case dbFlavorMSSQL:
		query := fmt.Sprintf("SELECT count(*) FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA = 'dbo' AND TABLE_NAME = %s", quoteSQLString(table))
		return queryCountGreaterThanZero(sqlDB, query)
	case dbFlavorOracle:
		query := fmt.Sprintf("SELECT count(*) FROM user_tables WHERE table_name = UPPER(%s)", quoteSQLString(table))
		return queryCountGreaterThanZero(sqlDB, query)
	default:
		return false, fmt.Errorf("unsupported database engine for clearsessions")
	}
}

func queryCountGreaterThanZero(sqlDB *sql.DB, query string) (bool, error) {
	var count int
	if err := sqlDB.QueryRow(query).Scan(&count); err != nil {
		return false, fmt.Errorf("query table existence: %w", err)
	}
	return count > 0, nil
}
