package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	"sync/atomic"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/signals"
)

// defaultSQLObserver, when set, is invoked for every CRUD instance after
// any per-instance observer registered via SetSQLQueryObserver. It is
// process-wide and additive, intended for the observability bus to receive
// SQL events from every CRUD instance without each construction site
// having to remember to wire it up.
//
// The legacy admin panel still registers its own observer per-CRUD via
// SetSQLQueryObserver; both fire. The default observer is set in pkg/app
// from the observability hooks adapter.
var defaultSQLObserver atomic.Pointer[SQLQueryObserver]

// SetDefaultSQLObserver installs (or clears, if obs is nil) the
// process-wide default SQL observer. It is safe for concurrent use and
// intended to be called once during application bootstrap.
//
// Calls after CRUD instances have been constructed are honoured: the
// observer is read on each emit, not captured at construction.
func SetDefaultSQLObserver(obs SQLQueryObserver) {
	if obs == nil {
		defaultSQLObserver.Store(nil)
		return
	}
	defaultSQLObserver.Store(&obs)
}

// QueryOpts controls filtering, searching, sorting, and pagination.
type QueryOpts struct {
	Page     int               // 1-based page number (default: 1)
	PageSize int               // Items per page (default: from ModelConfig)
	Search   string            // Free-text search across SearchFields
	Filters  map[string]string // Exact-match filters: column -> value
	OrderBy  string            // Sort clauses: comma-separated "<column> [asc|desc]" (e.g. "created_at desc, name asc"). Each column must be a known model column; invalid input is rejected with an error (not raw SQL).
	Fields   []string          // SELECT specific columns (empty = all)
}

// PaginatedResult wraps a paginated query response.
type PaginatedResult struct {
	Items       interface{} `json:"items"`
	Total       int64       `json:"total"`
	Page        int         `json:"page"`
	PageSize    int         `json:"page_size"`
	TotalPages  int         `json:"total_pages"`
	IsEstimated bool        `json:"is_estimated"` // Whether the Total count is an estimate
	HasMore     bool        `json:"has_more"`     // Whether there are more pages (useful for infinite scroll)
}

// CRUD provides generic create/read/update/delete operations for a registered model.
// It uses reflection to create model instances dynamically so the admin panel can
// operate on any registered model without compile-time type knowledge.
type CRUD struct {
	db          *sql.DB
	meta        *ModelMeta
	bus         *signals.Bus
	sqlObserver SQLQueryObserver
	dialect     string // Database dialect (sqlite, postgres, mysql, sqlserver/mssql, oracle); drives placeholder rebind
}

// SQLQueryEvent represents one SQL operation executed by CRUD.
// Values in Args are raw runtime arguments from the query execution call site.
type SQLQueryEvent struct {
	ModelName string
	Operation string
	Query     string
	Args      []interface{}
	Duration  time.Duration
	Error     error
	// RowsAffected is the driver-reported row count for exec-style
	// operations (INSERT/UPDATE/DELETE). 0 means "not reported": SELECT
	// paths cannot know it without consuming the rows, and some drivers
	// do not support it.
	RowsAffected int64
}

// SQLQueryObserver receives SQLQueryEvent notifications emitted by CRUD operations.
// It is optional and disabled by default.
type SQLQueryObserver func(ctx context.Context, event SQLQueryEvent)

// NewCRUD creates a CRUD operator for the given model metadata.
// The signals bus is optional (pass nil to disable signal emission).
func NewCRUD(db *sql.DB, meta *ModelMeta, bus *signals.Bus) *CRUD {
	return &CRUD{db: db, meta: meta, bus: bus}
}

// SetDialect sets the database dialect for this CRUD instance. The value drives
// per-engine placeholder rebinding (see rebind) and the getEstimate count
// queries, so it is normalised to a single canonical token. The codebase has
// two dialect-naming conventions — some callers pass "postgres"/"sqlserver"
// while db.DB.System() emits "postgresql"/"mssql" — and callers pass
// either; this collapses both to the canonical form so neither convention slips
// through as an unrebound `?` (F-3, ADR-013).
func (c *CRUD) SetDialect(dialect string) {
	d := strings.ToLower(strings.TrimSpace(dialect))
	switch d {
	case "postgresql":
		d = "postgres"
	case "sqlserver":
		d = "mssql"
	}
	c.dialect = d
}

// SetSQLQueryObserver registers a SQL observer for this CRUD instance.
// Passing nil disables SQL observation.
func (c *CRUD) SetSQLQueryObserver(observer SQLQueryObserver) {
	c.sqlObserver = observer
}

// FindAll retrieves a paginated, searchable, filterable list of records.
// It uses an "estimate first" strategy for performance and supports infinite scroll
// by fetching one extra record to detect if more data exists.
func (c *CRUD) FindAll(ctx context.Context, opts QueryOpts) (*PaginatedResult, error) {
	if opts.Page < 1 {
		opts.Page = 1
	}
	if opts.PageSize < 1 {
		opts.PageSize = c.meta.Config.PageSize
	}
	if opts.PageSize < 1 {
		opts.PageSize = 25
	}

	whereExpr, whereArgs := c.buildWhere(opts)

	var total int64
	var estimated bool

	// Get estimated count if no filters are applied, otherwise we might need a real count
	// depending on how much we care about the "total" indicator.
	// The user requested to avoid COUNT(*) for performance.
	if whereExpr == "" {
		total, estimated = c.getEstimate(ctx)
	} else {
		// With filters, we could still estimate or just return -1 (unknown)
		// For now, let's return -1 to signal that total is unknown/expensive
		total = -1
		estimated = true
	}

	columns := c.selectedColumns(opts.Fields)
	if len(columns) == 0 {
		return nil, fmt.Errorf("model.CRUD.FindAll model=%s: no valid columns selected", c.meta.Name)
	}

	// ORDER BY (SEC): opts.OrderBy is caller-supplied — admin-panel/data-studio
	// request fields flow straight into it — and was previously concatenated raw
	// into the query string, an SQL-injection vector. Validate it against the
	// model's known columns and a fixed asc/desc set, rebuilding the clause from
	// allow-listed tokens only. The developer-set meta.Config.OrderBy default is
	// trusted and used verbatim.
	orderBy, err := c.sanitizeOrderBy(opts.OrderBy)
	if err != nil {
		return nil, fmt.Errorf("model.CRUD.FindAll model=%s: %w", c.meta.Name, err)
	}
	if orderBy == "" {
		orderBy = strings.TrimSpace(c.meta.Config.OrderBy)
	}
	if orderBy == "" {
		orderBy = c.primaryColumn() + " desc"
	}

	offset := (opts.Page - 1) * opts.PageSize
	querySQL := fmt.Sprintf("SELECT %s FROM %s", strings.Join(columns, ", "), c.meta.Table)
	if whereExpr != "" {
		querySQL += " WHERE " + whereExpr
	}
	// Fetch PageSize + 1 to detect HasMore for infinite scroll
	querySQL += " ORDER BY " + orderBy + " LIMIT ? OFFSET ?"

	args := make([]interface{}, 0, len(whereArgs)+2)
	args = append(args, whereArgs...)
	args = append(args, opts.PageSize+1, offset)

	rows, err := c.queryContext(ctx, "select.list", querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("model.CRUD.FindAll model=%s query: %w", c.meta.Name, err)
	}
	defer rows.Close()

	items := reflect.MakeSlice(reflect.SliceOf(c.meta.Type), 0, opts.PageSize+1)
	count := 0
	for rows.Next() {
		entityPtr := reflect.New(c.meta.Type)
		if err := c.scanRowIntoEntity(rows, columns, entityPtr.Elem()); err != nil {
			return nil, fmt.Errorf("model.CRUD.FindAll model=%s scan: %w", c.meta.Name, err)
		}
		items = reflect.Append(items, entityPtr.Elem())
		count++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("model.CRUD.FindAll model=%s rows: %w", c.meta.Name, err)
	}

	hasMore := count > opts.PageSize
	finalItems := items
	if hasMore {
		finalItems = items.Slice(0, opts.PageSize)
	}

	totalPages := 0
	if total > 0 {
		totalPages = int(math.Ceil(float64(total) / float64(opts.PageSize)))
	} else if hasMore {
		// If total is unknown but we have more, we at least have 2 pages
		totalPages = opts.Page + 1
	} else {
		totalPages = opts.Page
	}

	return &PaginatedResult{
		Items:       finalItems.Interface(),
		Total:       total,
		Page:        opts.Page,
		PageSize:    opts.PageSize,
		TotalPages:  totalPages,
		IsEstimated: estimated,
		HasMore:     hasMore,
	}, nil
}

func (c *CRUD) getEstimate(ctx context.Context) (int64, bool) {
	var total int64
	var err error

	// These count queries bypass exec/queryContext (direct QueryRowContext), so
	// they must be rebound here too (F-3). c.rebind keys on c.dialect, which
	// matches each case, so postgres/mssql/oracle get $1/@p1/:1 and mysql/sqlite
	// pass through unchanged.
	switch c.dialect {
	case "postgres":
		err = c.db.QueryRowContext(ctx, c.rebind("SELECT reltuples::bigint FROM pg_class WHERE relname = ?"), c.meta.Table).Scan(&total)
	case "mysql":
		err = c.db.QueryRowContext(ctx, c.rebind("SELECT TABLE_ROWS FROM information_schema.tables WHERE table_name = ? AND table_schema = DATABASE()"), c.meta.Table).Scan(&total)
	case "sqlite", "sqlite3":
		err = c.db.QueryRowContext(ctx, c.rebind("SELECT n FROM sqlite_stat1 WHERE tbl = ? LIMIT 1"), c.meta.Table).Scan(&total)
	case "sqlserver", "mssql":
		err = c.db.QueryRowContext(ctx, c.rebind("SELECT SUM(rows) FROM sys.partitions WHERE object_id = OBJECT_ID(?) AND index_id IN (0, 1)"), c.meta.Table).Scan(&total)
	case "oracle":
		err = c.db.QueryRowContext(ctx, c.rebind("SELECT NUM_ROWS FROM ALL_TABLES WHERE TABLE_NAME = UPPER(?)"), c.meta.Table).Scan(&total)
	}

	if err != nil || total < 0 {
		return -1, true
	}
	return total, true
}

// FindByID retrieves a single record by primary key.
func (c *CRUD) FindByID(ctx context.Context, id interface{}) (interface{}, error) {
	columns := c.selectedColumns(nil)
	querySQL := fmt.Sprintf("SELECT %s FROM %s WHERE %s = ?", strings.Join(columns, ", "), c.meta.Table, c.primaryColumn())
	args := []interface{}{id}
	if c.hasDeletedAt() {
		querySQL += " AND deleted_at IS NULL"
	}
	querySQL += " LIMIT 1"

	rows, err := c.queryContext(ctx, "select.one", querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("model.CRUD.FindByID model=%s id=%v query: %w", c.meta.Name, id, err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, gferrors.NotFound(c.meta.Name, fmt.Sprintf("%v", id))
	}

	entityPtr := reflect.New(c.meta.Type)
	if err := c.scanRowIntoEntity(rows, columns, entityPtr.Elem()); err != nil {
		return nil, fmt.Errorf("model.CRUD.FindByID model=%s id=%v scan: %w", c.meta.Name, id, err)
	}
	return entityPtr.Interface(), nil
}

// Create inserts a new record. Emits PreCreate and PostCreate signals.
func (c *CRUD) Create(ctx context.Context, entity interface{}) error {
	if c.bus != nil {
		if err := c.bus.Emit(signals.Event{
			Signal: signals.PreCreate, ModelName: c.meta.Name, Payload: entity, Ctx: ctx,
		}); err != nil {
			return err
		}
	}

	if c.meta.Config.BeforeCreate != nil {
		if err := c.meta.Config.BeforeCreate(newSQLHookContext(ctx, c.db, nil), entity); err != nil {
			return fmt.Errorf("model.CRUD.Create BeforeCreate model=%s: %w", c.meta.Name, err)
		}
	}

	now := time.Now().UTC()
	c.setTimeIfZero(entity, "CreatedAt", now)
	c.setTime(entity, "UpdatedAt", now)

	columns, args := c.insertColumnsAndArgs(entity)
	if len(columns) == 0 {
		return fmt.Errorf("model.CRUD.Create model=%s: no insertable columns", c.meta.Name)
	}

	querySQL := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		c.meta.Table,
		strings.Join(columns, ", "),
		placeholders(len(columns)),
	)

	res, err := c.execContext(ctx, "insert", querySQL, args...)
	if err != nil {
		return fmt.Errorf("model.CRUD.Create model=%s: %w", c.meta.Name, err)
	}

	c.setLastInsertID(entity, res)

	if c.meta.Config.AfterCreate != nil {
		if err := c.meta.Config.AfterCreate(newSQLHookContext(ctx, c.db, nil), entity); err != nil {
			return fmt.Errorf("model.CRUD.Create AfterCreate model=%s: %w", c.meta.Name, err)
		}
	}

	if c.bus != nil {
		c.bus.EmitAsync(signals.Event{
			Signal: signals.PostCreate, ModelName: c.meta.Name, Payload: entity, Ctx: ctx,
		})
	}

	return nil
}

// Update modifies an existing record by primary key. Emits PreUpdate and PostUpdate signals.
func (c *CRUD) Update(ctx context.Context, id interface{}, updates map[string]interface{}) error {
	if c.bus != nil {
		if err := c.bus.Emit(signals.Event{
			Signal: signals.PreUpdate, ModelName: c.meta.Name, Payload: updates, Ctx: ctx,
		}); err != nil {
			return err
		}
	}

	if c.meta.Config.BeforeUpdate != nil {
		if err := c.meta.Config.BeforeUpdate(newSQLHookContext(ctx, c.db, nil), updates); err != nil {
			return fmt.Errorf("model.CRUD.Update BeforeUpdate model=%s: %w", c.meta.Name, err)
		}
	}

	setParts := make([]string, 0, len(updates)+1)
	args := make([]interface{}, 0, len(updates)+2)
	for key, val := range updates {
		col := c.resolveColumn(key)
		if col == "" || !c.isValidColumn(col) {
			continue
		}
		setParts = append(setParts, fmt.Sprintf("%s = ?", col))
		args = append(args, val)
	}
	if len(setParts) == 0 {
		return gferrors.BadRequest("no valid columns provided")
	}

	if c.hasColumn("updated_at") {
		setParts = append(setParts, "updated_at = ?")
		args = append(args, time.Now().UTC())
	}

	querySQL := fmt.Sprintf("UPDATE %s SET %s WHERE %s = ?", c.meta.Table, strings.Join(setParts, ", "), c.primaryColumn())
	args = append(args, id)
	if c.hasDeletedAt() {
		querySQL += " AND deleted_at IS NULL"
	}

	res, err := c.execContext(ctx, "update", querySQL, args...)
	if err != nil {
		return fmt.Errorf("model.CRUD.Update model=%s id=%v: %w", c.meta.Name, id, err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return gferrors.NotFound(c.meta.Name, fmt.Sprintf("%v", id))
	}

	if c.meta.Config.AfterUpdate != nil {
		if err := c.meta.Config.AfterUpdate(newSQLHookContext(ctx, c.db, nil), updates); err != nil {
			return fmt.Errorf("model.CRUD.Update AfterUpdate model=%s: %w", c.meta.Name, err)
		}
	}

	if c.bus != nil {
		c.bus.EmitAsync(signals.Event{
			Signal: signals.PostUpdate, ModelName: c.meta.Name, Payload: updates, Ctx: ctx,
		})
	}

	return nil
}

// Delete removes a record by primary key. If the model has a DeletedAt field,
// performs a soft delete; otherwise a hard delete. Emits PreDelete and PostDelete signals.
func (c *CRUD) Delete(ctx context.Context, id interface{}) error {
	if c.bus != nil {
		if err := c.bus.Emit(signals.Event{
			Signal: signals.PreDelete, ModelName: c.meta.Name, Payload: id, Ctx: ctx,
		}); err != nil {
			return err
		}
	}

	if c.meta.Config.BeforeDelete != nil {
		if err := c.meta.Config.BeforeDelete(newSQLHookContext(ctx, c.db, nil), id); err != nil {
			return fmt.Errorf("model.CRUD.Delete BeforeDelete model=%s: %w", c.meta.Name, err)
		}
	}

	var (
		querySQL string
		args     []interface{}
	)

	if c.hasDeletedAt() {
		querySQL = fmt.Sprintf("UPDATE %s SET deleted_at = ? WHERE %s = ? AND deleted_at IS NULL", c.meta.Table, c.primaryColumn())
		args = []interface{}{time.Now().UTC(), id}
	} else {
		querySQL = fmt.Sprintf("DELETE FROM %s WHERE %s = ?", c.meta.Table, c.primaryColumn())
		args = []interface{}{id}
	}

	op := "delete"
	if c.hasDeletedAt() {
		op = "soft_delete"
	}
	res, err := c.execContext(ctx, op, querySQL, args...)
	if err != nil {
		return fmt.Errorf("model.CRUD.Delete model=%s id=%v: %w", c.meta.Name, id, err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return gferrors.NotFound(c.meta.Name, fmt.Sprintf("%v", id))
	}

	if c.bus != nil {
		c.bus.EmitAsync(signals.Event{
			Signal: signals.PostDelete, ModelName: c.meta.Name, Payload: id, Ctx: ctx,
		})
	}

	return nil
}

func (c *CRUD) buildWhere(opts QueryOpts) (string, []interface{}) {
	clauses := make([]string, 0, len(opts.Filters)+2)
	args := make([]interface{}, 0, len(opts.Filters)+4)

	if strings.TrimSpace(opts.Search) != "" {
		searchFields := c.searchFields()
		if len(searchFields) > 0 {
			pattern := "%" + strings.ToLower(strings.TrimSpace(opts.Search)) + "%"
			sub := make([]string, 0, len(searchFields))
			for _, col := range searchFields {
				sub = append(sub, fmt.Sprintf("LOWER(%s) LIKE ?", col))
				args = append(args, pattern)
			}
			clauses = append(clauses, "("+strings.Join(sub, " OR ")+")")
		}
	}

	for col, val := range opts.Filters {
		resolved := c.resolveColumn(col)
		if resolved == "" || !c.isValidColumn(resolved) {
			continue
		}
		clauses = append(clauses, fmt.Sprintf("%s = ?", resolved))
		args = append(args, c.normalizeFilterValue(resolved, val))
	}

	if c.hasDeletedAt() {
		clauses = append(clauses, "deleted_at IS NULL")
	}

	return strings.Join(clauses, " AND "), args
}

func (c *CRUD) selectedColumns(requested []string) []string {
	if len(requested) == 0 {
		out := make([]string, 0, len(c.meta.Fields))
		for _, f := range c.meta.Fields {
			out = append(out, c.normalizeColumn(f.Column))
		}
		return dedupeStrings(out)
	}

	out := make([]string, 0, len(requested))
	for _, raw := range requested {
		col := c.resolveColumn(raw)
		if col == "" || !c.isValidColumn(col) {
			continue
		}
		out = append(out, col)
	}
	return dedupeStrings(out)
}

func (c *CRUD) scanRowIntoEntity(rows *sql.Rows, columns []string, entity reflect.Value) error {
	values := make([]interface{}, len(columns))
	dest := make([]interface{}, len(columns))
	for i := range values {
		dest[i] = &values[i]
	}
	if err := rows.Scan(dest...); err != nil {
		return err
	}

	fieldMap := c.fieldByColumn()
	for i, col := range columns {
		meta, ok := fieldMap[c.normalizeColumn(col)]
		if !ok {
			continue
		}
		field := entity.FieldByName(meta.Name)
		if !field.IsValid() || !field.CanSet() {
			continue
		}
		if err := assignDBValue(field, values[i]); err != nil {
			return fmt.Errorf("column %s: %w", col, err)
		}
	}
	return nil
}

func (c *CRUD) insertColumnsAndArgs(entity interface{}) ([]string, []interface{}) {
	entityVal := reflect.ValueOf(entity)
	if entityVal.Kind() == reflect.Ptr {
		entityVal = entityVal.Elem()
	}

	columns := make([]string, 0, len(c.meta.Fields))
	args := make([]interface{}, 0, len(c.meta.Fields))
	for _, f := range c.meta.Fields {
		if f.IsPK {
			continue
		}
		field := entityVal.FieldByName(f.Name)
		if !field.IsValid() {
			continue
		}
		columns = append(columns, c.normalizeColumn(f.Column))
		args = append(args, valueForSQL(field))
	}
	return columns, args
}

func (c *CRUD) setLastInsertID(entity interface{}, res sql.Result) {
	if res == nil {
		return
	}
	id, err := res.LastInsertId()
	if err != nil || id <= 0 {
		return
	}

	entityVal := reflect.ValueOf(entity)
	if entityVal.Kind() != reflect.Ptr || entityVal.IsNil() {
		return
	}
	entityVal = entityVal.Elem()
	pkName := c.meta.PrimaryKey
	if pkName == "" {
		pkName = "ID"
	}
	pk := entityVal.FieldByName(pkName)
	if !pk.IsValid() || !pk.CanSet() {
		return
	}

	switch pk.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		pk.SetInt(id)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		pk.SetUint(uint64(id))
	}
}

func (c *CRUD) normalizeFilterValue(column, raw string) interface{} {
	fieldMap := c.fieldByColumn()
	field, ok := fieldMap[c.normalizeColumn(column)]
	if !ok {
		return raw
	}

	switch field.GoType {
	case "bool":
		lower := strings.ToLower(strings.TrimSpace(raw))
		return lower == "1" || lower == "true" || lower == "yes" || lower == "on"
	case "int", "int8", "int16", "int32", "int64":
		if v, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err == nil {
			return v
		}
	case "uint", "uint8", "uint16", "uint32", "uint64":
		if v, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64); err == nil {
			return v
		}
	case "float32", "float64":
		if v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64); err == nil {
			return v
		}
	}
	return raw
}

func (c *CRUD) searchFields() []string {
	var cols []string
	for _, f := range c.meta.Fields {
		if f.IsSearch {
			cols = append(cols, c.normalizeColumn(f.Column))
		}
	}
	return dedupeStrings(cols)
}

func (c *CRUD) primaryColumn() string {
	for _, f := range c.meta.Fields {
		if f.IsPK {
			return c.normalizeColumn(f.Column)
		}
	}
	if c.meta.PrimaryKey != "" {
		if col := c.resolveColumn(c.meta.PrimaryKey); col != "" {
			return col
		}
	}
	return "id"
}

func (c *CRUD) resolveColumn(raw string) string {
	key := strings.TrimSpace(raw)
	if key == "" {
		return ""
	}
	for _, f := range c.meta.Fields {
		if strings.EqualFold(f.Column, key) || strings.EqualFold(f.Name, key) {
			return c.normalizeColumn(f.Column)
		}
	}
	if strings.EqualFold(key, "id") {
		return "id"
	}
	return ""
}

func (c *CRUD) normalizeColumn(col string) string {
	return normalizeOrderColumn(col)
}

func (c *CRUD) fieldByColumn() map[string]FieldMeta {
	out := make(map[string]FieldMeta, len(c.meta.Fields))
	for _, f := range c.meta.Fields {
		out[c.normalizeColumn(f.Column)] = f
		if strings.EqualFold(f.Name, "ID") {
			out["id"] = f
		}
	}
	return out
}

func (c *CRUD) isValidColumn(col string) bool {
	return c.resolveColumn(col) != ""
}

// sanitizeOrderBy validates a caller-supplied ORDER BY clause and rebuilds it
// from allow-listed tokens, neutralising SQL injection (SEC). It accepts a
// comma-separated list of `<column> [asc|desc]` clauses: each column must
// resolve to a known model column via resolveColumn, and the direction (if
// present) must be exactly asc/desc (case-insensitive). The returned clause
// contains only resolved column names + a canonical direction, so nothing
// attacker-controlled reaches the query string. An empty input returns ""
// (the caller then falls back to the trusted default). Any unresolved column,
// bad direction, or malformed clause is a hard error rather than being silently
// dropped, so an injection attempt fails loud instead of degrading to a default.
func (c *CRUD) sanitizeOrderBy(raw string) (string, error) {
	return SanitizeOrderBy(c.meta, raw)
}

// SanitizeOrderBy validates a user-supplied ORDER BY expression against a
// model's known columns and returns a safe "col dir[, col dir ...]" clause.
// It is the single order-by allow-list shared by the CRUD layer and the
// admin API (audit LOW-B), so the two cannot drift — it is the
// SQL-injection barrier for ordering, not quoting (see ADR-011). Column
// keys match a field's storage column or Go name (case-insensitive); the
// synthetic primary key "id" is always accepted. Empty input yields an
// empty clause and no error. Unknown columns and bad directions are
// rejected.
func SanitizeOrderBy(meta *ModelMeta, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if meta == nil {
		return "", fmt.Errorf("order-by: nil model metadata")
	}
	clauses := strings.Split(raw, ",")
	out := make([]string, 0, len(clauses))
	for _, clause := range clauses {
		fields := strings.Fields(clause)
		if len(fields) == 0 {
			// Empty clause (e.g. a trailing/double comma) — fail loud rather
			// than silently dropping it, matching the rest of this validator.
			return "", fmt.Errorf("invalid order-by clause: empty clause in %q", raw)
		}
		if len(fields) > 2 {
			return "", fmt.Errorf("invalid order-by clause %q", strings.TrimSpace(clause))
		}
		col, ok := resolveOrderColumn(meta, fields[0])
		if !ok {
			return "", fmt.Errorf("invalid order-by column %q", fields[0])
		}
		dir := "asc"
		if len(fields) == 2 {
			switch strings.ToLower(fields[1]) {
			case "asc":
				dir = "asc"
			case "desc":
				dir = "desc"
			default:
				return "", fmt.Errorf("invalid order-by direction %q", fields[1])
			}
		}
		out = append(out, col+" "+dir)
	}
	return strings.Join(out, ", "), nil
}

// resolveOrderColumn maps an order-by key to its safe storage column,
// matching against each field's column or Go name (case-insensitive) plus
// the synthetic "id". Returns ok=false for an unknown key.
func resolveOrderColumn(meta *ModelMeta, key string) (string, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", false
	}
	for _, f := range meta.Fields {
		col := normalizeOrderColumn(f.Column)
		if strings.EqualFold(key, col) || strings.EqualFold(key, f.Column) || strings.EqualFold(key, f.Name) {
			return col, true
		}
	}
	if strings.EqualFold(key, "id") {
		return "id", true
	}
	return "", false
}

// normalizeOrderColumn canonicalises a storage column for ordering. The
// only special case is the reflect-derived "i_d" → "id".
func normalizeOrderColumn(col string) string {
	if strings.EqualFold(col, "i_d") {
		return "id"
	}
	return col
}

func (c *CRUD) hasDeletedAt() bool {
	return c.hasColumn("deleted_at")
}

func (c *CRUD) hasColumn(col string) bool {
	for _, f := range c.meta.Fields {
		if c.normalizeColumn(f.Column) == col {
			return true
		}
	}
	return false
}

func (c *CRUD) setTimeIfZero(entity interface{}, fieldName string, value time.Time) {
	v := reflect.ValueOf(entity)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return
	}
	field := v.Elem().FieldByName(fieldName)
	if !field.IsValid() || !field.CanSet() {
		return
	}
	if field.Type().PkgPath() == "time" && field.Type().Name() == "Time" && field.IsZero() {
		field.Set(reflect.ValueOf(value))
	}
}

func (c *CRUD) setTime(entity interface{}, fieldName string, value time.Time) {
	v := reflect.ValueOf(entity)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return
	}
	field := v.Elem().FieldByName(fieldName)
	if !field.IsValid() || !field.CanSet() {
		return
	}
	if field.Type().PkgPath() == "time" && field.Type().Name() == "Time" {
		field.Set(reflect.ValueOf(value))
	}
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ", ")
}

// rebind converts the `?` positional parameter markers that the CRUD layer
// emits into the placeholder syntax the target engine requires (F-3, ADR-013):
//
//   - postgres            → $1, $2, …
//   - sqlserver / mssql   → @p1, @p2, …
//   - oracle              → :1, :2, …
//   - mysql / sqlite / "" → unchanged (`?` is native)
//
// The CRUD layer builds every statement from trusted identifiers plus `?`
// parameter markers and never emits a string literal containing a `?`, so a
// positional scan that rewrites each `?` in order is exact. Centralising the
// rewrite in execContext/queryContext (and the getEstimate count queries) keeps
// every CRUD path portable without each call site knowing the dialect.
func (c *CRUD) rebind(query string) string {
	// SetDialect normalises "sqlserver"→"mssql", so via the public setter only
	// "mssql" reaches here; "sqlserver" is kept as a guard for callers that set
	// the dialect field directly (e.g. white-box tests).
	switch c.dialect {
	case "postgres":
		return rebindNumbered(query, "$")
	case "sqlserver", "mssql":
		return rebindNumbered(query, "@p")
	case "oracle":
		return rebindNumbered(query, ":")
	default: // mysql, sqlite, sqlite3, unset — native `?`
		return query
	}
}

// rebindNumbered replaces each `?` in query with prefix + a 1-based ordinal
// (e.g. "$"→`$1,$2`; "@p"→`@p1,@p2`; ":"→`:1,:2`).
func rebindNumbered(query, prefix string) string {
	count := strings.Count(query, "?")
	if count == 0 {
		return query
	}
	var b strings.Builder
	// Each `?` (1 byte) becomes prefix + an ordinal; size for up to 2-digit
	// ordinals so multi-column INSERTs never reallocate.
	b.Grow(len(query) + count*(len(prefix)+2))
	n := 1
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			b.WriteString(prefix)
			b.WriteString(strconv.Itoa(n))
			n++
			continue
		}
		b.WriteByte(query[i])
	}
	return b.String()
}

func (c *CRUD) execContext(ctx context.Context, operation, query string, args ...interface{}) (sql.Result, error) {
	query = c.rebind(query)
	started := time.Now()
	res, err := c.db.ExecContext(ctx, query, args...)
	var rows int64
	if err == nil && res != nil {
		// Best-effort: drivers without RowsAffected support report 0
		// ("not reported"), never an error to the caller.
		if n, raErr := res.RowsAffected(); raErr == nil {
			rows = n
		}
	}
	c.observeSQL(ctx, operation, query, args, started, err, rows)
	return res, err
}

func (c *CRUD) queryContext(ctx context.Context, operation, query string, args ...interface{}) (*sql.Rows, error) {
	query = c.rebind(query)
	started := time.Now()
	rows, err := c.db.QueryContext(ctx, query, args...)
	c.observeSQL(ctx, operation, query, args, started, err, 0)
	return rows, err
}

func (c *CRUD) observeSQL(ctx context.Context, operation, query string, args []interface{}, started time.Time, err error, rowsAffected int64) {
	if c == nil {
		return
	}
	defObs := defaultSQLObserver.Load()
	if c.sqlObserver == nil && defObs == nil {
		return
	}
	argsCopy := make([]interface{}, len(args))
	copy(argsCopy, args)

	event := SQLQueryEvent{
		ModelName:    c.meta.Name,
		Operation:    strings.TrimSpace(operation),
		Query:        query,
		Args:         argsCopy,
		Duration:     time.Since(started),
		Error:        err,
		RowsAffected: rowsAffected,
	}
	if c.sqlObserver != nil {
		c.sqlObserver(ctx, event)
	}
	if defObs != nil {
		(*defObs)(ctx, event)
	}
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		key := strings.TrimSpace(v)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func valueForSQL(v reflect.Value) interface{} {
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil
		}
		return valueForSQL(v.Elem())
	}
	return v.Interface()
}

func assignDBValue(field reflect.Value, raw interface{}) error {
	if !field.CanSet() {
		return nil
	}

	if raw == nil {
		field.Set(reflect.Zero(field.Type()))
		return nil
	}

	if field.Kind() == reflect.Ptr {
		ptr := reflect.New(field.Type().Elem())
		if err := assignDBValue(ptr.Elem(), raw); err != nil {
			return err
		}
		field.Set(ptr)
		return nil
	}

	if field.Type().PkgPath() == "time" && field.Type().Name() == "Time" {
		ts, err := parseDBTime(raw)
		if err != nil {
			return err
		}
		field.Set(reflect.ValueOf(ts))
		return nil
	}

	switch field.Kind() {
	case reflect.String:
		switch v := raw.(type) {
		case string:
			field.SetString(v)
		case []byte:
			field.SetString(string(v))
		default:
			field.SetString(fmt.Sprintf("%v", raw))
		}
		return nil

	case reflect.Bool:
		switch v := raw.(type) {
		case bool:
			field.SetBool(v)
		case int64:
			field.SetBool(v != 0)
		case int:
			field.SetBool(v != 0)
		case uint64:
			field.SetBool(v != 0)
		case []byte:
			s := strings.ToLower(strings.TrimSpace(string(v)))
			field.SetBool(s == "1" || s == "true" || s == "yes" || s == "on")
		case string:
			s := strings.ToLower(strings.TrimSpace(v))
			field.SetBool(s == "1" || s == "true" || s == "yes" || s == "on")
		default:
			s := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", raw)))
			field.SetBool(s == "1" || s == "true" || s == "yes" || s == "on")
		}
		return nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v, err := toInt64(raw)
		if err != nil {
			return err
		}
		field.SetInt(v)
		return nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v, err := toUint64(raw)
		if err != nil {
			return err
		}
		field.SetUint(v)
		return nil

	case reflect.Float32, reflect.Float64:
		v, err := toFloat64(raw)
		if err != nil {
			return err
		}
		field.SetFloat(v)
		return nil
	}

	val := reflect.ValueOf(raw)
	if val.Type().AssignableTo(field.Type()) {
		field.Set(val)
		return nil
	}
	if val.Type().ConvertibleTo(field.Type()) {
		field.Set(val.Convert(field.Type()))
		return nil
	}
	return fmt.Errorf("unsupported conversion from %T to %s", raw, field.Type())
}

func parseDBTime(raw interface{}) (time.Time, error) {
	switch v := raw.(type) {
	case time.Time:
		return v, nil
	case string:
		return parseTimeString(v)
	case []byte:
		return parseTimeString(string(v))
	default:
		return time.Time{}, fmt.Errorf("unsupported time value type %T", raw)
	}
}

func parseTimeString(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if ts, err := time.Parse(layout, value); err == nil {
			return ts, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid time value %q", raw)
}

func toInt64(raw interface{}) (int64, error) {
	switch v := raw.(type) {
	case int64:
		return v, nil
	case int:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case uint64:
		return int64(v), nil
	case uint:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint8:
		return int64(v), nil
	case float64:
		return int64(v), nil
	case float32:
		return int64(v), nil
	case []byte:
		return strconv.ParseInt(strings.TrimSpace(string(v)), 10, 64)
	case string:
		return strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	default:
		return 0, errors.New("invalid integer value")
	}
}

func toUint64(raw interface{}) (uint64, error) {
	switch v := raw.(type) {
	case uint64:
		return v, nil
	case uint:
		return uint64(v), nil
	case uint32:
		return uint64(v), nil
	case uint16:
		return uint64(v), nil
	case uint8:
		return uint64(v), nil
	case int64:
		if v < 0 {
			return 0, errors.New("negative integer")
		}
		return uint64(v), nil
	case int:
		if v < 0 {
			return 0, errors.New("negative integer")
		}
		return uint64(v), nil
	case float64:
		if v < 0 {
			return 0, errors.New("negative float")
		}
		return uint64(v), nil
	case float32:
		if v < 0 {
			return 0, errors.New("negative float")
		}
		return uint64(v), nil
	case []byte:
		return strconv.ParseUint(strings.TrimSpace(string(v)), 10, 64)
	case string:
		return strconv.ParseUint(strings.TrimSpace(v), 10, 64)
	default:
		return 0, errors.New("invalid unsigned integer value")
	}
}

func toFloat64(raw interface{}) (float64, error) {
	switch v := raw.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case int:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	case uint:
		return float64(v), nil
	case []byte:
		return strconv.ParseFloat(strings.TrimSpace(string(v)), 64)
	case string:
		return strconv.ParseFloat(strings.TrimSpace(v), 64)
	default:
		return 0, errors.New("invalid float value")
	}
}
