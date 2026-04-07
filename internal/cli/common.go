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

func newDatabase(configPath string) (*app.Config, *db.DB, func(), error) {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return nil, nil, nil, err
	}
	defaultDB, ok := cfg.DatabaseByAlias(cfg.DefaultDatabaseAlias())
	if !ok {
		return nil, nil, nil, fmt.Errorf("database alias %q is not configured", cfg.DefaultDatabaseAlias())
	}

	logger := observe.NewLogger(cfg.LogLevel, cfg.LogFormat)
	database, err := db.New(db.Config{
		Engine:              db.EngineSQL,
		DatabaseURL:         defaultDB.URL,
		DatabaseMaxOpen:     defaultDB.MaxOpen,
		DatabaseMaxIdle:     defaultDB.MaxIdle,
		DatabaseMaxLifetime: defaultDB.MaxLifetime,
	}, logger)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open database: %w", err)
	}

	cleanup := func() {
		_ = database.Close()
	}
	return cfg, database, cleanup, nil
}

func newMigrator(configPath, migrationsPath string) (*db.Migrator, func(), error) {
	cfg, database, cleanup, err := newDatabase(configPath)
	if err != nil {
		return nil, nil, err
	}
	logger := observe.NewLogger(cfg.LogLevel, cfg.LogFormat)
	return db.NewMigrator(database, migrationsPath, logger), cleanup, nil
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
	if cfg == nil {
		return ""
	}
	dbCfg, ok := cfg.DatabaseByAlias(cfg.DefaultDatabaseAlias())
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
