package quark

import (
	"fmt"
	"strings"
)

// Dialect defines the interface for database-specific SQL generation.
// Each supported database (PostgreSQL, MySQL, SQLite, etc.) implements this interface.
type Dialect interface {
	// Name returns the dialect name (e.g., "postgres", "mysql", "sqlite").
	Name() string

	// Placeholder returns the placeholder for the given parameter index.
	// PostgreSQL: $1, $2, etc.
	// MySQL/SQLite: ?
	// MSSQL: @p1, @p2, etc.
	// Oracle: :1, :2, etc.
	Placeholder(index int) string

	// Quote returns a quoted identifier (table/column name).
	// PostgreSQL: "identifier"
	// MySQL: `identifier`
	// MSSQL: [identifier]
	// SQLite/Oracle: "identifier"
	Quote(identifier string) string

	// Placeholders returns a slice of placeholders for n parameters.
	Placeholders(n int) []string

	// LimitOffset returns the LIMIT/OFFSET clause for the given parameters.
	LimitOffset(limit, offset int) string

	// SupportsReturning indicates if the dialect supports RETURNING clause.
	SupportsReturning() bool

	// Returning returns the RETURNING clause for the given columns.
	// Returns empty string if not supported.
	Returning(columns ...string) string

	// SupportsLastInsertID indicates if the dialect supports LastInsertId().
	SupportsLastInsertID() bool

	// LastInsertIDQuery returns the query to get the last insert ID.
	// Used for dialects that don't support RETURNING.
	LastInsertIDQuery(table, pkColumn string) string

	// CurrentTimestamp returns the SQL function for current timestamp.
	CurrentTimestamp() string

	// BuildRoutineQuery returns the SQL for a table-valued function or routine returning rows.
	// E.g., Postgres: SELECT * FROM func($1, $2)
	BuildRoutineQuery(routine string, argCount int) string

	// BuildProcedureCall returns the SQL for calling a procedure (pure logic / OUT params).
	// E.g., MySQL: CALL proc(?, ?)
	BuildProcedureCall(procedure string, argCount int) string
}

// baseDialect provides common functionality for all dialects.
type baseDialect struct {
	name string
}

func (d *baseDialect) Name() string {
	return d.name
}

// PostgresDialect implements the PostgreSQL dialect.
type PostgresDialect struct {
	baseDialect
}

// PostgreSQL returns the PostgreSQL dialect instance.
func PostgreSQL() Dialect {
	return &PostgresDialect{
		baseDialect{name: "postgres"},
	}
}

func (p *PostgresDialect) Placeholder(index int) string {
	return fmt.Sprintf("$%d", index)
}

func (p *PostgresDialect) Placeholders(n int) []string {
	placeholders := make([]string, n)
	for i := 0; i < n; i++ {
		placeholders[i] = p.Placeholder(i + 1)
	}
	return placeholders
}

func (p *PostgresDialect) Quote(identifier string) string {
	return fmt.Sprintf(`"%s"`, identifier)
}

func (p *PostgresDialect) LimitOffset(limit, offset int) string {
	if limit > 0 && offset > 0 {
		return fmt.Sprintf("LIMIT %d OFFSET %d", limit, offset)
	}
	if limit > 0 {
		return fmt.Sprintf("LIMIT %d", limit)
	}
	if offset > 0 {
		return fmt.Sprintf("OFFSET %d", offset)
	}
	return ""
}

func (p *PostgresDialect) SupportsReturning() bool {
	return true
}

func (p *PostgresDialect) Returning(columns ...string) string {
	if len(columns) == 0 {
		return ""
	}
	quoted := make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = p.Quote(col)
	}
	return "RETURNING " + strings.Join(quoted, ", ")
}

func (p *PostgresDialect) SupportsLastInsertID() bool {
	return false
}

func (p *PostgresDialect) LastInsertIDQuery(table, pkColumn string) string {
	return ""
}

func (p *PostgresDialect) CurrentTimestamp() string {
	return "CURRENT_TIMESTAMP"
}

func (p *PostgresDialect) BuildRoutineQuery(routine string, argCount int) string {
	placeholders := strings.Join(p.Placeholders(argCount), ", ")
	return fmt.Sprintf("SELECT * FROM %s(%s)", p.Quote(routine), placeholders)
}

func (p *PostgresDialect) BuildProcedureCall(procedure string, argCount int) string {
	placeholders := strings.Join(p.Placeholders(argCount), ", ")
	return fmt.Sprintf("CALL %s(%s)", p.Quote(procedure), placeholders)
}

// MySQLDialect implements the MySQL dialect.
type MySQLDialect struct {
	baseDialect
}

// MySQL returns the MySQL dialect instance.
func MySQL() Dialect {
	return &MySQLDialect{
		baseDialect{name: "mysql"},
	}
}

func (m *MySQLDialect) Placeholder(index int) string {
	return "?"
}

func (m *MySQLDialect) Placeholders(n int) []string {
	placeholders := make([]string, n)
	for i := 0; i < n; i++ {
		placeholders[i] = "?"
	}
	return placeholders
}

func (m *MySQLDialect) Quote(identifier string) string {
	return fmt.Sprintf("`%s`", identifier)
}

func (m *MySQLDialect) LimitOffset(limit, offset int) string {
	// MySQL uses LIMIT offset, count
	if limit > 0 && offset > 0 {
		return fmt.Sprintf("LIMIT %d, %d", offset, limit)
	}
	if limit > 0 {
		return fmt.Sprintf("LIMIT %d", limit)
	}
	return ""
}

func (m *MySQLDialect) SupportsReturning() bool {
	// MySQL 8.0.19+ supports RETURNING, but we'll use LastInsertId for compatibility
	return false
}

func (m *MySQLDialect) Returning(columns ...string) string {
	return ""
}

func (m *MySQLDialect) SupportsLastInsertID() bool {
	return true
}

func (m *MySQLDialect) LastInsertIDQuery(table, pkColumn string) string {
	return "SELECT LAST_INSERT_ID()"
}

func (m *MySQLDialect) CurrentTimestamp() string {
	return "CURRENT_TIMESTAMP"
}

func (m *MySQLDialect) BuildRoutineQuery(routine string, argCount int) string {
	placeholders := strings.Join(m.Placeholders(argCount), ", ")
	// MySQL uses CALL for everything, even if returning result sets
	return fmt.Sprintf("CALL %s(%s)", m.Quote(routine), placeholders)
}

func (m *MySQLDialect) BuildProcedureCall(procedure string, argCount int) string {
	placeholders := strings.Join(m.Placeholders(argCount), ", ")
	return fmt.Sprintf("CALL %s(%s)", m.Quote(procedure), placeholders)
}

// SQLiteDialect implements the SQLite dialect.
type SQLiteDialect struct {
	baseDialect
}

// SQLite returns the SQLite dialect instance.
func SQLite() Dialect {
	return &SQLiteDialect{
		baseDialect{name: "sqlite"},
	}
}

func (s *SQLiteDialect) Placeholder(index int) string {
	return "?"
}

func (s *SQLiteDialect) Placeholders(n int) []string {
	placeholders := make([]string, n)
	for i := 0; i < n; i++ {
		placeholders[i] = "?"
	}
	return placeholders
}

func (s *SQLiteDialect) Quote(identifier string) string {
	return fmt.Sprintf(`"%s"`, identifier)
}

func (s *SQLiteDialect) LimitOffset(limit, offset int) string {
	if limit > 0 && offset > 0 {
		return fmt.Sprintf("LIMIT %d OFFSET %d", limit, offset)
	}
	if limit > 0 {
		return fmt.Sprintf("LIMIT %d", limit)
	}
	if offset > 0 {
		return fmt.Sprintf("OFFSET %d", offset)
	}
	return ""
}

func (s *SQLiteDialect) SupportsReturning() bool {
	// SQLite 3.35.0+ supports RETURNING
	return true
}

func (s *SQLiteDialect) Returning(columns ...string) string {
	if len(columns) == 0 {
		return ""
	}
	quoted := make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = s.Quote(col)
	}
	return "RETURNING " + strings.Join(quoted, ", ")
}

func (s *SQLiteDialect) SupportsLastInsertID() bool {
	return true
}

func (s *SQLiteDialect) LastInsertIDQuery(table, pkColumn string) string {
	return "SELECT last_insert_rowid()"
}

func (s *SQLiteDialect) CurrentTimestamp() string {
	return "CURRENT_TIMESTAMP"
}

func (s *SQLiteDialect) BuildRoutineQuery(routine string, argCount int) string {
	placeholders := strings.Join(s.Placeholders(argCount), ", ")
	// SQLite has User-Defined Functions but not procedures, so we select it
	return fmt.Sprintf("SELECT * FROM %s(%s)", s.Quote(routine), placeholders)
}

func (s *SQLiteDialect) BuildProcedureCall(procedure string, argCount int) string {
	placeholders := strings.Join(s.Placeholders(argCount), ", ")
	// SQLite has no CALL, map to SELECT
	return fmt.Sprintf("SELECT %s(%s)", s.Quote(procedure), placeholders)
}

// DetectDialect attempts to auto-detect the dialect from a driver name.
func DetectDialect(driverName string) (Dialect, error) {
	switch driverName {
	case "postgres", "pgx", "pgx/v5", "pq":
		return PostgreSQL(), nil
	case "mysql", "mariadb":
		return MySQL(), nil
	case "sqlite", "sqlite3", "modernc":
		return SQLite(), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrDialectNotSupported, driverName)
	}
}
