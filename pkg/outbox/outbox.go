// Package outbox provides a small SQL-backed transactional outbox runtime.
package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

const DefaultTableName = "goframe_outbox"

type Flavor string

const (
	FlavorSQLite   Flavor = "sqlite"
	FlavorPostgres Flavor = "postgres"
	FlavorMySQL    Flavor = "mysql"
)

type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusDelivered  Status = "delivered"
	StatusFailed     Status = "failed"
)

var (
	ErrNilDB          = fmt.Errorf("outbox: nil db")
	ErrNilTx          = fmt.Errorf("outbox: nil tx")
	ErrEmptyTopic     = fmt.Errorf("outbox: topic is required")
	ErrNilStore       = fmt.Errorf("outbox: store is nil")
	ErrHandlerMissing = fmt.Errorf("outbox: handler is required")
)

var sqlIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type Config struct {
	DatabaseURL string
	TableName   string
	Flavor      Flavor
}

type Entry struct {
	ID          string
	Topic       string
	Payload     any
	AvailableAt time.Time
}

type Message struct {
	ID          string
	Topic       string
	Payload     []byte
	Status      Status
	Attempts    int
	AvailableAt time.Time
	CreatedAt   time.Time
	DeliveredAt time.Time
	LastError   string
}

type RuntimeSnapshot struct {
	Enabled         bool   `json:"enabled"`
	Table           string `json:"table"`
	Flavor          string `json:"flavor,omitempty"`
	Reason          string `json:"reason,omitempty"`
	Pending         int    `json:"pending"`
	Processing      int    `json:"processing"`
	Delivered       int    `json:"delivered"`
	Failed          int    `json:"failed"`
	Total           int    `json:"total"`
	OldestPendingAt string `json:"oldest_pending_at,omitempty"`
	LastDeliveredAt string `json:"last_delivered_at,omitempty"`
}

type Store struct {
	db     *sql.DB
	table  string
	flavor Flavor
}

func NewStore(db *sql.DB, cfg Config) (*Store, error) {
	if db == nil {
		return nil, ErrNilDB
	}

	table := strings.TrimSpace(cfg.TableName)
	if table == "" {
		table = DefaultTableName
	}
	if !sqlIdentifierPattern.MatchString(table) {
		return nil, fmt.Errorf("outbox: invalid table name %q", table)
	}

	store := &Store{
		db:     db,
		table:  table,
		flavor: resolveFlavor(cfg),
	}
	if err := store.ensureSchema(context.Background()); err != nil {
		return nil, fmt.Errorf("outbox: ensure schema: %w", err)
	}
	return store, nil
}

func InspectRuntime(db *sql.DB, cfg Config) RuntimeSnapshot {
	table := strings.TrimSpace(cfg.TableName)
	if table == "" {
		table = DefaultTableName
	}
	snapshot := RuntimeSnapshot{
		Enabled: false,
		Table:   table,
		Flavor:  string(resolveFlavor(cfg)),
	}
	if db == nil {
		snapshot.Reason = "database handle not available"
		return snapshot
	}
	if !sqlIdentifierPattern.MatchString(table) {
		snapshot.Reason = "invalid table name"
		return snapshot
	}

	store := &Store{
		db:     db,
		table:  table,
		flavor: resolveFlavor(cfg),
	}
	query := fmt.Sprintf(
		`SELECT status, COUNT(*), MIN(CASE WHEN status = 'pending' THEN available_at END), MAX(delivered_at) FROM %s GROUP BY status`,
		store.quotedTable(),
	)
	rows, err := db.QueryContext(context.Background(), query)
	if err != nil {
		snapshot.Reason = summarizeInspectError(err)
		return snapshot
	}
	defer rows.Close()

	var oldestPending time.Time
	var lastDelivered time.Time
	for rows.Next() {
		var status string
		var count int
		var oldestRaw any
		var deliveredRaw any
		if err := rows.Scan(&status, &count, &oldestRaw, &deliveredRaw); err != nil {
			snapshot.Reason = summarizeInspectError(err)
			return snapshot
		}
		switch Status(strings.ToLower(strings.TrimSpace(status))) {
		case StatusPending:
			snapshot.Pending = count
			if ts, err := parseTimeValue(oldestRaw); err == nil && !ts.IsZero() {
				oldestPending = ts
			}
		case StatusProcessing:
			snapshot.Processing = count
		case StatusDelivered:
			snapshot.Delivered = count
			if ts, err := parseTimeValue(deliveredRaw); err == nil && !ts.IsZero() {
				lastDelivered = ts
			}
		case StatusFailed:
			snapshot.Failed = count
		}
		snapshot.Total += count
	}
	if err := rows.Err(); err != nil {
		snapshot.Reason = summarizeInspectError(err)
		return snapshot
	}

	snapshot.Enabled = true
	if !oldestPending.IsZero() {
		snapshot.OldestPendingAt = oldestPending.UTC().Format(time.RFC3339)
	}
	if !lastDelivered.IsZero() {
		snapshot.LastDeliveredAt = lastDelivered.UTC().Format(time.RFC3339)
	}
	return snapshot
}

func (s *Store) Enqueue(ctx context.Context, entry Entry) (Message, error) {
	if s == nil {
		return Message{}, ErrNilStore
	}
	return s.enqueueOn(ctx, s.db, entry)
}

func (s *Store) EnqueueTx(ctx context.Context, tx *sql.Tx, entry Entry) (Message, error) {
	if s == nil {
		return Message{}, ErrNilStore
	}
	if tx == nil {
		return Message{}, ErrNilTx
	}
	return s.enqueueOn(ctx, tx, entry)
}

func (s *Store) Snapshot(ctx context.Context) RuntimeSnapshot {
	if s == nil {
		return RuntimeSnapshot{Enabled: false, Reason: "store is nil", Table: DefaultTableName}
	}
	return InspectRuntime(s.db, Config{TableName: s.table, Flavor: s.flavor})
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (s *Store) enqueueOn(ctx context.Context, exec execer, entry Entry) (Message, error) {
	if strings.TrimSpace(entry.Topic) == "" {
		return Message{}, ErrEmptyTopic
	}
	payload, err := encodePayload(entry.Payload)
	if err != nil {
		return Message{}, err
	}
	now := time.Now().UTC()
	availableAt := entry.AvailableAt.UTC()
	if availableAt.IsZero() {
		availableAt = now
	}
	id := strings.TrimSpace(entry.ID)
	if id == "" {
		id = uuid.NewString()
	}

	query := fmt.Sprintf(
		`INSERT INTO %s (id, topic, payload, status, available_at, created_at, delivered_at, attempts, last_error, lease_owner, lease_until) VALUES (%s, %s, %s, %s, %s, %s, NULL, %s, NULL, NULL, NULL)`,
		s.quotedTable(),
		s.placeholder(1), s.placeholder(2), s.placeholder(3), s.placeholder(4), s.placeholder(5), s.placeholder(6), s.placeholder(7),
	)
	if _, err := exec.ExecContext(ctx, query, id, strings.TrimSpace(entry.Topic), string(payload), string(StatusPending), availableAt, now, 0); err != nil {
		return Message{}, fmt.Errorf("outbox enqueue: %w", err)
	}

	return Message{
		ID:          id,
		Topic:       strings.TrimSpace(entry.Topic),
		Payload:     payload,
		Status:      StatusPending,
		Attempts:    0,
		AvailableAt: availableAt,
		CreatedAt:   now,
	}, nil
}

func encodePayload(payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("outbox encode payload: %w", err)
	}
	return body, nil
}

func (s *Store) ensureSchema(ctx context.Context) error {
	var createStmt string
	switch s.flavor {
	case FlavorPostgres:
		createStmt = fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s (id TEXT PRIMARY KEY, topic TEXT NOT NULL, payload TEXT NOT NULL, status TEXT NOT NULL, available_at TIMESTAMPTZ NOT NULL, created_at TIMESTAMPTZ NOT NULL, delivered_at TIMESTAMPTZ NULL, attempts INTEGER NOT NULL, last_error TEXT NULL, lease_owner TEXT NULL, lease_until TIMESTAMPTZ NULL)`,
			s.quotedTable(),
		)
	case FlavorMySQL:
		createStmt = fmt.Sprintf(
			"CREATE TABLE IF NOT EXISTS %s (id VARCHAR(191) PRIMARY KEY, topic VARCHAR(255) NOT NULL, payload LONGTEXT NOT NULL, status VARCHAR(32) NOT NULL, available_at DATETIME(6) NOT NULL, created_at DATETIME(6) NOT NULL, delivered_at DATETIME(6) NULL, attempts INTEGER NOT NULL, last_error TEXT NULL, lease_owner VARCHAR(191) NULL, lease_until DATETIME(6) NULL)",
			s.quotedTable(),
		)
	default:
		createStmt = fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s (id TEXT PRIMARY KEY, topic TEXT NOT NULL, payload TEXT NOT NULL, status TEXT NOT NULL, available_at DATETIME NOT NULL, created_at DATETIME NOT NULL, delivered_at DATETIME NULL, attempts INTEGER NOT NULL, last_error TEXT NULL, lease_owner TEXT NULL, lease_until DATETIME NULL)`,
			s.quotedTable(),
		)
	}
	if _, err := s.db.ExecContext(ctx, createStmt); err != nil {
		return err
	}

	indexes := []string{
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (status, available_at)`, s.quotedIdentifier("idx_"+s.table+"_status_available_at"), s.quotedTable()),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (lease_until)`, s.quotedIdentifier("idx_"+s.table+"_lease_until"), s.quotedTable()),
	}
	for _, stmt := range indexes {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) placeholder(i int) string {
	if s.flavor == FlavorPostgres {
		return fmt.Sprintf("$%d", i)
	}
	return "?"
}

func (s *Store) quotedTable() string {
	return s.quotedIdentifier(s.table)
}

func (s *Store) quotedIdentifier(name string) string {
	switch s.flavor {
	case FlavorMySQL:
		return "`" + name + "`"
	default:
		return `"` + name + `"`
	}
}

func resolveFlavor(cfg Config) Flavor {
	if cfg.Flavor != "" {
		return cfg.Flavor
	}
	raw := strings.ToLower(strings.TrimSpace(cfg.DatabaseURL))
	switch {
	case strings.HasPrefix(raw, "postgres://"), strings.HasPrefix(raw, "postgresql://"):
		return FlavorPostgres
	case strings.HasPrefix(raw, "mysql://"):
		return FlavorMySQL
	default:
		return FlavorSQLite
	}
}

func parseTimeValue(raw any) (time.Time, error) {
	switch v := raw.(type) {
	case nil:
		return time.Time{}, nil
	case time.Time:
		return v.UTC(), nil
	case string:
		return parseTimeString(v)
	case []byte:
		return parseTimeString(string(v))
	default:
		return time.Time{}, fmt.Errorf("unsupported time type %T", raw)
	}
}

func parseTimeString(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, nil
	}
	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if ts, err := time.Parse(layout, value); err == nil {
			return ts.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid time %q", raw)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMessage(scanner rowScanner) (Message, error) {
	var (
		row          Message
		payloadRaw   any
		statusRaw    string
		availableRaw any
		createdRaw   any
		deliveredRaw any
		lastErrorRaw any
	)
	if err := scanner.Scan(
		&row.ID,
		&row.Topic,
		&payloadRaw,
		&statusRaw,
		&availableRaw,
		&createdRaw,
		&deliveredRaw,
		&row.Attempts,
		&lastErrorRaw,
	); err != nil {
		return Message{}, err
	}
	switch payload := payloadRaw.(type) {
	case nil:
		row.Payload = nil
	case string:
		row.Payload = []byte(payload)
	case []byte:
		row.Payload = append([]byte(nil), payload...)
	default:
		return Message{}, fmt.Errorf("unsupported payload type %T", payloadRaw)
	}
	row.Status = Status(strings.ToLower(strings.TrimSpace(statusRaw)))
	var err error
	row.AvailableAt, err = parseTimeValue(availableRaw)
	if err != nil {
		return Message{}, err
	}
	row.CreatedAt, err = parseTimeValue(createdRaw)
	if err != nil {
		return Message{}, err
	}
	row.DeliveredAt, err = parseTimeValue(deliveredRaw)
	if err != nil {
		return Message{}, err
	}
	switch value := lastErrorRaw.(type) {
	case nil:
		row.LastError = ""
	case string:
		row.LastError = value
	case []byte:
		row.LastError = string(value)
	default:
		return Message{}, fmt.Errorf("unsupported last_error type %T", lastErrorRaw)
	}
	return row, nil
}

func summarizeInspectError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(strings.ToLower(err.Error()))
	switch {
	case strings.Contains(msg, "no such table"),
		strings.Contains(msg, "does not exist"),
		strings.Contains(msg, "doesn't exist"),
		strings.Contains(msg, "unknown table"),
		strings.Contains(msg, "undefined table"):
		return "outbox table is not initialized"
	default:
		return err.Error()
	}
}
