package cache

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// SQLOptions configures NewSQL.
type SQLOptions struct {
	// Table is the cache table name. Empty means DefaultTableName — the
	// table `nucleus createcachetable` creates. The table must already
	// exist; run the command (or ship its DDL as a migration) first.
	Table string
	// System is the SQL system the *sql.DB speaks, using the same names
	// `(*db.DB).System()` returns: "sqlite", "postgresql", "mysql",
	// "mssql", or "oracle". It selects placeholder style, upsert form,
	// and the server-side UTC now() expression.
	System string
}

// SQL is a Cache backed by the table `nucleus createcachetable` creates.
// It shares state across replicas that point at the same database, which
// makes it the SQL-backed option in the deployment guide's shared-state
// table. Expiry is enforced on read (expired rows are invisible to Get)
// and reclaimed by PruneExpired; expired rows that are never pruned only
// cost storage, never correctness.
//
// mssql and oracle support follows the exploratory posture of those CI
// lanes (docs/governance/CI_MATRIX.md): the statements are exercised
// against sqlite/postgresql/mysql in CI, and take a transactional
// delete+insert path on mssql/oracle instead of a native upsert.
type SQL struct {
	db    *sql.DB
	table string

	getStmt    string
	setStmt    string // native upsert; empty when the system needs the tx path
	deleteStmt string
	pruneStmt  string
	txDelete   string // tx path: delete by key
	txInsert   string // tx path: insert row

	// sqliteTime is true when timestamps must be bound as UTC strings in
	// the `datetime('now')` format (the sqlite table stores TEXT).
	sqliteTime bool

	// now is the client-side clock used to compute expiries, injectable in
	// tests. Expiry COMPARISON happens server-side against the database
	// clock, so replicas do not need synchronised process clocks.
	now func() time.Time
}

var identifierRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// sqliteTimeLayout matches sqlite's datetime('now') text format (UTC).
const sqliteTimeLayout = "2006-01-02 15:04:05"

// NewSQL builds a SQL cache over db. The table (SQLOptions.Table, default
// DefaultTableName) must have the schema `nucleus createcachetable`
// creates: cache_key, value, expires_at, created_at, updated_at. NewSQL
// validates its inputs but does not touch the database; a missing table
// surfaces as an error from the first Get/Set.
func NewSQL(db *sql.DB, opts SQLOptions) (*SQL, error) {
	if db == nil {
		return nil, errors.New("cache.NewSQL: db cannot be nil")
	}
	table := strings.TrimSpace(opts.Table)
	if table == "" {
		table = DefaultTableName
	}
	if !identifierRe.MatchString(table) {
		return nil, fmt.Errorf("cache.NewSQL: invalid table name %q", table)
	}

	system := strings.ToLower(strings.TrimSpace(opts.System))
	s := &SQL{db: db, table: table, now: time.Now}

	q := func(name string) string { return quoteIdentifier(system, name) }
	qt := q(table)
	key, value, expires := q("cache_key"), q("value"), q("expires_at")
	created, updated := q("created_at"), q("updated_at")
	cols := fmt.Sprintf("(%s, %s, %s, %s, %s)", key, value, expires, created, updated)

	switch system {
	case "sqlite":
		s.sqliteTime = true
		s.getStmt = fmt.Sprintf("SELECT %s FROM %s WHERE %s = ? AND %s > datetime('now')", value, qt, key, expires)
		s.setStmt = fmt.Sprintf(
			"INSERT INTO %s %s VALUES (?, ?, ?, ?, ?) ON CONFLICT(%s) DO UPDATE SET %s = excluded.%s, %s = excluded.%s, %s = excluded.%s",
			qt, cols, key, value, value, expires, expires, updated, updated)
		s.deleteStmt = fmt.Sprintf("DELETE FROM %s WHERE %s = ?", qt, key)
		s.pruneStmt = fmt.Sprintf("DELETE FROM %s WHERE %s <= datetime('now')", qt, expires)

	case "postgresql":
		s.getStmt = fmt.Sprintf("SELECT %s FROM %s WHERE %s = $1 AND %s > NOW()", value, qt, key, expires)
		s.setStmt = fmt.Sprintf(
			"INSERT INTO %s %s VALUES ($1, $2, $3, $4, $5) ON CONFLICT (%s) DO UPDATE SET %s = excluded.%s, %s = excluded.%s, %s = excluded.%s",
			qt, cols, key, value, value, expires, expires, updated, updated)
		s.deleteStmt = fmt.Sprintf("DELETE FROM %s WHERE %s = $1", qt, key)
		s.pruneStmt = fmt.Sprintf("DELETE FROM %s WHERE %s <= NOW()", qt, expires)

	case "mysql":
		// UTC_TIMESTAMP(), not NOW(): the DATETIME columns carry no zone,
		// so both the stored expiry (bound in UTC) and the comparison
		// clock must be UTC regardless of the session time_zone.
		s.getStmt = fmt.Sprintf("SELECT %s FROM %s WHERE %s = ? AND %s > UTC_TIMESTAMP()", value, qt, key, expires)
		s.setStmt = fmt.Sprintf(
			"INSERT INTO %s %s VALUES (?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE %s = VALUES(%s), %s = VALUES(%s), %s = VALUES(%s)",
			qt, cols, value, value, expires, expires, updated, updated)
		s.deleteStmt = fmt.Sprintf("DELETE FROM %s WHERE %s = ?", qt, key)
		s.pruneStmt = fmt.Sprintf("DELETE FROM %s WHERE %s <= UTC_TIMESTAMP()", qt, expires)

	case "mssql":
		s.getStmt = fmt.Sprintf("SELECT %s FROM %s WHERE %s = @p1 AND %s > SYSUTCDATETIME()", value, qt, key, expires)
		s.txDelete = fmt.Sprintf("DELETE FROM %s WHERE %s = @p1", qt, key)
		s.txInsert = fmt.Sprintf("INSERT INTO %s %s VALUES (@p1, @p2, @p3, @p4, @p5)", qt, cols)
		s.deleteStmt = s.txDelete
		s.pruneStmt = fmt.Sprintf("DELETE FROM %s WHERE %s <= SYSUTCDATETIME()", qt, expires)

	case "oracle":
		s.getStmt = fmt.Sprintf("SELECT %s FROM %s WHERE %s = :1 AND %s > SYS_EXTRACT_UTC(SYSTIMESTAMP)", value, qt, key, expires)
		s.txDelete = fmt.Sprintf("DELETE FROM %s WHERE %s = :1", qt, key)
		s.txInsert = fmt.Sprintf("INSERT INTO %s %s VALUES (:1, :2, :3, :4, :5)", qt, cols)
		s.deleteStmt = s.txDelete
		s.pruneStmt = fmt.Sprintf("DELETE FROM %s WHERE %s <= SYS_EXTRACT_UTC(SYSTIMESTAMP)", qt, expires)

	default:
		return nil, fmt.Errorf("cache.NewSQL: unsupported SQL system %q (expected sqlite, postgresql, mysql, mssql, or oracle — the values (*db.DB).System() returns)", opts.System)
	}
	return s, nil
}

// quoteIdentifier quotes an already-validated identifier for the system.
// Mirrors the CLI's quoting so the runtime addresses exactly the table
// `nucleus createcachetable` created (including oracle's upper-casing).
func quoteIdentifier(system, name string) string {
	switch system {
	case "mysql":
		return "`" + name + "`"
	case "mssql":
		return "[" + name + "]"
	case "oracle":
		return `"` + strings.ToUpper(name) + `"`
	default:
		return `"` + name + `"`
	}
}

// timestampArg binds a timestamp in the representation the backing column
// stores: a `datetime('now')`-format UTC string for sqlite's TEXT columns,
// a UTC time.Time everywhere else.
func (s *SQL) timestampArg(t time.Time) any {
	if s.sqliteTime {
		return t.UTC().Format(sqliteTimeLayout)
	}
	return t.UTC()
}

// Get implements Cache. Expired rows are filtered server-side against the
// database clock, so a stale replica clock cannot resurrect an entry.
func (s *SQL) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if strings.TrimSpace(key) == "" {
		return nil, false, ErrEmptyKey
	}
	var value []byte
	err := s.db.QueryRowContext(ctx, s.getStmt, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("cache: get %q: %w", key, err)
	}
	return value, true, nil
}

// Set implements Cache.
func (s *SQL) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if strings.TrimSpace(key) == "" {
		return ErrEmptyKey
	}
	if ttl <= 0 {
		return ErrNonPositiveTTL
	}
	now := s.now()
	expiresAt := s.timestampArg(now.Add(ttl))
	nowArg := s.timestampArg(now)

	if s.setStmt != "" {
		if _, err := s.db.ExecContext(ctx, s.setStmt, key, value, expiresAt, nowArg, nowArg); err != nil {
			return fmt.Errorf("cache: set %q: %w", key, err)
		}
		return nil
	}

	// mssql/oracle: no portable single-statement upsert wired here —
	// delete+insert inside one transaction keeps replaces atomic.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("cache: set %q begin: %w", key, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, s.txDelete, key); err != nil {
		return fmt.Errorf("cache: set %q delete: %w", key, err)
	}
	if _, err := tx.ExecContext(ctx, s.txInsert, key, value, expiresAt, nowArg, nowArg); err != nil {
		return fmt.Errorf("cache: set %q insert: %w", key, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("cache: set %q commit: %w", key, err)
	}
	return nil
}

// Delete implements Cache.
func (s *SQL) Delete(ctx context.Context, key string) error {
	if strings.TrimSpace(key) == "" {
		return ErrEmptyKey
	}
	if _, err := s.db.ExecContext(ctx, s.deleteStmt, key); err != nil {
		return fmt.Errorf("cache: delete %q: %w", key, err)
	}
	return nil
}

// PruneExpired deletes every expired row and returns how many it removed.
// Run it from a scheduled task or cron; Get never returns expired rows, so
// pruning is a storage-reclamation concern, not a correctness one.
func (s *SQL) PruneExpired(ctx context.Context) (int, error) {
	res, err := s.db.ExecContext(ctx, s.pruneStmt)
	if err != nil {
		return 0, fmt.Errorf("cache: prune expired: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		// Some drivers cannot report affected rows; the prune itself worked.
		return 0, nil
	}
	return int(affected), nil
}
