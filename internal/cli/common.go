package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jcsvwinston/GoFrame/pkg/app"
	"github.com/jcsvwinston/GoFrame/pkg/db"
	"github.com/jcsvwinston/GoFrame/pkg/observe"
)

var identifierRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func parseOptionalPositiveInt(args []string, fallback int) (int, error) {
	if len(args) == 0 {
		return fallback, nil
	}
	if len(args) > 1 {
		return 0, fmt.Errorf("too many arguments")
	}
	n, err := strconv.Atoi(args[0])
	if err != nil {
		return 0, fmt.Errorf("invalid integer %q", args[0])
	}
	if n <= 0 {
		return 0, fmt.Errorf("value must be greater than 0")
	}
	return n, nil
}

func loadConfig(configPath string) (*app.Config, error) {
	cfg, err := app.LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}

func normalizeDatabaseAlias(alias string) string {
	return strings.ToLower(strings.TrimSpace(alias))
}

func resolveDatabaseAlias(cfg *app.Config, alias string) (string, app.DatabaseConfig, error) {
	if cfg == nil {
		return "", app.DatabaseConfig{}, fmt.Errorf("nil config")
	}

	resolved := normalizeDatabaseAlias(alias)
	if resolved == "" {
		resolved = cfg.DefaultDatabaseAlias()
	}

	dbCfg, ok := cfg.DatabaseByAlias(resolved)
	if !ok {
		return "", app.DatabaseConfig{}, fmt.Errorf("database alias %q is not configured", resolved)
	}
	return resolved, dbCfg, nil
}

func newDatabaseWithAlias(configPath, alias string) (*app.Config, *db.DB, string, func(), error) {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return nil, nil, "", nil, err
	}

	resolvedAlias, dbCfg, err := resolveDatabaseAlias(cfg, alias)
	if err != nil {
		return nil, nil, "", nil, err
	}

	logger := observe.NewLogger(cfg.LogLevel, cfg.LogFormat)
	database, err := db.New(db.Config{
		Engine:              db.EngineSQL,
		DatabaseURL:         dbCfg.URL,
		DatabaseMaxOpen:     dbCfg.MaxOpen,
		DatabaseMaxIdle:     dbCfg.MaxIdle,
		DatabaseMaxLifetime: dbCfg.MaxLifetime,
	}, logger)
	if err != nil {
		return nil, nil, "", nil, fmt.Errorf("open database: %w", err)
	}

	cleanup := func() {
		_ = database.Close()
	}
	return cfg, database, resolvedAlias, cleanup, nil
}

func newDatabase(configPath string) (*app.Config, *db.DB, func(), error) {
	cfg, database, _, cleanup, err := newDatabaseWithAlias(configPath, "")
	if err != nil {
		return nil, nil, nil, err
	}
	return cfg, database, cleanup, nil
}

func newMigratorWithAlias(configPath, migrationsPath, databaseAlias string) (*db.Migrator, string, func(), error) {
	cfg, database, resolvedAlias, cleanup, err := newDatabaseWithAlias(configPath, databaseAlias)
	if err != nil {
		return nil, "", nil, err
	}
	logger := observe.NewLogger(cfg.LogLevel, cfg.LogFormat)
	return db.NewMigrator(database, migrationsPath, logger), resolvedAlias, cleanup, nil
}

func newMigrator(configPath, migrationsPath string) (*db.Migrator, func(), error) {
	migrator, _, cleanup, err := newMigratorWithAlias(configPath, migrationsPath, "")
	if err != nil {
		return nil, nil, err
	}
	return migrator, cleanup, nil
}

func toSnakeCase(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ""
	}

	parts := splitWords(trimmed)
	for i := range parts {
		parts[i] = strings.ToLower(parts[i])
	}
	return strings.Join(parts, "_")
}

func toPascalCase(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ""
	}
	parts := splitWords(trimmed)
	for i := range parts {
		p := strings.ToLower(parts[i])
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

func splitWords(input string) []string {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}

	input = strings.ReplaceAll(input, "-", " ")
	input = strings.ReplaceAll(input, "_", " ")
	fields := strings.Fields(input)
	out := make([]string, 0, len(fields)*2)

	for _, f := range fields {
		if f == "" {
			continue
		}
		start := 0
		runes := []rune(f)
		for i := 1; i < len(runes); i++ {
			if unicode.IsLower(runes[i-1]) && unicode.IsUpper(runes[i]) {
				out = append(out, string(runes[start:i]))
				start = i
			}
		}
		out = append(out, string(runes[start:]))
	}
	return out
}

func ensureDir(path string) error {
	if path == "" {
		return fmt.Errorf("directory path is required")
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("create directory %s: %w", path, err)
	}
	return nil
}

func detectModulePath(root string) (string, bool, error) {
	if strings.TrimSpace(root) == "" {
		return "", false, fmt.Errorf("project root is required")
	}

	goModPath := filepath.Join(root, "go.mod")
	data, err := os.ReadFile(goModPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read go.mod: %w", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "module ") {
			continue
		}
		modulePath := strings.TrimSpace(strings.TrimPrefix(line, "module "))
		if modulePath == "" {
			return "", false, fmt.Errorf("go.mod has an empty module declaration")
		}
		return modulePath, true, nil
	}
	if err := scanner.Err(); err != nil {
		return "", false, fmt.Errorf("scan go.mod: %w", err)
	}
	return "", false, fmt.Errorf("go.mod does not contain a module declaration")
}

func writeFileIfNotExists(path, body string, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("file already exists: %s (use --force to overwrite)", path)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create parent directory for %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		return fmt.Errorf("write file %s: %w", path, err)
	}
	return nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func quoteSQLString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func defaultDatabaseURL(cfg *app.Config) string {
	return databaseURLByAlias(cfg, "")
}

func databaseURLByAlias(cfg *app.Config, alias string) string {
	if cfg == nil {
		return ""
	}
	resolved := normalizeDatabaseAlias(alias)
	if resolved == "" {
		resolved = cfg.DefaultDatabaseAlias()
	}
	dbCfg, ok := cfg.DatabaseByAlias(resolved)
	if !ok {
		return ""
	}
	return dbCfg.URL
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func newSilentLogger() *slog.Logger {
	return observe.NewLogger("error", "text")
}

func validateSQLIdentifier(id string) error {
	if !identifierRegex.MatchString(id) {
		return fmt.Errorf("invalid SQL identifier %q", id)
	}
	return nil
}

func isTerminalReader(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func requireDangerousApproval(cfg *app.Config, stdin io.Reader, stdout io.Writer, force, yes bool, action string) error {
	if force || yes {
		return nil
	}
	if cfg == nil || !cfg.IsProd() {
		return nil
	}

	if !isTerminalReader(stdin) {
		return fmt.Errorf("%s requires --force or --yes in production", action)
	}

	fmt.Fprintf(stdout, "Production safeguard: %s can modify live data. Type 'yes' to continue: ", action)
	reader := bufio.NewReader(stdin)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read confirmation: %w", err)
	}
	if strings.TrimSpace(strings.ToLower(line)) != "yes" {
		return fmt.Errorf("aborted by user")
	}
	return nil
}
