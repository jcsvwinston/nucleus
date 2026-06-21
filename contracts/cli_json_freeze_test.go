package contracts

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/internal/cli"
	_ "modernc.org/sqlite"
)

func TestContractFreeze_CLIJSONStatusKeys_NoRemovals(t *testing.T) {
	currentLines := stableCLIJSONStatusKeyLines(t)
	if os.Getenv("NUCLEUS_UPDATE_CONTRACT_BASELINE") == "1" {
		writeBaselineLines(t, currentLines, "baseline", "cli_json_status_keys.txt")
	}

	baseline := readBaselineLines(t, "baseline", "cli_json_status_keys.txt")
	current := toSet(currentLines)

	missing := make([]string, 0)
	for _, item := range baseline {
		if _, ok := current[item]; !ok {
			missing = append(missing, item)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("stable CLI JSON contract regression: missing key(s): %s", strings.Join(missing, ", "))
	}
}

func stableCLIJSONStatusKeyLines(t *testing.T) []string {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeContractCLIConfig(t, dir, dbPath)

	dbConn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer dbConn.Close()

	lines := make([]string, 0, 128)

	// The nucleus_admin_users table is owned by the orbit module
	// (ADR-019); `createuser`/`changepassword` no longer auto-create it
	// and require orbit to have initialised the schema first. Simulate
	// that here by pre-creating the table before exercising the commands.
	if _, err := dbConn.Exec(`CREATE TABLE nucleus_admin_users (
		id VARCHAR(64) PRIMARY KEY,
		username VARCHAR(191) NOT NULL UNIQUE,
		email VARCHAR(191) NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		is_superuser INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("create admin users table failed: %v", err)
	}

	createPayload := runContractCLIJSONCommand(t,
		"--output", "json",
		"createuser",
		"--config", cfgPath,
		"--no-input",
		"--username", "admin_json",
		"--email", "admin_json@example.com",
		"--password", "supersecret123",
	)
	lines = append(lines, collectCLIJSONPayloadKeyLines(t, "createuser", createPayload)...)

	changePayload := runContractCLIJSONCommand(t,
		"--output", "json",
		"changepassword",
		"--config", cfgPath,
		"--no-input",
		"--password", "newsecret456",
		"admin_json",
	)
	lines = append(lines, collectCLIJSONPayloadKeyLines(t, "changepassword", changePayload)...)

	cachePayload := runContractCLIJSONCommand(t,
		"--output", "json",
		"createcachetable",
		"--config", cfgPath,
		"--dry-run",
	)
	lines = append(lines, collectCLIJSONPayloadKeyLines(t, "createcachetable", cachePayload)...)

	if _, err := dbConn.Exec(`CREATE TABLE nucleus_sessions (id TEXT PRIMARY KEY, payload TEXT NOT NULL, expires_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("create sessions table failed: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO nucleus_sessions (id, payload, expires_at) VALUES ('old', '{}', datetime('now','-1 day'))`); err != nil {
		t.Fatalf("seed expired session failed: %v", err)
	}

	clearPayload := runContractCLIJSONCommand(t,
		"--output", "json",
		"clearsessions",
		"--config", cfgPath,
		"--dry-run",
	)
	lines = append(lines, collectCLIJSONPayloadKeyLines(t, "clearsessions", clearPayload)...)

	if _, err := dbConn.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatalf("create users table failed: %v", err)
	}
	if _, err := dbConn.Exec(`CREATE TABLE nucleus_content_types (id INTEGER PRIMARY KEY, model TEXT NOT NULL)`); err != nil {
		t.Fatalf("create content types table failed: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO nucleus_content_types(model) VALUES ('users'), ('ghost_model')`); err != nil {
		t.Fatalf("seed content types failed: %v", err)
	}

	contentPayload := runContractCLIJSONCommand(t,
		"--output", "json",
		"remove_stale_contenttypes",
		"--config", cfgPath,
		"--dry-run",
	)
	lines = append(lines, collectCLIJSONPayloadKeyLines(t, "remove_stale_contenttypes", contentPayload)...)

	sort.Strings(lines)
	return dedupeSorted(lines)
}

func runContractCLIJSONCommand(t *testing.T, args ...string) map[string]interface{} {
	t.Helper()
	var out bytes.Buffer
	var errOut bytes.Buffer
	code := cli.Run(args, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("cli run failed: code=%d args=%v stderr=%s", code, args, errOut.String())
	}
	raw := bytes.TrimSpace(out.Bytes())
	if len(raw) == 0 {
		t.Fatalf("empty output for args=%v", args)
	}
	payload := make(map[string]interface{})
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode json output for args=%v: %v raw=%s", args, err, string(raw))
	}
	return payload
}

func collectCLIJSONPayloadKeyLines(t *testing.T, command string, payload map[string]interface{}) []string {
	t.Helper()
	gotCommand, _ := payload["command"].(string)
	if gotCommand != command {
		t.Fatalf("unexpected command value for %s: got=%q payload=%v", command, gotCommand, payload)
	}
	status, _ := payload["status"].(string)
	if strings.TrimSpace(status) == "" {
		t.Fatalf("empty status for %s payload=%v", command, payload)
	}

	lines := make([]string, 0, 16)
	lines = append(lines, fmt.Sprintf("%s status.%s", command, status))
	for key := range payload {
		lines = append(lines, fmt.Sprintf("%s top.%s", command, key))
	}
	if data, ok := payload["data"].(map[string]interface{}); ok {
		for key := range data {
			lines = append(lines, fmt.Sprintf("%s data.%s", command, key))
		}
	}
	sort.Strings(lines)
	return dedupeSorted(lines)
}

func writeContractCLIConfig(t *testing.T, dir, dbPath string) string {
	t.Helper()
	cfgPath := filepath.Join(dir, "nucleus.yml")
	cfg := fmt.Sprintf(
		"database_default: default\ndatabases:\n  default:\n    url: sqlite://%s\nlog_level: error\nlog_format: text\n",
		dbPath,
	)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config %s failed: %v", cfgPath, err)
	}
	return cfgPath
}
