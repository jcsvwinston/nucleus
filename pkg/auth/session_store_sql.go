package auth

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const defaultSessionTableName = "goframe_sessions"
const defaultSessionStoreListLimit = 5000

type sqlSessionStoreFlavor string

const (
	sqlSessionStoreFlavorSQLite   sqlSessionStoreFlavor = "sqlite"
	sqlSessionStoreFlavorPostgres sqlSessionStoreFlavor = "postgres"
	sqlSessionStoreFlavorMySQL    sqlSessionStoreFlavor = "mysql"
)

var sqlIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// SQLSessionStoreConfig configures a SQL-backed SCS store.
type SQLSessionStoreConfig struct {
	DatabaseURL string
	TableName   string
}

// SQLSessionStore persists sessions in a SQL table.
type SQLSessionStore struct {
	db     *sql.DB
	table  string
	flavor sqlSessionStoreFlavor
}

// NewSQLSessionStore builds a SQL-backed session store and ensures table schema.
func NewSQLSessionStore(db *sql.DB, cfg SQLSessionStoreConfig) (*SQLSessionStore, error) {
	if db == nil {
		return nil, fmt.Errorf("new sql session store: nil db")
	}

	table := strings.TrimSpace(cfg.TableName)
	if table == "" {
		table = defaultSessionTableName
	}
	if !sqlIdentifierPattern.MatchString(table) {
		return nil, fmt.Errorf("new sql session store: invalid table name %q", table)
	}

	store := &SQLSessionStore{
		db:     db,
		table:  table,
		flavor: inferSQLSessionStoreFlavor(cfg.DatabaseURL),
	}

	if err := store.ensureSchema(context.Background()); err != nil {
		return nil, fmt.Errorf("new sql session store: %w", err)
	}

	return store, nil
}

// Delete removes the session token from the store.
func (s *SQLSessionStore) Delete(token string) error {
	return s.DeleteCtx(context.Background(), token)
}

// Find retrieves the session payload for token.
func (s *SQLSessionStore) Find(token string) ([]byte, bool, error) {
	return s.FindCtx(context.Background(), token)
}

// Commit stores the session payload for token with absolute expiry.
func (s *SQLSessionStore) Commit(token string, b []byte, expiry time.Time) error {
	return s.CommitCtx(context.Background(), token, b, expiry)
}

// All returns all non-expired sessions.
func (s *SQLSessionStore) All() (map[string][]byte, error) {
	return s.AllCtx(context.Background())
}

// DeleteCtx removes the session token from the store.
func (s *SQLSessionStore) DeleteCtx(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}

	query := fmt.Sprintf(`DELETE FROM %s WHERE token = %s`, s.quotedTable(), s.placeholder(1))
	if _, err := s.db.ExecContext(ctx, query, token); err != nil {
		return fmt.Errorf("sql session delete: %w", err)
	}
	return nil
}

// FindCtx retrieves the session payload for token.
func (s *SQLSessionStore) FindCtx(ctx context.Context, token string) ([]byte, bool, error) {
	if token == "" {
		return nil, false, nil
	}

	query := fmt.Sprintf(`SELECT data, expires_at FROM %s WHERE token = %s`, s.quotedTable(), s.placeholder(1))

	var payload []byte
	var expiresRaw any
	err := s.db.QueryRowContext(ctx, query, token).Scan(&payload, &expiresRaw)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("sql session find: %w", err)
	}

	expiresAt, err := parseSessionExpiryValue(expiresRaw)
	if err != nil {
		return nil, false, fmt.Errorf("sql session find: parse expiry: %w", err)
	}
	if !expiresAt.After(time.Now().UTC()) {
		// Expired rows are treated as missing and cleaned up opportunistically.
		_ = s.DeleteCtx(ctx, token)
		return nil, false, nil
	}

	result := make([]byte, len(payload))
	copy(result, payload)
	return result, true, nil
}

// CommitCtx stores the session payload for token with absolute expiry.
func (s *SQLSessionStore) CommitCtx(ctx context.Context, token string, b []byte, expiry time.Time) error {
	if token == "" {
		return fmt.Errorf("sql session commit: empty token")
	}
	if expiry.IsZero() {
		return fmt.Errorf("sql session commit: zero expiry")
	}

	query, args := s.commitStatement(token, b, expiry.UTC())
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("sql session commit: %w", err)
	}
	return nil
}

// AllCtx returns all non-expired sessions.
func (s *SQLSessionStore) AllCtx(ctx context.Context) (map[string][]byte, error) {
	query := fmt.Sprintf(`SELECT token, data, expires_at FROM %s LIMIT %d`, s.quotedTable(), defaultSessionStoreListLimit)
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("sql session all: %w", err)
	}
	defer rows.Close()

	now := time.Now().UTC()
	result := make(map[string][]byte)
	for rows.Next() {
		var token string
		var payload []byte
		var expiresRaw any
		if err := rows.Scan(&token, &payload, &expiresRaw); err != nil {
			return nil, fmt.Errorf("sql session all scan: %w", err)
		}

		expiresAt, err := parseSessionExpiryValue(expiresRaw)
		if err != nil {
			continue
		}
		if !expiresAt.After(now) {
			continue
		}

		copyPayload := make([]byte, len(payload))
		copy(copyPayload, payload)
		result[token] = copyPayload
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sql session all rows: %w", err)
	}

	return result, nil
}

func (s *SQLSessionStore) ensureSchema(ctx context.Context) error {
	var createStmt string
	switch s.flavor {
	case sqlSessionStoreFlavorPostgres:
		createStmt = fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s (token TEXT PRIMARY KEY, data BYTEA NOT NULL, expires_at TIMESTAMPTZ NOT NULL)`,
			s.quotedTable(),
		)
	case sqlSessionStoreFlavorMySQL:
		createStmt = fmt.Sprintf(
			"CREATE TABLE IF NOT EXISTS %s (token VARCHAR(191) PRIMARY KEY, data LONGBLOB NOT NULL, expires_at DATETIME(6) NOT NULL)",
			s.quotedTable(),
		)
	default:
		createStmt = fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s (token TEXT PRIMARY KEY, data BLOB NOT NULL, expires_at DATETIME NOT NULL)`,
			s.quotedTable(),
		)
	}

	if _, err := s.db.ExecContext(ctx, createStmt); err != nil {
		return fmt.Errorf("create table: %w", err)
	}

	if s.flavor == sqlSessionStoreFlavorMySQL {
		return nil
	}

	indexStmt := fmt.Sprintf(
		`CREATE INDEX IF NOT EXISTS %s ON %s (expires_at)`,
		s.quotedIdentifier("idx_"+s.table+"_expires_at"),
		s.quotedTable(),
	)
	if _, err := s.db.ExecContext(ctx, indexStmt); err != nil {
		return fmt.Errorf("create index: %w", err)
	}

	return nil
}

func (s *SQLSessionStore) commitStatement(token string, payload []byte, expiry time.Time) (string, []any) {
	switch s.flavor {
	case sqlSessionStoreFlavorPostgres:
		return fmt.Sprintf(
				`INSERT INTO %s (token, data, expires_at) VALUES ($1, $2, $3) `+
					`ON CONFLICT (token) DO UPDATE SET data = EXCLUDED.data, expires_at = EXCLUDED.expires_at`,
				s.quotedTable(),
			),
			[]any{token, payload, expiry}
	case sqlSessionStoreFlavorMySQL:
		return fmt.Sprintf(
				"INSERT INTO %s (token, data, expires_at) VALUES (?, ?, ?) "+
					"ON DUPLICATE KEY UPDATE data = VALUES(data), expires_at = VALUES(expires_at)",
				s.quotedTable(),
			),
			[]any{token, payload, expiry}
	default:
		return fmt.Sprintf(
				`INSERT INTO %s (token, data, expires_at) VALUES (?, ?, ?) `+
					`ON CONFLICT(token) DO UPDATE SET data = excluded.data, expires_at = excluded.expires_at`,
				s.quotedTable(),
			),
			[]any{token, payload, expiry}
	}
}

func (s *SQLSessionStore) placeholder(i int) string {
	if s.flavor == sqlSessionStoreFlavorPostgres {
		return fmt.Sprintf("$%d", i)
	}
	return "?"
}

func (s *SQLSessionStore) quotedTable() string {
	return s.quotedIdentifier(s.table)
}

func (s *SQLSessionStore) quotedIdentifier(name string) string {
	switch s.flavor {
	case sqlSessionStoreFlavorMySQL:
		return "`" + name + "`"
	default:
		return `"` + name + `"`
	}
}

func inferSQLSessionStoreFlavor(databaseURL string) sqlSessionStoreFlavor {
	raw := strings.ToLower(strings.TrimSpace(databaseURL))
	switch {
	case strings.HasPrefix(raw, "postgres://"), strings.HasPrefix(raw, "postgresql://"):
		return sqlSessionStoreFlavorPostgres
	case strings.HasPrefix(raw, "mysql://"):
		return sqlSessionStoreFlavorMySQL
	default:
		return sqlSessionStoreFlavorSQLite
	}
}

func parseSessionExpiryValue(raw any) (time.Time, error) {
	switch v := raw.(type) {
	case time.Time:
		return v.UTC(), nil
	case string:
		return parseSessionExpiryString(v)
	case []byte:
		return parseSessionExpiryString(string(v))
	case int64:
		return time.Unix(v, 0).UTC(), nil
	case float64:
		return time.Unix(int64(v), 0).UTC(), nil
	default:
		return time.Time{}, fmt.Errorf("unsupported expiry type %T", raw)
	}
}

func parseSessionExpiryString(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty expiry value")
	}

	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}

	for _, layout := range layouts {
		if ts, err := time.Parse(layout, value); err == nil {
			return ts.UTC(), nil
		}
	}

	return time.Time{}, fmt.Errorf("invalid expiry time %q", raw)
}
