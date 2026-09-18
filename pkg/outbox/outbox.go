// Package outbox provides a small SQL-backed transactional outbox runtime.
package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const DefaultTableName = "nucleus_outbox"

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

	// Topics breaks the same counts down per topic, and carries the reason
	// the last failure on each one failed.
	//
	// Without it a panel that shows mail delivery could only display the
	// WHOLE outbox and say so: an application that also queues webhooks
	// would read "4 pending" on its mail screen as four unsent mails
	// (NU-76). And a stuck topic showed a count with no cause, when the
	// cause is the entire reason anybody is looking.
	Topics []TopicSnapshot `json:"topics,omitempty"`
}

// TopicSnapshot is one topic's share of the outbox.
type TopicSnapshot struct {
	Topic      string `json:"topic"`
	Pending    int    `json:"pending"`
	Processing int    `json:"processing"`
	Delivered  int    `json:"delivered"`
	Failed     int    `json:"failed"`
	Total      int    `json:"total"`
	// LastError is the most recent failure recorded on this topic, and
	// LastErrorAt when it happened. Empty when nothing has failed.
	LastError   string `json:"last_error,omitempty"`
	LastErrorAt string `json:"last_error_at,omitempty"`
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

	flavor, err := resolveFlavor(cfg)
	if err != nil {
		return nil, err
	}

	store := &Store{
		db:     db,
		table:  table,
		flavor: flavor,
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
	flavor, flavorErr := resolveFlavor(cfg)
	snapshot := RuntimeSnapshot{
		Enabled: false,
		Table:   table,
		Flavor:  string(flavor),
	}
	if flavorErr != nil {
		snapshot.Reason = flavorErr.Error()
		return snapshot
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
		flavor: flavor,
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
	snapshot.Topics = store.topicSnapshots(context.Background())
	return snapshot
}

// topicSnapshots counts the outbox per topic, with the last failure on each.
//
// It is a second query on purpose: the totals above answer "is the outbox
// healthy", which every caller wants, and this answers "what about MY topic",
// which is what a screen showing one kind of message needs. Failing to read it
// leaves the totals intact rather than blanking the whole snapshot.
func (s *Store) topicSnapshots(ctx context.Context) []TopicSnapshot {
	query := fmt.Sprintf(
		`SELECT topic, status, COUNT(*) FROM %s GROUP BY topic, status`, s.quotedTable())
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()

	byTopic := map[string]*TopicSnapshot{}
	order := make([]string, 0, 8)
	for rows.Next() {
		var topic, status string
		var count int
		if err := rows.Scan(&topic, &status, &count); err != nil {
			return nil
		}
		entry, ok := byTopic[topic]
		if !ok {
			entry = &TopicSnapshot{Topic: topic}
			byTopic[topic] = entry
			order = append(order, topic)
		}
		switch Status(strings.ToLower(strings.TrimSpace(status))) {
		case StatusPending:
			entry.Pending = count
		case StatusProcessing:
			entry.Processing = count
		case StatusDelivered:
			entry.Delivered = count
		case StatusFailed:
			entry.Failed = count
		}
		entry.Total += count
	}
	if err := rows.Err(); err != nil {
		return nil
	}
	sort.Strings(order)

	// The last error per topic, from the rows that carry one. A message that
	// is retrying has a last_error too, and that is the point: a topic that is
	// struggling should not have to reach the dead letter before anybody can
	// see why.
	errQuery := fmt.Sprintf(
		`SELECT topic, last_error, created_at FROM %s WHERE last_error IS NOT NULL AND last_error <> '' ORDER BY created_at ASC`,
		s.quotedTable())
	errRows, err := s.db.QueryContext(ctx, errQuery)
	if err == nil {
		defer func() { _ = errRows.Close() }()
		for errRows.Next() {
			var topic, lastError string
			var at any
			if err := errRows.Scan(&topic, &lastError, &at); err != nil {
				break
			}
			entry, ok := byTopic[topic]
			if !ok {
				continue
			}
			entry.LastError = lastError
			if ts, err := parseTimeValue(at); err == nil && !ts.IsZero() {
				entry.LastErrorAt = ts.UTC().Format(time.RFC3339)
			}
		}
	}

	out := make([]TopicSnapshot, 0, len(order))
	for _, topic := range order {
		out = append(out, *byTopic[topic])
	}
	return out
}

// TopicSnapshotFor returns one topic's share, or false when the outbox holds
// nothing under that name. It is the narrow question a screen about one kind
// of message asks, without having to scan the slice itself.
func (r RuntimeSnapshot) TopicSnapshotFor(topic string) (TopicSnapshot, bool) {
	for _, t := range r.Topics {
		if t.Topic == topic {
			return t, true
		}
	}
	return TopicSnapshot{}, false
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

	for _, idx := range []struct{ name, columns string }{
		{indexName(s.table + "_status_available_at"), "status, available_at"},
		{indexName(s.table + "_lease_until"), "lease_until"},
	} {
		if err := s.ensureIndex(ctx, idx.name, idx.columns); err != nil {
			return err
		}
	}
	return nil
}

// maxIndexNameLength is MySQL's identifier limit, and the smallest of the
// engines this store opens.
const maxIndexNameLength = 64

// indexName builds an index name that fits every engine's identifier limit,
// keeping the tail — where the distinguishing part is — when a long table
// prefix would overflow it.
func indexName(suffix string) string {
	name := "idx_" + suffix
	if len(name) <= maxIndexNameLength {
		return name
	}
	return name[len(name)-maxIndexNameLength:]
}

// ensureIndex creates an index when it is missing, in the way each engine
// allows.
//
// MySQL has no CREATE INDEX IF NOT EXISTS, and emitting it there is a SYNTAX
// error, not a no-op — which is how this store failed to open a MySQL database
// at all, since ensureSchema runs inside NewStore and its error reaches
// app.New (NU-85). The rest of the tree already knew: pkg/accounts asks
// information_schema and says so in a comment with the same scar, and
// pkg/auth skips the index on MySQL. This asks information_schema rather than
// swallowing the error, because swallowing "duplicate key name" also swallows
// every other reason a create can fail.
func (s *Store) ensureIndex(ctx context.Context, name, columns string) error {
	if s.flavor == FlavorMySQL {
		var count int
		query := `SELECT COUNT(*) FROM information_schema.statistics
			WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?`
		if err := s.db.QueryRowContext(ctx, query, s.table, name).Scan(&count); err != nil {
			return fmt.Errorf("outbox: look for index %s: %w", name, err)
		}
		if count > 0 {
			return nil
		}
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf("CREATE INDEX %s ON %s (%s)",
			s.quotedIdentifier(name), s.quotedTable(), columns)); err != nil {
			return fmt.Errorf("outbox: create index %s: %w", name, err)
		}
		return nil
	}

	stmt := fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s (%s)",
		s.quotedIdentifier(name), s.quotedTable(), columns)
	if _, err := s.db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("outbox: create index %s: %w", name, err)
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

// resolveFlavor maps the configuration to the SQL grammar the store
// emits. The outbox only knows sqlite, postgres and mysql; a dialect
// nucleus supports elsewhere (mssql, oracle) must fail here — at store
// construction — with an explicit error instead of producing invalid
// SQL (DDL types, placeholders, lease UPDATEs) at runtime (NU6-3).
// Unrecognized URLs keep the historical sqlite default: file paths and
// sqlite:// DSNs land there.
func resolveFlavor(cfg Config) (Flavor, error) {
	if cfg.Flavor != "" {
		switch cfg.Flavor {
		case FlavorSQLite, FlavorPostgres, FlavorMySQL:
			return cfg.Flavor, nil
		default:
			return "", fmt.Errorf("outbox: store supports sqlite/postgres/mysql; got %s", cfg.Flavor)
		}
	}
	raw := strings.ToLower(strings.TrimSpace(cfg.DatabaseURL))
	switch {
	case strings.HasPrefix(raw, "postgres://"), strings.HasPrefix(raw, "postgresql://"):
		return FlavorPostgres, nil
	case strings.HasPrefix(raw, "mysql://"):
		return FlavorMySQL, nil
	case strings.HasPrefix(raw, "mssql://"), strings.HasPrefix(raw, "sqlserver://"):
		return "", fmt.Errorf("outbox: store supports sqlite/postgres/mysql; got mssql")
	case strings.HasPrefix(raw, "oracle://"):
		return "", fmt.Errorf("outbox: store supports sqlite/postgres/mysql; got oracle")
	default:
		return FlavorSQLite, nil
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
