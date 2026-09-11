package apikeys

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Flavor is the SQL dialect, handled explicitly for the same reason the
// accounts store does it: placeholders and timestamp types differ, and
// guessing is how a store "works" on one engine.
type Flavor string

const (
	FlavorSQLite   Flavor = "sqlite"
	FlavorPostgres Flavor = "postgres"
	FlavorMySQL    Flavor = "mysql"
)

// SQLStore keeps keys in one table.
type SQLStore struct {
	db     *sql.DB
	flavor Flavor
	table  string
}

// SQLStoreConfig configures the store. Table defaults to
// "nucleus_api_keys".
type SQLStoreConfig struct {
	Flavor Flavor
	Table  string
}

// NewSQLStore creates the table if it does not exist.
func NewSQLStore(ctx context.Context, db *sql.DB, cfg SQLStoreConfig) (*SQLStore, error) {
	if db == nil {
		return nil, errors.New("apikeys: nil database")
	}
	flavor := Flavor(strings.ToLower(strings.TrimSpace(string(cfg.Flavor))))
	switch flavor {
	case FlavorSQLite, FlavorPostgres, FlavorMySQL:
	default:
		return nil, fmt.Errorf("apikeys: unsupported SQL flavor %q", cfg.Flavor)
	}
	table := strings.TrimSpace(cfg.Table)
	if table == "" {
		table = "nucleus_api_keys"
	}

	s := &SQLStore{db: db, flavor: flavor, table: table}
	if err := s.ensureSchema(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SQLStore) rebind(query string) string {
	if s.flavor != FlavorPostgres {
		return query
	}
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			fmt.Fprintf(&b, "$%d", n)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (s *SQLStore) ensureSchema(ctx context.Context) error {
	var stmt string
	switch s.flavor {
	case FlavorPostgres:
		stmt = fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			owner_id TEXT NOT NULL DEFAULT '',
			secret_hash TEXT NOT NULL,
			scopes TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL,
			expires_at TIMESTAMPTZ NULL,
			last_used_at TIMESTAMPTZ NULL,
			revoked_at TIMESTAMPTZ NULL,
			rotated_from TEXT NOT NULL DEFAULT '')`, s.table)
	case FlavorMySQL:
		stmt = fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id VARCHAR(64) PRIMARY KEY,
			name VARCHAR(191) NOT NULL DEFAULT '',
			owner_id VARCHAR(64) NOT NULL DEFAULT '',
			secret_hash VARCHAR(64) NOT NULL,
			scopes TEXT NOT NULL,
			created_at DATETIME(6) NOT NULL,
			expires_at DATETIME(6) NULL,
			last_used_at DATETIME(6) NULL,
			revoked_at DATETIME(6) NULL,
			rotated_from VARCHAR(64) NOT NULL DEFAULT '')`, s.table)
	default:
		stmt = fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			owner_id TEXT NOT NULL DEFAULT '',
			secret_hash TEXT NOT NULL,
			scopes TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL,
			expires_at TIMESTAMP NULL,
			last_used_at TIMESTAMP NULL,
			revoked_at TIMESTAMP NULL,
			rotated_from TEXT NOT NULL DEFAULT '')`, s.table)
	}
	if _, err := s.db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("apikeys: ensure schema: %w", err)
	}
	return nil
}

// Create implements Store. It also serves as the update path for rotation,
// so one row's lifecycle is written in one place.
func (s *SQLStore) Create(ctx context.Context, key Key) error {
	del := s.rebind(fmt.Sprintf(`DELETE FROM %s WHERE id = ?`, s.table))
	if _, err := s.db.ExecContext(ctx, del, key.ID); err != nil {
		return fmt.Errorf("apikeys: replace: %w", err)
	}
	insert := s.rebind(fmt.Sprintf(
		`INSERT INTO %s (id, name, owner_id, secret_hash, scopes, created_at, expires_at, last_used_at, revoked_at, rotated_from)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, s.table))
	if _, err := s.db.ExecContext(ctx, insert,
		key.ID, key.Name, key.OwnerID, key.SecretHash, strings.Join(key.Scopes, " "),
		key.CreatedAt, nullTime(key.ExpiresAt), nullTime(key.LastUsedAt), nullTime(key.RevokedAt),
		key.RotatedFrom); err != nil {
		return fmt.Errorf("apikeys: insert: %w", err)
	}
	return nil
}

// ByID implements Store.
func (s *SQLStore) ByID(ctx context.Context, id string) (Key, error) {
	query := s.rebind(fmt.Sprintf(
		`SELECT id, name, owner_id, secret_hash, scopes, created_at, expires_at, last_used_at, revoked_at, rotated_from
		 FROM %s WHERE id = ?`, s.table))
	key, err := scanKey(s.db.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Key{}, ErrNotFound
	}
	return key, err
}

// List implements Store. An empty ownerID lists every key, which is what an
// operator surface asks for.
func (s *SQLStore) List(ctx context.Context, ownerID string) ([]Key, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if strings.TrimSpace(ownerID) == "" {
		rows, err = s.db.QueryContext(ctx, fmt.Sprintf(
			`SELECT id, name, owner_id, secret_hash, scopes, created_at, expires_at, last_used_at, revoked_at, rotated_from
			 FROM %s ORDER BY created_at DESC`, s.table))
	} else {
		rows, err = s.db.QueryContext(ctx, s.rebind(fmt.Sprintf(
			`SELECT id, name, owner_id, secret_hash, scopes, created_at, expires_at, last_used_at, revoked_at, rotated_from
			 FROM %s WHERE owner_id = ? ORDER BY created_at DESC`, s.table)), ownerID)
	}
	if err != nil {
		return nil, fmt.Errorf("apikeys: list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Key
	for rows.Next() {
		key, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

// Revoke implements Store.
func (s *SQLStore) Revoke(ctx context.Context, id string, at time.Time) error {
	query := s.rebind(fmt.Sprintf(`UPDATE %s SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, s.table))
	res, err := s.db.ExecContext(ctx, query, at.UTC(), id)
	if err != nil {
		return fmt.Errorf("apikeys: revoke: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		// Either it does not exist or it was already revoked. The
		// caller's goal holds in the second case, so only the first is
		// an error.
		if _, err := s.ByID(ctx, id); err != nil {
			return ErrNotFound
		}
	}
	return nil
}

// TouchLastUsed implements Store.
func (s *SQLStore) TouchLastUsed(ctx context.Context, id string, at time.Time) error {
	query := s.rebind(fmt.Sprintf(`UPDATE %s SET last_used_at = ? WHERE id = ?`, s.table))
	if _, err := s.db.ExecContext(ctx, query, at.UTC(), id); err != nil {
		return fmt.Errorf("apikeys: touch: %w", err)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanKey(row rowScanner) (Key, error) {
	var (
		k                          Key
		scopes                     string
		expires, lastUsed, revoked sql.NullTime
	)
	if err := row.Scan(&k.ID, &k.Name, &k.OwnerID, &k.SecretHash, &scopes,
		&k.CreatedAt, &expires, &lastUsed, &revoked, &k.RotatedFrom); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Key{}, err
		}
		return Key{}, fmt.Errorf("apikeys: scan: %w", err)
	}
	k.Scopes = strings.Fields(scopes)
	if expires.Valid {
		k.ExpiresAt = expires.Time
	}
	if lastUsed.Valid {
		k.LastUsedAt = lastUsed.Time
	}
	if revoked.Valid {
		k.RevokedAt = revoked.Time
	}
	return k, nil
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}
