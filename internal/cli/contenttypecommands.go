package cli

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
)

const defaultContentTypesTableName = "goframe_content_types"

func runRemoveStaleContentTypes(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("remove_stale_contenttypes", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to goframe config file")
	databaseAlias := fs.String("database", "", "Database alias to use (defaults to database_default)")
	table := fs.String("table", defaultContentTypesTableName, "Content types table name")
	column := fs.String("column", "model", "Column storing model/table names")
	keepRaw := fs.String("keep", "", "Comma-separated model names to keep even if table is missing")
	dryRun := fs.Bool("dry-run", false, "Print SQL and stale entries without deleting")
	force := fs.Bool("force", false, "Force destructive actions (recommended in CI)")
	yes := fs.Bool("yes", false, "Auto-confirm destructive actions without prompt")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("remove_stale_contenttypes does not accept positional arguments")
	}

	targetTable := strings.TrimSpace(*table)
	if err := validateSQLIdentifier(targetTable); err != nil {
		return err
	}
	targetColumn := strings.TrimSpace(*column)
	if err := validateSQLIdentifier(targetColumn); err != nil {
		return err
	}

	keepSet, err := parseNormalizedNameSet(*keepRaw)
	if err != nil {
		return err
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
	exists, err := sqlTableExists(sqlDB, flavor, targetTable)
	if err != nil {
		return err
	}
	if !exists {
		fmt.Fprintf(stdout, "Content types table %s not found; nothing to remove\n", targetTable)
		return nil
	}

	userTables, err := listUserTables(sqlDB, flavor)
	if err != nil {
		return err
	}

	stale, err := findStaleContentTypes(sqlDB, flavor, targetTable, targetColumn, userTables, keepSet)
	if err != nil {
		return err
	}
	if len(stale) == 0 {
		fmt.Fprintf(stdout, "No stale content types found in %s\n", targetTable)
		return nil
	}

	statements := buildRemoveStaleContentTypeStatements(flavor, targetTable, targetColumn, stale)
	if *dryRun {
		fmt.Fprint(stdout, renderSQLStatements(statements))
		fmt.Fprintf(stdout, "DRY-RUN\tREMOVE_STALE_CONTENTTYPES\tstale=%s\n", strings.Join(stale, ","))
		return nil
	}

	if err := requireDangerousApproval(cfg, stdin, stdout, *force, *yes, "remove_stale_contenttypes"); err != nil {
		return err
	}

	var affected int64
	for _, statement := range statements {
		res, err := sqlDB.Exec(statement)
		if err != nil {
			return fmt.Errorf("remove stale content types: %w", err)
		}
		rows, _ := res.RowsAffected()
		affected += rows
	}

	fmt.Fprintf(stdout, "Removed stale content types: table=%s entries=%d rows=%d\n", targetTable, len(stale), affected)
	return nil
}

func findStaleContentTypes(
	sqlDB *sql.DB,
	flavor dbFlavor,
	table string,
	column string,
	userTables []string,
	keepSet map[string]struct{},
) ([]string, error) {
	query := fmt.Sprintf(
		"SELECT DISTINCT %s FROM %s WHERE %s IS NOT NULL AND TRIM(%s) <> ''",
		quoteIdentifier(flavor, column),
		quoteIdentifier(flavor, table),
		quoteIdentifier(flavor, column),
		quoteIdentifier(flavor, column),
	)

	values, err := scanSingleTextColumn(sqlDB, query)
	if err != nil {
		return nil, err
	}

	tableSet := make(map[string]struct{}, len(userTables))
	for _, tableName := range userTables {
		tableSet[strings.ToLower(strings.TrimSpace(tableName))] = struct{}{}
	}

	seen := make(map[string]struct{}, len(values))
	stale := make([]string, 0, len(values))
	for _, raw := range values {
		normalized := strings.ToLower(strings.TrimSpace(raw))
		if normalized == "" {
			continue
		}
		if _, keep := keepSet[normalized]; keep {
			continue
		}
		if _, exists := tableSet[normalized]; exists {
			continue
		}
		if _, dup := seen[normalized]; dup {
			continue
		}
		seen[normalized] = struct{}{}
		stale = append(stale, normalized)
	}

	sort.Strings(stale)
	return stale, nil
}

func buildRemoveStaleContentTypeStatements(flavor dbFlavor, table, column string, stale []string) []string {
	statements := make([]string, 0, len(stale))
	for _, value := range stale {
		statements = append(statements,
			fmt.Sprintf(
				"DELETE FROM %s WHERE LOWER(TRIM(%s)) = %s",
				quoteIdentifier(flavor, table),
				quoteIdentifier(flavor, column),
				quoteSQLString(value),
			),
		)
	}
	return statements
}

func parseNormalizedNameSet(raw string) (map[string]struct{}, error) {
	out := make(map[string]struct{})
	if strings.TrimSpace(raw) == "" {
		return out, nil
	}

	for _, token := range strings.Split(raw, ",") {
		name := strings.ToLower(strings.TrimSpace(token))
		if name == "" {
			continue
		}
		if err := validateSQLIdentifier(name); err != nil {
			return nil, err
		}
		out[name] = struct{}{}
	}
	return out, nil
}
