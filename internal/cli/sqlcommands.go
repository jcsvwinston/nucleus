package cli

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const schemaMigrationsTable = "goframe_schema_migrations"

type dbFlavor string

const (
	dbFlavorSQLite   dbFlavor = "sqlite"
	dbFlavorPostgres dbFlavor = "postgres"
	dbFlavorMySQL    dbFlavor = "mysql"
	dbFlavorUnknown  dbFlavor = "unknown"
)

type migrationPair struct {
	ID       string
	UpPath   string
	DownPath string
}

func runSQLMigrate(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("sqlmigrate", flag.ContinueOnError)
	fs.SetOutput(stderr)

	migrationsPath := fs.String("migrations", "migrations", "Migrations directory")
	down := fs.Bool("down", false, "Print rollback SQL (.down.sql) instead of apply SQL (.up.sql)")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	rest := fs.Args()
	if len(rest) != 1 {
		return fmt.Errorf("usage: goframe sqlmigrate [--migrations migrations] [--down] <migration_id_or_name>")
	}

	pairs, err := loadMigrationPairs(*migrationsPath)
	if err != nil {
		return err
	}
	mig, err := resolveMigrationRef(rest[0], pairs)
	if err != nil {
		return err
	}

	path := mig.UpPath
	if *down {
		path = mig.DownPath
		if path == "" {
			return fmt.Errorf("migration %q does not have a .down.sql file", mig.ID)
		}
	}

	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read migration file %s: %w", path, err)
	}

	fmt.Fprint(stdout, string(body))
	if len(body) == 0 || body[len(body)-1] != '\n' {
		fmt.Fprintln(stdout)
	}
	return nil
}

func runSQLFlush(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("sqlflush", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to goframe config file")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("sqlflush does not accept positional arguments")
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
	tables, err := listUserTables(sqlDB, flavor)
	if err != nil {
		return err
	}
	stmts := buildFlushStatements(flavor, tables)
	fmt.Fprint(stdout, renderSQLStatements(stmts))
	return nil
}

func runFlush(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("flush", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to goframe config file")
	force := fs.Bool("force", false, "Force destructive actions (recommended in CI)")
	yes := fs.Bool("yes", false, "Auto-confirm destructive actions without prompt")
	dryRun := fs.Bool("dry-run", false, "Print generated SQL and exit")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("flush does not accept positional arguments")
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
	tables, err := listUserTables(sqlDB, flavor)
	if err != nil {
		return err
	}
	stmts := buildFlushStatements(flavor, tables)
	sqlScript := renderSQLStatements(stmts)

	if *dryRun {
		fmt.Fprint(stdout, sqlScript)
		return nil
	}
	if err := requireDangerousApproval(cfg, stdin, stdout, *force, *yes, "flush"); err != nil {
		return err
	}

	if err := executeSQLScript(sqlDB, sqlScript); err != nil {
		return fmt.Errorf("flush execution failed: %w", err)
	}
	fmt.Fprintf(stdout, "Database flushed (%d table(s) processed)\n", len(tables))
	return nil
}

func runSQLSequenceReset(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("sqlsequencereset", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to goframe config file")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
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

	tables := fs.Args()
	if len(tables) == 0 {
		tables, err = listUserTables(sqlDB, flavor)
		if err != nil {
			return err
		}
	}
	tables = normalizeTableList(tables)

	stmts := buildSequenceResetStatements(flavor, tables)
	fmt.Fprint(stdout, renderSQLStatements(stmts))
	return nil
}

func detectDBFlavor(databaseURL string) dbFlavor {
	lower := strings.ToLower(strings.TrimSpace(databaseURL))
	switch {
	case strings.HasPrefix(lower, "sqlite://") || strings.HasSuffix(lower, ".db") || strings.HasSuffix(lower, ".sqlite") || lower == ":memory:":
		return dbFlavorSQLite
	case strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://"):
		return dbFlavorPostgres
	case strings.HasPrefix(lower, "mysql://"):
		return dbFlavorMySQL
	default:
		return dbFlavorUnknown
	}
}

func listUserTables(sqlDB *sql.DB, flavor dbFlavor) ([]string, error) {
	var query string
	switch flavor {
	case dbFlavorSQLite:
		query = "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name"
	case dbFlavorPostgres:
		query = "SELECT tablename FROM pg_catalog.pg_tables WHERE schemaname='public' ORDER BY tablename"
	case dbFlavorMySQL:
		query = "SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() ORDER BY table_name"
	default:
		return nil, fmt.Errorf("unsupported database engine for sql helpers")
	}

	rows, err := sqlDB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query tables: %w", err)
	}
	defer rows.Close()

	out := make([]string, 0, 16)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan table name: %w", err)
		}
		if shouldSkipSQLTable(name) {
			continue
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read tables: %w", err)
	}
	return out, nil
}

func shouldSkipSQLTable(name string) bool {
	switch strings.TrimSpace(strings.ToLower(name)) {
	case "", schemaMigrationsTable:
		return true
	default:
		return false
	}
}

func buildFlushStatements(flavor dbFlavor, tables []string) []string {
	if len(tables) == 0 {
		return []string{"-- no user tables found"}
	}

	switch flavor {
	case dbFlavorSQLite:
		stmts := make([]string, 0, len(tables)*2+2)
		stmts = append(stmts, "PRAGMA foreign_keys = OFF")
		for _, table := range tables {
			stmts = append(stmts, "DELETE FROM "+quoteIdentifier(flavor, table))
			stmts = append(stmts, "DELETE FROM sqlite_sequence WHERE name = "+quoteSQLString(table))
		}
		stmts = append(stmts, "PRAGMA foreign_keys = ON")
		return stmts

	case dbFlavorPostgres:
		quoted := make([]string, 0, len(tables))
		for _, table := range tables {
			quoted = append(quoted, quoteIdentifier(flavor, table))
		}
		return []string{"TRUNCATE TABLE " + strings.Join(quoted, ", ") + " RESTART IDENTITY CASCADE"}

	case dbFlavorMySQL:
		stmts := make([]string, 0, len(tables)+2)
		stmts = append(stmts, "SET FOREIGN_KEY_CHECKS = 0")
		for _, table := range tables {
			stmts = append(stmts, "TRUNCATE TABLE "+quoteIdentifier(flavor, table))
		}
		stmts = append(stmts, "SET FOREIGN_KEY_CHECKS = 1")
		return stmts

	default:
		return []string{"-- unsupported database engine"}
	}
}

func buildSequenceResetStatements(flavor dbFlavor, tables []string) []string {
	if len(tables) == 0 {
		return []string{"-- no user tables found"}
	}

	switch flavor {
	case dbFlavorSQLite:
		stmts := make([]string, 0, len(tables))
		for _, table := range tables {
			stmts = append(stmts, "DELETE FROM sqlite_sequence WHERE name = "+quoteSQLString(table))
		}
		return stmts

	case dbFlavorPostgres:
		stmts := make([]string, 0, len(tables))
		for _, table := range tables {
			quoted := quoteIdentifier(flavor, table)
			// Convention: reset sequence bound to "id" column.
			stmts = append(stmts, fmt.Sprintf("SELECT setval(pg_get_serial_sequence(%s, 'id'), COALESCE(MAX(id), 1), false) FROM %s", quoteSQLString(table), quoted))
		}
		return stmts

	case dbFlavorMySQL:
		stmts := make([]string, 0, len(tables))
		for _, table := range tables {
			stmts = append(stmts, "ALTER TABLE "+quoteIdentifier(flavor, table)+" AUTO_INCREMENT = 1")
		}
		return stmts

	default:
		return []string{"-- unsupported database engine"}
	}
}

func quoteIdentifier(flavor dbFlavor, name string) string {
	switch flavor {
	case dbFlavorMySQL:
		return "`" + strings.ReplaceAll(name, "`", "``") + "`"
	default:
		return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	}
}

func renderSQLStatements(stmts []string) string {
	var b strings.Builder
	for _, statement := range stmts {
		stmt := strings.TrimSpace(statement)
		if stmt == "" {
			continue
		}
		b.WriteString(stmt)
		if !strings.HasPrefix(stmt, "--") && !strings.HasSuffix(stmt, ";") {
			b.WriteString(";")
		}
		b.WriteString("\n")
	}
	if b.Len() == 0 {
		return "-- no statements generated\n"
	}
	return b.String()
}

func normalizeTableList(input []string) []string {
	seen := make(map[string]struct{}, len(input))
	out := make([]string, 0, len(input))
	for _, raw := range input {
		name := strings.TrimSpace(raw)
		if name == "" || shouldSkipSQLTable(name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func loadMigrationPairs(migrationsPath string) ([]migrationPair, error) {
	if migrationsPath == "" {
		migrationsPath = "migrations"
	}
	if err := ensureDir(migrationsPath); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(migrationsPath)
	if err != nil {
		return nil, fmt.Errorf("read migrations directory: %w", err)
	}

	byID := make(map[string]*migrationPair)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		id, kind, ok := parseMigrationFilename(entry.Name())
		if !ok {
			continue
		}

		item := byID[id]
		if item == nil {
			item = &migrationPair{ID: id}
			byID[id] = item
		}

		path := filepath.Join(migrationsPath, entry.Name())
		switch kind {
		case "up":
			item.UpPath = path
		case "down":
			item.DownPath = path
		}
	}

	out := make([]migrationPair, 0, len(byID))
	for _, item := range byID {
		if item.UpPath == "" {
			return nil, fmt.Errorf("migration %q is missing .up.sql file", item.ID)
		}
		out = append(out, *item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func parseMigrationFilename(name string) (id string, kind string, ok bool) {
	switch {
	case strings.HasSuffix(name, ".up.sql"):
		return strings.TrimSuffix(name, ".up.sql"), "up", true
	case strings.HasSuffix(name, ".down.sql"):
		return strings.TrimSuffix(name, ".down.sql"), "down", true
	default:
		return "", "", false
	}
}

func resolveMigrationRef(ref string, migrations []migrationPair) (*migrationPair, error) {
	needle := strings.TrimSpace(ref)
	if needle == "" {
		return nil, fmt.Errorf("migration reference cannot be empty")
	}

	for i := range migrations {
		if migrations[i].ID == needle {
			return &migrations[i], nil
		}
	}

	candidates := make([]*migrationPair, 0, 4)
	suffix := "_" + needle
	for i := range migrations {
		id := migrations[i].ID
		if strings.HasSuffix(id, suffix) || strings.Contains(id, needle) {
			candidates = append(candidates, &migrations[i])
		}
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("migration %q not found", needle)
	}
	if len(candidates) > 1 {
		names := make([]string, 0, len(candidates))
		for _, item := range candidates {
			names = append(names, item.ID)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("migration %q is ambiguous; matches: %s", needle, strings.Join(names, ", "))
	}
	return candidates[0], nil
}
