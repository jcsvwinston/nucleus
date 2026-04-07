package cli

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type fixtureDocument struct {
	GeneratedAt string         `json:"generated_at,omitempty"`
	Engine      string         `json:"engine,omitempty"`
	Tables      []tableFixture `json:"tables"`
}

type tableFixture struct {
	Name string           `json:"name"`
	Rows []map[string]any `json:"rows"`
}

func runDumpData(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("dumpdata", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to goframe config file")
	databaseAlias := fs.String("database", "", "Database alias to use (defaults to database_default)")
	tablesRaw := fs.String("tables", "", "Comma-separated table list to export (default: all user tables)")
	excludeRaw := fs.String("exclude", "", "Comma-separated table list to exclude")
	outputPath := fs.String("output", "-", "Output JSON file path ('-' for stdout)")
	pretty := fs.Bool("pretty", true, "Pretty-print JSON output")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("dumpdata does not accept positional arguments")
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

	selected, err := selectTablesForDump(allTables, parseTableCSV(*tablesRaw), parseTableCSV(*excludeRaw))
	if err != nil {
		return err
	}

	doc := fixtureDocument{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Engine:      string(flavor),
		Tables:      make([]tableFixture, 0, len(selected)),
	}

	for _, table := range selected {
		rows, err := dumpTableRows(sqlDB, flavor, table)
		if err != nil {
			return err
		}
		doc.Tables = append(doc.Tables, tableFixture{
			Name: table,
			Rows: rows,
		})
	}

	var raw []byte
	if *pretty {
		raw, err = json.MarshalIndent(doc, "", "  ")
	} else {
		raw, err = json.Marshal(doc)
	}
	if err != nil {
		return fmt.Errorf("encode fixture JSON: %w", err)
	}
	raw = append(raw, '\n')

	target := strings.TrimSpace(*outputPath)
	switch target {
	case "", "-":
		_, _ = stdout.Write(raw)
	default:
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
		if err := os.WriteFile(target, raw, 0644); err != nil {
			return fmt.Errorf("write fixture output: %w", err)
		}
		fmt.Fprintf(stdout, "Fixture dump written: %s (%d table(s))\n", target, len(doc.Tables))
	}

	return nil
}

func runLoadData(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("loaddata", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to goframe config file")
	databaseAlias := fs.String("database", "", "Database alias to use (defaults to database_default)")
	filePath := fs.String("file", "", "Path to fixture JSON file")
	tablesRaw := fs.String("tables", "", "Comma-separated subset of fixture tables to load")
	truncate := fs.Bool("truncate", false, "Truncate target tables before loading fixtures")
	dryRun := fs.Bool("dry-run", false, "Print loading plan without modifying data")
	force := fs.Bool("force", false, "Force destructive actions (recommended in CI)")
	yes := fs.Bool("yes", false, "Auto-confirm destructive actions without prompt")

	fixtureFirst := ""
	parseArgs := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		fixtureFirst = strings.TrimSpace(args[0])
		parseArgs = args[1:]
	}

	if err := fs.Parse(parseArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	rest := fs.Args()
	if fixtureFirst != "" {
		rest = append([]string{fixtureFirst}, rest...)
	}

	path := strings.TrimSpace(*filePath)
	if path == "" {
		if len(rest) != 1 {
			return fmt.Errorf("usage: goframe loaddata [--config goframe.yaml] [--truncate] [--dry-run] <fixture.json>")
		}
		path = rest[0]
	} else if len(rest) > 0 {
		return fmt.Errorf("loaddata accepts either positional fixture path or --file, not both")
	}

	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read fixture file %s: %w", path, err)
	}

	doc, err := decodeFixtureDocument(body)
	if err != nil {
		return err
	}
	if len(doc.Tables) == 0 {
		fmt.Fprintln(stdout, "No fixture tables found")
		return nil
	}

	selectedTables := parseTableCSV(*tablesRaw)
	selectedFixtures, err := selectFixtures(doc.Tables, selectedTables)
	if err != nil {
		return err
	}
	if len(selectedFixtures) == 0 {
		fmt.Fprintln(stdout, "No fixture tables selected")
		return nil
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

	targetTables := fixtureTableNames(selectedFixtures)
	if *truncate {
		if err := requireDangerousApproval(cfg, stdin, stdout, *force, *yes, "loaddata --truncate"); err != nil {
			return err
		}
	}

	if *dryRun {
		if *truncate {
			fmt.Fprintln(stdout, "DRY-RUN flush SQL:")
			fmt.Fprint(stdout, renderSQLStatements(buildFlushStatements(flavor, targetTables)))
		}
		totalRows := 0
		for _, table := range selectedFixtures {
			fmt.Fprintf(stdout, "DRY-RUN\tLOAD\t%s\trows=%d\n", table.Name, len(table.Rows))
			totalRows += len(table.Rows)
		}
		fmt.Fprintf(stdout, "Planned load: %d row(s) across %d table(s)\n", totalRows, len(selectedFixtures))
		return nil
	}

	tx, err := sqlDB.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if *truncate {
		if err := execStatementsTx(tx, buildFlushStatements(flavor, targetTables)); err != nil {
			return fmt.Errorf("truncate target tables: %w", err)
		}
	}

	loadedRows := 0
	for _, table := range selectedFixtures {
		count, err := insertFixtureRowsTx(tx, flavor, table.Name, table.Rows)
		if err != nil {
			return err
		}
		loadedRows += count
		fmt.Fprintf(stdout, "LOADED\t%s\trows=%d\n", table.Name, count)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit loaddata: %w", err)
	}

	fmt.Fprintf(stdout, "Loaded %d row(s) across %d table(s)\n", loadedRows, len(selectedFixtures))
	return nil
}

func parseTableCSV(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	return normalizeTableList(parts)
}

func selectTablesForDump(allTables, include, exclude []string) ([]string, error) {
	known := make(map[string]struct{}, len(allTables))
	for _, table := range allTables {
		known[table] = struct{}{}
	}

	selected := make([]string, 0, len(allTables))
	if len(include) == 0 {
		selected = append(selected, allTables...)
	} else {
		for _, table := range include {
			if _, ok := known[table]; !ok {
				return nil, fmt.Errorf("table %q not found in database", table)
			}
			selected = append(selected, table)
		}
	}

	if len(exclude) == 0 {
		return selected, nil
	}

	excluded := make(map[string]struct{}, len(exclude))
	for _, table := range exclude {
		excluded[table] = struct{}{}
	}

	out := make([]string, 0, len(selected))
	for _, table := range selected {
		if _, drop := excluded[table]; drop {
			continue
		}
		out = append(out, table)
	}
	return out, nil
}

func dumpTableRows(sqlDB *sql.DB, flavor dbFlavor, table string) ([]map[string]any, error) {
	if err := validateSQLIdentifier(table); err != nil {
		return nil, err
	}

	query := fmt.Sprintf("SELECT * FROM %s", quoteIdentifier(flavor, table))
	rows, err := sqlDB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query table %s: %w", table, err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("read columns for table %s: %w", table, err)
	}

	rawValues := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range rawValues {
		ptrs[i] = &rawValues[i]
	}

	out := make([]map[string]any, 0, 32)
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scan row for table %s: %w", table, err)
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			row[col] = toFixtureJSONValue(rawValues[i])
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows for table %s: %w", table, err)
	}
	return out, nil
}

func toFixtureJSONValue(v any) any {
	switch vv := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(vv)
	case time.Time:
		return vv.UTC().Format(time.RFC3339Nano)
	default:
		return vv
	}
}

func decodeFixtureDocument(raw []byte) (*fixtureDocument, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	var doc fixtureDocument
	if err := dec.Decode(&doc); err == nil {
		return &doc, nil
	}

	dec = json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var tables []tableFixture
	if err := dec.Decode(&tables); err == nil {
		return &fixtureDocument{Tables: tables}, nil
	}

	return nil, fmt.Errorf("unsupported fixture format; expected object with 'tables' array or array of table fixtures")
}

func selectFixtures(fixtures []tableFixture, include []string) ([]tableFixture, error) {
	clean := make([]tableFixture, 0, len(fixtures))
	for _, fixture := range fixtures {
		name := strings.TrimSpace(fixture.Name)
		if name == "" || shouldSkipSQLTable(name) {
			continue
		}
		if err := validateSQLIdentifier(name); err != nil {
			return nil, err
		}
		fixture.Name = name
		clean = append(clean, fixture)
	}

	if len(include) == 0 {
		return clean, nil
	}

	index := make(map[string]tableFixture, len(clean))
	for _, fixture := range clean {
		index[fixture.Name] = fixture
	}

	out := make([]tableFixture, 0, len(include))
	for _, table := range include {
		fixture, ok := index[table]
		if !ok {
			return nil, fmt.Errorf("fixture table %q not found", table)
		}
		out = append(out, fixture)
	}
	return out, nil
}

func fixtureTableNames(fixtures []tableFixture) []string {
	names := make([]string, 0, len(fixtures))
	for _, fixture := range fixtures {
		names = append(names, fixture.Name)
	}
	return normalizeTableList(names)
}

func execStatementsTx(tx *sql.Tx, statements []string) error {
	for _, raw := range statements {
		stmt := strings.TrimSpace(raw)
		if stmt == "" || strings.HasPrefix(stmt, "--") {
			continue
		}
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func insertFixtureRowsTx(tx *sql.Tx, flavor dbFlavor, table string, rows []map[string]any) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}

	columns := fixtureColumns(rows)
	if len(columns) == 0 {
		return 0, nil
	}

	stmt := buildInsertStatement(flavor, table, columns)
	count := 0

	for _, row := range rows {
		args := make([]any, len(columns))
		for i, col := range columns {
			args[i] = normalizeFixtureValue(row[col])
		}
		if _, err := tx.Exec(stmt, args...); err != nil {
			return count, fmt.Errorf("insert row into %s: %w", table, err)
		}
		count++
	}
	return count, nil
}

func fixtureColumns(rows []map[string]any) []string {
	set := map[string]struct{}{}
	for _, row := range rows {
		for key := range row {
			name := strings.TrimSpace(key)
			if name == "" {
				continue
			}
			set[name] = struct{}{}
		}
	}

	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func buildInsertStatement(flavor dbFlavor, table string, columns []string) string {
	quotedCols := make([]string, len(columns))
	for i, col := range columns {
		quotedCols[i] = quoteIdentifier(flavor, col)
	}
	placeholders := makeInsertPlaceholders(flavor, len(columns))
	return fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		quoteIdentifier(flavor, table),
		strings.Join(quotedCols, ", "),
		strings.Join(placeholders, ", "),
	)
}

func makeInsertPlaceholders(flavor dbFlavor, n int) []string {
	out := make([]string, n)
	for i := 0; i < n; i++ {
		switch flavor {
		case dbFlavorPostgres:
			out[i] = "$" + strconv.Itoa(i+1)
		case dbFlavorMSSQL:
			out[i] = "@p" + strconv.Itoa(i+1)
		case dbFlavorOracle:
			out[i] = ":" + strconv.Itoa(i+1)
		default:
			out[i] = "?"
		}
	}
	return out
}

func normalizeFixtureValue(v any) any {
	switch vv := v.(type) {
	case json.Number:
		if i, err := vv.Int64(); err == nil {
			return i
		}
		if f, err := vv.Float64(); err == nil {
			return f
		}
		return vv.String()
	case map[string]any, []any:
		raw, _ := json.Marshal(vv)
		return string(raw)
	default:
		return vv
	}
}
