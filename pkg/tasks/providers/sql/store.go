// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package sqlprovider is a durable job queue on the database the application
// already has — no broker, no Redis.
//
// It exists because the default provider keeps its jobs in memory: they
// survive a handler failure and a missing worker (the provider holds them),
// but not a restart. Durability used to mean running asynq, which means
// operating a Redis, and that is exactly the dependency Solid Queue and Oban
// removed from Rails and Elixir.
//
// It lives in the root module deliberately. The criterion the packaging ADRs
// use for moving something to a sibling module is the WEIGHT of what it drags
// in — the cloud SDKs were 42 MB of a 75 MB hello world (ADR-030), the five
// database drivers and two exporters the same shape of problem (ADR-031).
// This provider speaks database/sql and imports no driver, the way pkg/outbox
// does, so it adds nothing to anybody's build and costs the release train no
// tag and no floor.
package sqlprovider

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Flavor is the SQL dialect of the database the queue lives in.
type Flavor string

const (
	FlavorPostgres Flavor = "postgres"
	FlavorMySQL    Flavor = "mysql"
	FlavorSQLite   Flavor = "sqlite"
)

// Status is where a job is in its life.
type Status string

const (
	// StatusPending: waiting to be claimed, at or after available_at.
	StatusPending Status = "pending"
	// StatusRunning: claimed by a worker that holds a lease on it.
	StatusRunning Status = "running"
	// StatusDone: the handler returned without an error.
	StatusDone Status = "done"
	// StatusDead: out of attempts, or retired because whoever claimed it
	// never came back and it had nothing left to spend.
	StatusDead Status = "dead"
)

// DefaultTableName is where the queue lives when nothing says otherwise.
const DefaultTableName = "nucleus_jobs"

// identifierPattern is what makes interpolating the table name safe: the
// quoting helpers below quote, they do not escape.
var identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var (
	// ErrUnsupportedFlavor reports an engine this provider does not speak.
	// It names the ones it does rather than degrading quietly: a queue that
	// silently spoke a dialect it had not been tested against would lose
	// work in ways nobody would attribute to the choice of engine.
	ErrUnsupportedFlavor = errors.New("sqlprovider: unsupported database engine")
	// ErrInvalidTableName reports a table name that cannot be quoted safely.
	ErrInvalidTableName = errors.New("sqlprovider: invalid table name")
)

// Config configures the store.
type Config struct {
	// TableName defaults to DefaultTableName.
	TableName string
	// Flavor is derived from DatabaseURL when empty.
	Flavor Flavor
	// DatabaseURL is only read to derive Flavor.
	DatabaseURL string
}

// Store is the queue's table, and every statement that touches it.
type Store struct {
	db     *sql.DB
	table  string
	flavor Flavor
}

// NewStore opens the queue against db, creating the table and its indexes if
// they are not there yet.
func NewStore(db *sql.DB, cfg Config) (*Store, error) {
	if db == nil {
		return nil, errors.New("sqlprovider: db is nil")
	}
	table := strings.TrimSpace(cfg.TableName)
	if table == "" {
		table = DefaultTableName
	}
	if !identifierPattern.MatchString(table) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidTableName, table)
	}
	flavor, err := resolveFlavor(cfg)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, table: table, flavor: flavor}
	if err := s.ensureSchema(context.Background()); err != nil {
		return nil, fmt.Errorf("sqlprovider: prepare %s: %w", table, err)
	}
	return s, nil
}

// resolveFlavor takes the explicit flavor, or derives it from the URL.
//
// mssql and oracle are refused BY NAME rather than falling through to a
// default: this provider's statements have not been exercised against them,
// and a queue that quietly spoke an untested dialect would lose jobs in a way
// nobody would trace back to the engine. pkg/outbox refuses the same two for
// the same reason.
func resolveFlavor(cfg Config) (Flavor, error) {
	if f := Flavor(strings.ToLower(strings.TrimSpace(string(cfg.Flavor)))); f != "" {
		switch f {
		case FlavorPostgres, FlavorMySQL, FlavorSQLite:
			return f, nil
		default:
			return "", fmt.Errorf("%w: %q (this provider speaks postgres, mysql and sqlite)", ErrUnsupportedFlavor, f)
		}
	}
	lower := strings.ToLower(strings.TrimSpace(cfg.DatabaseURL))
	switch {
	case strings.HasPrefix(lower, "postgres://"), strings.HasPrefix(lower, "postgresql://"):
		return FlavorPostgres, nil
	case strings.HasPrefix(lower, "mysql://"):
		return FlavorMySQL, nil
	case strings.HasPrefix(lower, "sqlite://"), strings.HasPrefix(lower, "file:"), lower == "":
		return FlavorSQLite, nil
	case strings.HasPrefix(lower, "sqlserver://"), strings.HasPrefix(lower, "mssql://"):
		return "", fmt.Errorf("%w: sql server (this provider speaks postgres, mysql and sqlite)", ErrUnsupportedFlavor)
	case strings.HasPrefix(lower, "oracle://"):
		return "", fmt.Errorf("%w: oracle (this provider speaks postgres, mysql and sqlite)", ErrUnsupportedFlavor)
	default:
		return FlavorSQLite, nil
	}
}

func (s *Store) placeholder(i int) string {
	if s.flavor == FlavorPostgres {
		return fmt.Sprintf("$%d", i)
	}
	return "?"
}

// rebind rewrites `?` placeholders for the engines that want their own. The
// statements are written once, with `?`, which is what keeps them readable.
func (s *Store) rebind(query string) string {
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

func (s *Store) quoted(name string) string {
	if s.flavor == FlavorMySQL {
		return "`" + name + "`"
	}
	return `"` + name + `"`
}

func (s *Store) quotedTable() string { return s.quoted(s.table) }

// ensureSchema creates the table and its indexes, idempotently.
func (s *Store) ensureSchema(ctx context.Context) error {
	var create string
	switch s.flavor {
	case FlavorPostgres:
		create = fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id TEXT PRIMARY KEY,
			queue TEXT NOT NULL,
			task_type TEXT NOT NULL,
			payload TEXT NOT NULL,
			status TEXT NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			max_attempts INTEGER NOT NULL,
			timeout_ms BIGINT NOT NULL DEFAULT 0,
			backoff_base_ms BIGINT NOT NULL DEFAULT 0,
			backoff_max_ms BIGINT NOT NULL DEFAULT 0,
			available_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ NOT NULL,
			finished_at TIMESTAMPTZ,
			last_error TEXT,
			lease_owner TEXT,
			lease_until TIMESTAMPTZ,
			unique_key TEXT
		)`, s.quotedTable())
	case FlavorMySQL:
		create = fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id VARCHAR(191) PRIMARY KEY,
			queue VARCHAR(191) NOT NULL,
			task_type VARCHAR(255) NOT NULL,
			payload LONGTEXT NOT NULL,
			status VARCHAR(32) NOT NULL,
			attempts INT NOT NULL DEFAULT 0,
			max_attempts INT NOT NULL,
			timeout_ms BIGINT NOT NULL DEFAULT 0,
			backoff_base_ms BIGINT NOT NULL DEFAULT 0,
			backoff_max_ms BIGINT NOT NULL DEFAULT 0,
			available_at DATETIME(6) NOT NULL,
			created_at DATETIME(6) NOT NULL,
			finished_at DATETIME(6) NULL,
			last_error TEXT,
			lease_owner VARCHAR(191),
			lease_until DATETIME(6) NULL,
			unique_key VARCHAR(191) NULL
		)`, s.quotedTable())
	default:
		create = fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id TEXT PRIMARY KEY,
			queue TEXT NOT NULL,
			task_type TEXT NOT NULL,
			payload TEXT NOT NULL,
			status TEXT NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			max_attempts INTEGER NOT NULL,
			timeout_ms INTEGER NOT NULL DEFAULT 0,
			backoff_base_ms INTEGER NOT NULL DEFAULT 0,
			backoff_max_ms INTEGER NOT NULL DEFAULT 0,
			available_at DATETIME NOT NULL,
			created_at DATETIME NOT NULL,
			finished_at DATETIME,
			last_error TEXT,
			lease_owner TEXT,
			lease_until DATETIME,
			unique_key TEXT
		)`, s.quotedTable())
	}
	if _, err := s.db.ExecContext(ctx, create); err != nil {
		return err
	}

	// The claim's index. Created per dialect because MySQL has no
	// CREATE INDEX IF NOT EXISTS and emitting it there is a syntax error,
	// not a no-op — which is how the outbox failed to open a MySQL database
	// at all until NU-85.
	if err := s.ensureIndex(ctx, indexName(s.table+"_claim"), "queue, status, available_at"); err != nil {
		return err
	}
	// Uniqueness rides on a plain unique index over a NULLABLE column, which
	// every engine here treats the same way: several NULLs are allowed, so a
	// job without a key is unconstrained, and clearing the key when the job
	// finishes frees it for the next one. A partial index would be tidier on
	// Postgres and does not exist on MySQL.
	if err := s.ensureUniqueIndex(ctx, indexName(s.table+"_unique_key"), "queue, unique_key"); err != nil {
		return err
	}
	// The leadership lease lives next to the queue: the scheduler's election
	// needs no Redis precisely because every replica already shares this
	// database. One small table, created with the queue so that acquiring the
	// lease is never a DDL on the hot path.
	return s.ensureLeaderSchema(ctx)
}

// maxIndexNameLength is MySQL's identifier limit, the smallest of the three.
const maxIndexNameLength = 64

func indexName(suffix string) string {
	name := "idx_" + suffix
	if len(name) <= maxIndexNameLength {
		return name
	}
	return name[len(name)-maxIndexNameLength:]
}

// ensureUniqueIndex is ensureIndex with UNIQUE, kept separate so the
// difference is visible at the call site.
func (s *Store) ensureUniqueIndex(ctx context.Context, name, columns string) error {
	return s.ensureIndexWith(ctx, "CREATE UNIQUE INDEX", name, columns)
}

func (s *Store) ensureIndex(ctx context.Context, name, columns string) error {
	return s.ensureIndexWith(ctx, "CREATE INDEX", name, columns)
}

func (s *Store) ensureIndexWith(ctx context.Context, verb, name, columns string) error {
	if s.flavor == FlavorMySQL {
		var count int
		query := `SELECT COUNT(*) FROM information_schema.statistics
			WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?`
		if err := s.db.QueryRowContext(ctx, query, s.table, name).Scan(&count); err != nil {
			return fmt.Errorf("sqlprovider: look for index %s: %w", name, err)
		}
		if count > 0 {
			return nil
		}
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf("%s %s ON %s (%s)",
			verb, s.quoted(name), s.quotedTable(), columns)); err != nil {
			return fmt.Errorf("sqlprovider: create index %s: %w", name, err)
		}
		return nil
	}
	if _, err := s.db.ExecContext(ctx, fmt.Sprintf("%s IF NOT EXISTS %s ON %s (%s)",
		verb, s.quoted(name), s.quotedTable(), columns)); err != nil {
		return fmt.Errorf("sqlprovider: create index %s: %w", name, err)
	}
	return nil
}

// Job is one row of the queue.
type Job struct {
	ID          string
	Queue       string
	TaskType    string
	Payload     []byte
	Status      Status
	Attempts    int
	MaxAttempts int
	Timeout     time.Duration
	BackoffBase time.Duration
	BackoffMax  time.Duration
	AvailableAt time.Time
	CreatedAt   time.Time
	LastError   string
	UniqueKey   string
}

// Enqueue writes one job. The caller's time is never the engine's: every
// timestamp is bound from Go in UTC, which is the convention the rest of the
// tree follows and what keeps a server in another zone from deciding when a
// job is due.
func (s *Store) Enqueue(ctx context.Context, job Job) error {
	return s.enqueueOn(ctx, s.db, job)
}

// EnqueueTx writes one job inside the caller's transaction, so the job exists
// exactly when the work that asked for it commits.
func (s *Store) EnqueueTx(ctx context.Context, tx *sql.Tx, job Job) error {
	if tx == nil {
		return errors.New("sqlprovider: tx is nil")
	}
	return s.enqueueOn(ctx, tx, job)
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (s *Store) enqueueOn(ctx context.Context, exec execer, job Job) error {
	var uniqueKey any
	if job.UniqueKey != "" {
		uniqueKey = job.UniqueKey
	}
	query := s.rebind(fmt.Sprintf(
		`INSERT INTO %s (id, queue, task_type, payload, status, attempts, max_attempts,
			timeout_ms, backoff_base_ms, backoff_max_ms, available_at, created_at, unique_key)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?, ?)`, s.quotedTable()))
	_, err := exec.ExecContext(ctx, query,
		job.ID, job.Queue, job.TaskType, string(job.Payload), string(StatusPending),
		job.MaxAttempts, job.Timeout.Milliseconds(), job.BackoffBase.Milliseconds(),
		job.BackoffMax.Milliseconds(), job.AvailableAt.UTC(), job.CreatedAt.UTC(), uniqueKey)
	if err != nil {
		if job.UniqueKey != "" && isDuplicate(err) {
			// Somebody already enqueued this exact piece of work and it has
			// not finished. That is the point of the key, so it is not an
			// error: the caller is told which job theirs collapsed into.
			return &DuplicateError{Queue: job.Queue, UniqueKey: job.UniqueKey, ExistingID: s.lookupUnique(ctx, job.Queue, job.UniqueKey)}
		}
		return fmt.Errorf("sqlprovider: enqueue: %w", err)
	}
	return nil
}

// DuplicateError reports an enqueue collapsed into a job that was already
// there, and names it.
type DuplicateError struct {
	Queue      string
	UniqueKey  string
	ExistingID string
}

func (e *DuplicateError) Error() string {
	return fmt.Sprintf("sqlprovider: a job with unique key %q is already queued on %q (id %s)",
		e.UniqueKey, e.Queue, e.ExistingID)
}

// lookupUnique finds the job that holds a key, for the duplicate report. A
// miss returns an empty id rather than an error: the caller is already on an
// error path and the id is a courtesy.
func (s *Store) lookupUnique(ctx context.Context, queue, key string) string {
	var id string
	query := s.rebind(fmt.Sprintf(`SELECT id FROM %s WHERE queue = ? AND unique_key = ?`, s.quotedTable()))
	if err := s.db.QueryRowContext(ctx, query, queue, key).Scan(&id); err != nil {
		return ""
	}
	return id
}
