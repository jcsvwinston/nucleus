package accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Flavor is the SQL dialect the store speaks. The differences are small and
// real: placeholder syntax, the type of a timestamp, and how a conflict is
// reported. They are handled explicitly rather than by string-matching
// driver errors, which is how a store ends up "working" on one engine.
type Flavor string

const (
	FlavorSQLite   Flavor = "sqlite"
	FlavorPostgres Flavor = "postgres"
	FlavorMySQL    Flavor = "mysql"
)

// SQLStore keeps accounts, tokens and failure counters in three tables it
// owns. An application with its own users table implements Store instead;
// this one exists so a new application has somewhere for them to live on
// the first day.
type SQLStore struct {
	db     *sql.DB
	flavor Flavor
	prefix string
}

// SQLStoreConfig configures the store. TablePrefix defaults to "nucleus_".
type SQLStoreConfig struct {
	Flavor      Flavor
	TablePrefix string
}

// NewSQLStore creates the tables if they do not exist and returns the
// store.
func NewSQLStore(ctx context.Context, db *sql.DB, cfg SQLStoreConfig) (*SQLStore, error) {
	if db == nil {
		return nil, errors.New("accounts: nil database")
	}
	flavor := Flavor(strings.ToLower(strings.TrimSpace(string(cfg.Flavor))))
	switch flavor {
	case FlavorSQLite, FlavorPostgres, FlavorMySQL:
	case "":
		return nil, errors.New("accounts: a SQL flavor is required (sqlite, postgres or mysql)")
	default:
		// Failing here beats emitting the sqlite grammar at an engine
		// that does not speak it and finding out at the first write.
		return nil, fmt.Errorf("accounts: unsupported SQL flavor %q", cfg.Flavor)
	}
	prefix := strings.TrimSpace(cfg.TablePrefix)
	if prefix == "" {
		prefix = "nucleus_"
	}

	s := &SQLStore{db: db, flavor: flavor, prefix: prefix}
	if err := s.ensureSchema(ctx); err != nil {
		return nil, err
	}
	if err := s.ensureMFASchema(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SQLStore) accountsTable() string { return s.prefix + "accounts" }
func (s *SQLStore) tokensTable() string   { return s.prefix + "account_tokens" }
func (s *SQLStore) failuresTable() string { return s.prefix + "account_failures" }

// rebind turns the ? placeholders these statements are written with into
// the dialect's own, so one statement text serves every engine.
func (s *SQLStore) rebind(query string) string {
	if s.flavor != FlavorPostgres {
		return query
	}
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			b.WriteString(fmt.Sprintf("$%d", n))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (s *SQLStore) ensureSchema(ctx context.Context) error {
	var stmts []string
	switch s.flavor {
	case FlavorPostgres:
		stmts = []string{
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				id TEXT PRIMARY KEY,
				email TEXT NOT NULL UNIQUE,
				username TEXT NOT NULL DEFAULT '',
				password_hash TEXT NOT NULL,
				email_verified BOOLEAN NOT NULL DEFAULT FALSE,
				disabled BOOLEAN NOT NULL DEFAULT FALSE,
				role TEXT NOT NULL DEFAULT '',
				created_at TIMESTAMPTZ NOT NULL,
				updated_at TIMESTAMPTZ NOT NULL)`, s.accountsTable()),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				hash TEXT PRIMARY KEY,
				account_id TEXT NOT NULL,
				purpose TEXT NOT NULL,
				expires_at TIMESTAMPTZ NOT NULL,
				created_at TIMESTAMPTZ NOT NULL,
				used_at TIMESTAMPTZ NULL)`, s.tokensTable()),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				id BIGSERIAL PRIMARY KEY,
				failure_key TEXT NOT NULL,
				occurred_at TIMESTAMPTZ NOT NULL)`, s.failuresTable()),
		}
	case FlavorMySQL:
		stmts = []string{
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				id VARCHAR(64) PRIMARY KEY,
				email VARCHAR(320) NOT NULL UNIQUE,
				username VARCHAR(191) NOT NULL DEFAULT '',
				password_hash VARCHAR(255) NOT NULL,
				email_verified BOOLEAN NOT NULL DEFAULT FALSE,
				disabled BOOLEAN NOT NULL DEFAULT FALSE,
				role VARCHAR(191) NOT NULL DEFAULT '',
				created_at DATETIME(6) NOT NULL,
				updated_at DATETIME(6) NOT NULL)`, s.accountsTable()),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				hash VARCHAR(64) PRIMARY KEY,
				account_id VARCHAR(64) NOT NULL,
				purpose VARCHAR(64) NOT NULL,
				expires_at DATETIME(6) NOT NULL,
				created_at DATETIME(6) NOT NULL,
				used_at DATETIME(6) NULL)`, s.tokensTable()),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				id BIGINT AUTO_INCREMENT PRIMARY KEY,
				failure_key VARCHAR(191) NOT NULL,
				occurred_at DATETIME(6) NOT NULL)`, s.failuresTable()),
		}
	default:
		stmts = []string{
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				id TEXT PRIMARY KEY,
				email TEXT NOT NULL UNIQUE,
				username TEXT NOT NULL DEFAULT '',
				password_hash TEXT NOT NULL,
				email_verified INTEGER NOT NULL DEFAULT 0,
				disabled INTEGER NOT NULL DEFAULT 0,
				role TEXT NOT NULL DEFAULT '',
				created_at TIMESTAMP NOT NULL,
				updated_at TIMESTAMP NOT NULL)`, s.accountsTable()),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				hash TEXT PRIMARY KEY,
				account_id TEXT NOT NULL,
				purpose TEXT NOT NULL,
				expires_at TIMESTAMP NOT NULL,
				created_at TIMESTAMP NOT NULL,
				used_at TIMESTAMP NULL)`, s.tokensTable()),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				failure_key TEXT NOT NULL,
				occurred_at TIMESTAMP NOT NULL)`, s.failuresTable()),
		}
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("accounts: ensure schema: %w", err)
		}
	}

	// Indexes are created separately, because "create it if it is not
	// there" is where the three dialects stop agreeing: MySQL has no
	// CREATE INDEX IF NOT EXISTS at all, and emitting it there is a
	// syntax error — which is how this store failed to open a MySQL
	// database until its live test ran against one.
	for _, idx := range []struct{ name, table, columns string }{
		{s.indexName("tokens_account"), s.tokensTable(), "account_id, purpose"},
		{s.indexName("failures_key"), s.failuresTable(), "failure_key, occurred_at"},
	} {
		if err := s.ensureIndex(ctx, idx.name, idx.table, idx.columns); err != nil {
			return err
		}
	}
	return nil
}

// maxIndexNameLength is MySQL's identifier limit, and the smallest of the
// three. A prefix long enough to overflow it produced a create that failed
// only on one engine.
const maxIndexNameLength = 64

// indexName builds an index name that fits every engine's identifier limit,
// keeping the tail — where the distinguishing part is — when a long table
// prefix would overflow it.
func (s *SQLStore) indexName(suffix string) string {
	name := "idx_" + s.prefix + suffix
	if len(name) <= maxIndexNameLength {
		return name
	}
	return name[len(name)-maxIndexNameLength:]
}

// ensureIndex creates an index when it is missing, in the way each engine
// allows. MySQL is asked information_schema rather than being handed a
// statement whose error would have to be swallowed: swallowing "duplicate
// key name" also swallows every other reason a create can fail.
func (s *SQLStore) ensureIndex(ctx context.Context, name, table, columns string) error {
	if s.flavor == FlavorMySQL {
		var count int
		query := s.rebind(`SELECT COUNT(*) FROM information_schema.statistics
			WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?`)
		if err := s.db.QueryRowContext(ctx, query, table, name).Scan(&count); err != nil {
			return fmt.Errorf("accounts: look for index %s: %w", name, err)
		}
		if count > 0 {
			return nil
		}
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf("CREATE INDEX %s ON %s (%s)", name, table, columns)); err != nil {
			return fmt.Errorf("accounts: create index %s: %w", name, err)
		}
		return nil
	}

	stmt := fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s (%s)", name, table, columns)
	if _, err := s.db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("accounts: create index %s: %w", name, err)
	}
	return nil
}

// Create implements Store. A duplicate address is reported as ErrEmailTaken
// by asking the database, not by checking first: a read-then-write leaves a
// window where two registrations both find the address free.
func (s *SQLStore) Create(ctx context.Context, account Account) (Account, error) {
	query := s.rebind(fmt.Sprintf(
		`INSERT INTO %s (id, email, username, password_hash, email_verified, disabled, role, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, s.accountsTable()))
	_, err := s.db.ExecContext(ctx, query,
		account.ID, account.Email, account.Username, account.PasswordHash,
		account.EmailVerified, account.Disabled, account.Role,
		account.CreatedAt, account.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return Account{}, ErrEmailTaken
		}
		return Account{}, fmt.Errorf("accounts: insert: %w", err)
	}
	return account, nil
}

// ByID implements Store.
func (s *SQLStore) ByID(ctx context.Context, id string) (Account, error) {
	return s.queryOne(ctx, "id", id)
}

// ByEmail implements Store.
func (s *SQLStore) ByEmail(ctx context.Context, email string) (Account, error) {
	return s.queryOne(ctx, "email", normalizeEmail(email))
}

func (s *SQLStore) queryOne(ctx context.Context, column, value string) (Account, error) {
	query := s.rebind(fmt.Sprintf(
		`SELECT id, email, username, password_hash, email_verified, disabled, role, created_at, updated_at
		 FROM %s WHERE %s = ?`, s.accountsTable(), column))
	row := s.db.QueryRowContext(ctx, query, value)

	var a Account
	err := row.Scan(&a.ID, &a.Email, &a.Username, &a.PasswordHash,
		&a.EmailVerified, &a.Disabled, &a.Role, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("accounts: select: %w", err)
	}
	return a, nil
}

// Update implements Store.
func (s *SQLStore) Update(ctx context.Context, account Account) error {
	query := s.rebind(fmt.Sprintf(
		`UPDATE %s SET email = ?, username = ?, password_hash = ?, email_verified = ?,
		 disabled = ?, role = ?, updated_at = ? WHERE id = ?`, s.accountsTable()))
	res, err := s.db.ExecContext(ctx, query,
		account.Email, account.Username, account.PasswordHash, account.EmailVerified,
		account.Disabled, account.Role, account.UpdatedAt, account.ID)
	if err != nil {
		return fmt.Errorf("accounts: update: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateToken implements Store.
func (s *SQLStore) CreateToken(ctx context.Context, token Token) error {
	query := s.rebind(fmt.Sprintf(
		`INSERT INTO %s (hash, account_id, purpose, expires_at, created_at) VALUES (?, ?, ?, ?, ?)`,
		s.tokensTable()))
	if _, err := s.db.ExecContext(ctx, query,
		token.Hash, token.AccountID, string(token.Purpose), token.ExpiresAt, token.CreatedAt); err != nil {
		return fmt.Errorf("accounts: insert token: %w", err)
	}
	return nil
}

// ConsumeToken implements Store atomically: the UPDATE carries every
// condition, so two concurrent uses of the same link cannot both win. A
// read-then-write here would make a password-reset link replayable under
// exactly the race an attacker would try to cause.
func (s *SQLStore) ConsumeToken(ctx context.Context, purpose TokenPurpose, hash string) (Token, error) {
	now := time.Now().UTC()
	query := s.rebind(fmt.Sprintf(
		`UPDATE %s SET used_at = ?
		 WHERE hash = ? AND purpose = ? AND used_at IS NULL AND expires_at > ?`,
		s.tokensTable()))
	res, err := s.db.ExecContext(ctx, query, now, hash, string(purpose), now)
	if err != nil {
		return Token{}, fmt.Errorf("accounts: consume token: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return Token{}, fmt.Errorf("accounts: consume token: %w", err)
	}
	if affected == 0 {
		return Token{}, ErrInvalidToken
	}

	selectQuery := s.rebind(fmt.Sprintf(
		`SELECT hash, account_id, purpose, expires_at, created_at FROM %s WHERE hash = ?`,
		s.tokensTable()))
	var t Token
	var purposeText string
	if err := s.db.QueryRowContext(ctx, selectQuery, hash).
		Scan(&t.Hash, &t.AccountID, &purposeText, &t.ExpiresAt, &t.CreatedAt); err != nil {
		return Token{}, fmt.Errorf("accounts: read consumed token: %w", err)
	}
	t.Purpose = TokenPurpose(purposeText)
	t.UsedAt = now
	return t, nil
}

// DeleteTokens implements Store.
func (s *SQLStore) DeleteTokens(ctx context.Context, accountID string, purpose TokenPurpose) error {
	query := s.rebind(fmt.Sprintf(`DELETE FROM %s WHERE account_id = ? AND purpose = ?`, s.tokensTable()))
	if _, err := s.db.ExecContext(ctx, query, accountID, string(purpose)); err != nil {
		return fmt.Errorf("accounts: delete tokens: %w", err)
	}
	return nil
}

// RecordFailure implements Store, returning the count INCLUDING this one.
func (s *SQLStore) RecordFailure(ctx context.Context, key string, now time.Time) (int, error) {
	insert := s.rebind(fmt.Sprintf(`INSERT INTO %s (failure_key, occurred_at) VALUES (?, ?)`, s.failuresTable()))
	if _, err := s.db.ExecContext(ctx, insert, key, now.UTC()); err != nil {
		return 0, fmt.Errorf("accounts: record failure: %w", err)
	}
	return s.FailureCount(ctx, key, now)
}

// FailureCount implements Store.
func (s *SQLStore) FailureCount(ctx context.Context, key string, now time.Time) (int, error) {
	query := s.rebind(fmt.Sprintf(
		`SELECT COUNT(*) FROM %s WHERE failure_key = ? AND occurred_at > ?`, s.failuresTable()))
	var count int
	if err := s.db.QueryRowContext(ctx, query, key, now.UTC().Add(-s.window())).Scan(&count); err != nil {
		return 0, fmt.Errorf("accounts: count failures: %w", err)
	}
	return count, nil
}

// ClearFailures implements Store.
func (s *SQLStore) ClearFailures(ctx context.Context, key string) error {
	query := s.rebind(fmt.Sprintf(`DELETE FROM %s WHERE failure_key = ?`, s.failuresTable()))
	if _, err := s.db.ExecContext(ctx, query, key); err != nil {
		return fmt.Errorf("accounts: clear failures: %w", err)
	}
	return nil
}

// window is how far back a failure counts. The store keeps its own copy of
// the value rather than reading the service's config, because the count is
// a SQL predicate: a service-level window would mean loading every row to
// filter it in Go.
func (s *SQLStore) window() time.Duration { return 15 * time.Minute }

// PurgeExpired drops used and expired tokens and stale failure rows. An
// application calls it from a scheduled job; nothing depends on it for
// correctness, because every read already filters on time.
func (s *SQLStore) PurgeExpired(ctx context.Context, now time.Time) (int64, error) {
	var total int64
	tokens := s.rebind(fmt.Sprintf(`DELETE FROM %s WHERE expires_at < ? OR used_at IS NOT NULL`, s.tokensTable()))
	res, err := s.db.ExecContext(ctx, tokens, now.UTC())
	if err != nil {
		return 0, fmt.Errorf("accounts: purge tokens: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil {
		total += n
	}

	failures := s.rebind(fmt.Sprintf(`DELETE FROM %s WHERE occurred_at < ?`, s.failuresTable()))
	res, err = s.db.ExecContext(ctx, failures, now.UTC().Add(-s.window()))
	if err != nil {
		return total, fmt.Errorf("accounts: purge failures: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil {
		total += n
	}
	return total, nil
}

// isUniqueViolation asks each engine the way it answers. Matching on the
// message text is what makes a store "portable" until an engine is
// localised — quark learned that one the expensive way — so the check is on
// the CODE where the driver exposes one, with the message as the last
// resort for drivers that expose nothing else.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// Postgres (pgx / lib/pq): SQLSTATE 23505.
	var sqlState interface{ SQLState() string }
	if errors.As(err, &sqlState) && sqlState.SQLState() == "23505" {
		return true
	}
	// MySQL: 1062. SQLite: 1555 / 2067 (SQLITE_CONSTRAINT_PRIMARYKEY and
	// _UNIQUE), 19 for the generic constraint code.
	var number interface{ Number() uint16 }
	if errors.As(err, &number) && number.Number() == 1062 {
		return true
	}
	var code interface{ Code() int }
	if errors.As(err, &code) {
		switch code.Code() {
		case 19, 1555, 2067:
			return true
		}
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicate entry") ||
		strings.Contains(msg, "unique violation")
}
