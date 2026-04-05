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

func writeSQLMatrixConfig(t *testing.T, dir, rawURL string) string {
	t.Helper()
	path := filepath.Join(dir, "goframe.yaml")
	body := fmt.Sprintf(
		"database_engine: bun\n"+
			"database_url: %s\n"+
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
