package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSQLMatrix_CriticalCommands(t *testing.T) {
	rawURL := strings.TrimSpace(os.Getenv("GOFRAME_SQL_MATRIX_URL"))
	if rawURL == "" {
		t.Skip("GOFRAME_SQL_MATRIX_URL is not set; skipping SQL matrix CLI integration test")
	}

	flavor := detectDBFlavor(rawURL)
	if flavor != dbFlavorPostgres && flavor != dbFlavorMySQL {
		t.Skipf("GOFRAME_SQL_MATRIX_URL=%q is not a PostgreSQL/MySQL profile", rawURL)
	}

	dir := t.TempDir()
	cfgPath := writeSQLMatrixConfig(t, dir, rawURL)
	migrationsDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(migrationsDir, 0755); err != nil {
		t.Fatalf("mkdir migrations failed: %v", err)
	}

	suffix := time.Now().UTC().Format("20060102150405.000000000")
	suffix = strings.NewReplacer(".", "", "-", "").Replace(suffix)
	tableName := "ci_matrix_" + suffix
	migrationID := suffix + "_create_" + tableName

	upSQL := fmt.Sprintf("CREATE TABLE %s (id INTEGER PRIMARY KEY, name VARCHAR(255) NOT NULL);", quoteIdentifier(flavor, tableName))
	downSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s;", quoteIdentifier(flavor, tableName))
	writeSQLMatrixFile(t, filepath.Join(migrationsDir, migrationID+".up.sql"), upSQL)
	writeSQLMatrixFile(t, filepath.Join(migrationsDir, migrationID+".down.sql"), downSQL)

	var out bytes.Buffer
	var errOut bytes.Buffer

	if err := runMigrate([]string{"--config", cfgPath, "--migrations", migrationsDir, "up"}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("migrate up failed: err=%v stderr=%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "Migrations applied") {
		t.Fatalf("unexpected migrate up output: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if err := runHealth([]string{"--config", cfgPath, "--json"}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("health failed: err=%v stderr=%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "\"status\": \"ok\"") {
		t.Fatalf("unexpected health output: %s", out.String())
	}

	fixturePath := filepath.Join(dir, "fixtures.json")
	fixture := fmt.Sprintf(`{"tables":[{"name":"%s","rows":[{"id":1,"name":"matrix-user"}]}]}`, tableName)
	writeSQLMatrixFile(t, fixturePath, fixture)

	out.Reset()
	errOut.Reset()
	if err := runLoadData([]string{"--config", cfgPath, fixturePath}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("loaddata failed: err=%v stderr=%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "Loaded 1 row(s)") {
		t.Fatalf("unexpected loaddata output: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if err := runDumpData([]string{"--config", cfgPath, "--tables", tableName, "--pretty=false"}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("dumpdata failed: err=%v stderr=%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), `"name":"matrix-user"`) {
		t.Fatalf("expected dumped fixture to include inserted row, got: %s", out.String())
	}

	query := fmt.Sprintf("SELECT count(*) AS total FROM %s;", quoteIdentifier(flavor, tableName))
	out.Reset()
	errOut.Reset()
	if err := runShell([]string{"--config", cfgPath, "-c", query}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("shell failed: err=%v stderr=%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "total") || !strings.Contains(out.String(), "1") {
		t.Fatalf("unexpected shell output: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if err := runMigrate([]string{"--config", cfgPath, "--migrations", migrationsDir, "down"}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("migrate down failed: err=%v stderr=%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "Rolled back 1 migration(s)") {
		t.Fatalf("unexpected migrate down output: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if err := runMigrate([]string{"--config", cfgPath, "--migrations", migrationsDir, "status"}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("migrate status failed: err=%v stderr=%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "pending") {
		t.Fatalf("expected migration to be pending after rollback, got: %s", out.String())
	}
}

func TestSQLMatrix_ExploratoryCriticalCommands(t *testing.T) {
	rawURL := strings.TrimSpace(os.Getenv("GOFRAME_SQL_EXPLORATORY_URL"))
	if rawURL == "" {
		t.Skip("GOFRAME_SQL_EXPLORATORY_URL is not set; skipping exploratory SQL matrix CLI integration test")
	}

	flavor := detectDBFlavor(rawURL)
	if flavor != dbFlavorMSSQL && flavor != dbFlavorOracle {
		t.Skipf("GOFRAME_SQL_EXPLORATORY_URL=%q is not an MSSQL/Oracle profile", rawURL)
	}

	dir := t.TempDir()
	cfgPath := writeSQLMatrixConfig(t, dir, rawURL)

	suffix := time.Now().UTC().Format("20060102150405.000000000")
	suffix = strings.NewReplacer(".", "", "-", "").Replace(suffix)
	tableName := "ci_cache_" + suffix

	var out bytes.Buffer
	var errOut bytes.Buffer

	if err := runHealth([]string{"--config", cfgPath, "--json"}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("health failed: err=%v stderr=%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "\"status\": \"ok\"") {
		t.Fatalf("unexpected health output: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if err := runCreateCacheTable([]string{"--config", cfgPath, "--table", tableName}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("createcachetable failed: err=%v stderr=%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "Cache table ready") {
		t.Fatalf("unexpected createcachetable output: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if err := runCreateCacheTable([]string{"--config", cfgPath, "--table", tableName}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("createcachetable idempotency failed: err=%v stderr=%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "Cache table ready") {
		t.Fatalf("unexpected createcachetable idempotency output: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if err := runInspectDB([]string{"--config", cfgPath, "--tables", tableName, "--package", "models", "--output", "-"}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("inspectdb failed: err=%v stderr=%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), `return "`+tableName+`"`) {
		t.Fatalf("inspectdb output does not include expected table name %q, got: %s", tableName, out.String())
	}

	out.Reset()
	errOut.Reset()
	if err := runSQLFlush([]string{"--config", cfgPath}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("sqlflush failed: err=%v stderr=%s", err, errOut.String())
	}
	assertExploratoryFlushOutput(t, flavor, tableName, out.String())

	out.Reset()
	errOut.Reset()
	if err := runFlush([]string{"--config", cfgPath, "--dry-run"}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("flush --dry-run failed: err=%v stderr=%s", err, errOut.String())
	}
	assertExploratoryFlushOutput(t, flavor, tableName, out.String())

	out.Reset()
	errOut.Reset()
	if err := runSQLSequenceReset([]string{"--config", cfgPath, tableName}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("sqlsequencereset failed: err=%v stderr=%s", err, errOut.String())
	}
	assertExploratorySequenceResetOutput(t, flavor, tableName, out.String())

	if flavor == dbFlavorOracle {
		oracleSeqTable := "ci_seq_" + suffix[len(suffix)-8:]
		oracleSeqName := strings.ToUpper(oracleSeqTable) + "_SEQ"

		if err := createOracleSequenceFixture(cfgPath, oracleSeqTable, oracleSeqName); err != nil {
			t.Fatalf("create oracle sequence fixture failed: %v", err)
		}

		out.Reset()
		errOut.Reset()
		if err := runSQLSequenceReset([]string{"--config", cfgPath, oracleSeqTable}, strings.NewReader(""), &out, &errOut); err != nil {
			t.Fatalf("oracle sqlsequencereset with fixture failed: err=%v stderr=%s", err, errOut.String())
		}
		if !strings.Contains(out.String(), "ALTER SEQUENCE "+quoteIdentifier(flavor, oracleSeqName)+" RESTART START WITH 8;") {
			t.Fatalf("expected oracle sequence reset output to include restart statement for %q, got: %s", oracleSeqName, out.String())
		}
	}

	query := exploratoryTableCountQuery(flavor, tableName)
	out.Reset()
	errOut.Reset()
	if err := runShell([]string{"--config", cfgPath, "-c", query}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("shell failed: err=%v stderr=%s", err, errOut.String())
	}
	lowerShellOut := strings.ToLower(out.String())
	if !strings.Contains(lowerShellOut, "total") || !strings.Contains(out.String(), "1") {
		t.Fatalf("unexpected shell output: %s", out.String())
	}
}

func assertExploratoryFlushOutput(t *testing.T, flavor dbFlavor, tableName, output string) {
	t.Helper()

	switch flavor {
	case dbFlavorMSSQL:
		if !strings.Contains(output, "DBCC CHECKIDENT") {
			t.Fatalf("expected mssql flush output to include DBCC CHECKIDENT, got: %s", output)
		}
		if !strings.Contains(output, quoteIdentifier(flavor, tableName)) {
			t.Fatalf("expected mssql flush output to include table %q, got: %s", tableName, output)
		}
	case dbFlavorOracle:
		if !strings.Contains(output, "TRUNCATE TABLE") {
			t.Fatalf("expected oracle flush output to include TRUNCATE TABLE, got: %s", output)
		}
		if !strings.Contains(output, quoteIdentifier(flavor, tableName)) {
			t.Fatalf("expected oracle flush output to include table %q, got: %s", tableName, output)
		}
	default:
		t.Fatalf("unexpected flavor for exploratory flush assertions: %s", flavor)
	}
}

func assertExploratorySequenceResetOutput(t *testing.T, flavor dbFlavor, tableName, output string) {
	t.Helper()

	switch flavor {
	case dbFlavorMSSQL:
		if !strings.Contains(output, "DBCC CHECKIDENT") {
			t.Fatalf("expected mssql sequence reset output to include DBCC CHECKIDENT, got: %s", output)
		}
		if !strings.Contains(output, quoteSQLString(tableName)) {
			t.Fatalf("expected mssql sequence reset output to include table %q, got: %s", tableName, output)
		}
	case dbFlavorOracle:
		if !strings.Contains(output, "no known sequence found for table "+tableName) {
			t.Fatalf("expected oracle sequence reset output to include unknown-sequence guidance, got: %s", output)
		}
	default:
		t.Fatalf("unexpected flavor for exploratory sequence reset assertions: %s", flavor)
	}
}

func createOracleSequenceFixture(cfgPath, tableName, seqName string) error {
	var out bytes.Buffer
	var errOut bytes.Buffer

	createTableSQL := fmt.Sprintf(
		"CREATE TABLE %s (%s NUMBER PRIMARY KEY, %s VARCHAR2(100))",
		quoteIdentifier(dbFlavorOracle, tableName),
		quoteIdentifier(dbFlavorOracle, "id"),
		quoteIdentifier(dbFlavorOracle, "name"),
	)
	if err := runShell([]string{"--config", cfgPath, "-c", createTableSQL}, strings.NewReader(""), &out, &errOut); err != nil {
		return fmt.Errorf("create table: %w (stderr=%s)", err, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	createSeqSQL := fmt.Sprintf(
		"CREATE SEQUENCE %s START WITH 1 INCREMENT BY 1",
		quoteIdentifier(dbFlavorOracle, seqName),
	)
	if err := runShell([]string{"--config", cfgPath, "-c", createSeqSQL}, strings.NewReader(""), &out, &errOut); err != nil {
		return fmt.Errorf("create sequence: %w (stderr=%s)", err, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	insertSQL := fmt.Sprintf(
		"INSERT INTO %s (%s, %s) VALUES (7, %s)",
		quoteIdentifier(dbFlavorOracle, tableName),
		quoteIdentifier(dbFlavorOracle, "id"),
		quoteIdentifier(dbFlavorOracle, "name"),
		quoteSQLString("oracle-seq"),
	)
	if err := runShell([]string{"--config", cfgPath, "-c", insertSQL}, strings.NewReader(""), &out, &errOut); err != nil {
		return fmt.Errorf("insert fixture row: %w (stderr=%s)", err, errOut.String())
	}

	return nil
}

func exploratoryTableCountQuery(flavor dbFlavor, table string) string {
	switch flavor {
	case dbFlavorMSSQL:
		return fmt.Sprintf(
			"SELECT COUNT(*) AS total FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA = 'dbo' AND TABLE_NAME = %s",
			quoteSQLString(table),
		)
	case dbFlavorOracle:
		return fmt.Sprintf(
			"SELECT COUNT(*) AS total FROM user_tables WHERE table_name = UPPER(%s)",
			quoteSQLString(table),
		)
	default:
		return "SELECT 0 AS total"
	}
}

func writeSQLMatrixConfig(t *testing.T, dir, rawURL string) string {
	t.Helper()
	path := filepath.Join(dir, "goframe.yaml")
	body := fmt.Sprintf(
		"database_default: default\n"+
			"databases:\n"+
			"  default:\n"+
			"    url: %q\n"+
			"log_level: error\n"+
			"log_format: text\n",
		rawURL,
	)
	writeSQLMatrixFile(t, path, body)
	return path
}

func writeSQLMatrixFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write file %s failed: %v", path, err)
	}
}
