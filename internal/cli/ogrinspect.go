package cli

import (
	"bytes"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func runOGRInspect(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("ogrinspect", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to goframe config file")
	databaseAlias := fs.String("database", "", "Database alias to use (defaults to database_default)")
	tablesRaw := fs.String("tables", "", "Comma-separated table list to inspect (default: all user tables)")
	excludeRaw := fs.String("exclude", "", "Comma-separated table list to exclude")
	packageName := fs.String("package", "models", "Go package name for generated structs")
	outputPath := fs.String("output", "-", "Output Go file path ('-' for stdout)")
	includeAll := fs.Bool("all", false, "Include non-geospatial tables (default: geospatial tables only)")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	positionalTables := normalizeTableList(fs.Args())
	if *tablesRaw != "" && len(positionalTables) > 0 {
		return fmt.Errorf("ogrinspect accepts either positional table names or --tables, not both")
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
		return fmt.Errorf("no tables selected for ogrinspect")
	}

	if !*includeAll {
		selectedTables, err = selectGeospatialTables(sqlDB, flavor, selectedTables)
		if err != nil {
			return err
		}
		if len(selectedTables) == 0 {
			return fmt.Errorf("no geospatial tables selected for ogrinspect (use --all to inspect all selected tables)")
		}
	}

	models, err := buildInspectModels(sqlDB, flavor, selectedTables)
	if err != nil {
		return err
	}
	if len(models) == 0 {
		return fmt.Errorf("ogrinspect found no inspectable columns in selected tables")
	}

	source, err := renderInspectModels(pkg, models)
	if err != nil {
		return err
	}
	source = bytes.Replace(source, []byte("goframe inspectdb"), []byte("goframe ogrinspect"), 1)

	target := strings.TrimSpace(*outputPath)
	switch target {
	case "", "-":
		_, _ = stdout.Write(source)
	default:
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
		if err := os.WriteFile(target, source, 0644); err != nil {
			return fmt.Errorf("write ogrinspect output: %w", err)
		}
		fmt.Fprintf(stdout, "ogrinspect output written: %s (%d table(s))\n", target, len(models))
	}
	return nil
}

func selectGeospatialTables(sqlDB *sql.DB, flavor dbFlavor, tables []string) ([]string, error) {
	out := make([]string, 0, len(tables))
	for _, table := range tables {
		columns, err := inspectTableColumns(sqlDB, flavor, table)
		if err != nil {
			return nil, err
		}
		if hasGeometryColumn(columns) {
			out = append(out, table)
		}
	}
	return out, nil
}

func hasGeometryColumn(columns []introspectedColumn) bool {
	for _, col := range columns {
		if isGeometryDBType(col.DBType) {
			return true
		}
	}
	return false
}

func isGeometryDBType(dbType string) bool {
	base := strings.ToLower(strings.TrimSpace(dbType))
	if base == "" {
		return false
	}
	base = strings.ReplaceAll(base, " ", "")

	if strings.HasPrefix(base, "geometry") || strings.HasPrefix(base, "geography") {
		return true
	}

	switch {
	case base == "point",
		base == "line",
		base == "linearring",
		strings.Contains(base, "linestring"),
		strings.Contains(base, "polygon"),
		strings.Contains(base, "multipoint"),
		strings.Contains(base, "multilinestring"),
		strings.Contains(base, "multipolygon"),
		strings.Contains(base, "geometrycollection"),
		strings.Contains(base, "circularstring"),
		strings.Contains(base, "compoundcurve"),
		strings.Contains(base, "curvepolygon"),
		strings.Contains(base, "geom"):
		return true
	default:
		return false
	}
}
