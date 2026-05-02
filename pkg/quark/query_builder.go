package quark

import "context"

// condition represents a WHERE clause condition.
type condition struct {
	column   string
	operator string
	value    any
	logic    string      // "AND" or "OR" (default "AND")
	group    []condition // sub-conditions for grouping
}

// order represents an ORDER BY clause.
type order struct {
	column string
	desc   bool
}

// join represents a JOIN clause.
type join struct {
	joinType string // "INNER JOIN", "LEFT JOIN", "RIGHT JOIN"
	table    string
	onClause string
	args     []any
}

// Query represents a type-safe database query builder for model T.
// All builder methods return a new Query (immutable/clone pattern) for thread-safety.
// Execution methods are in query_exec.go and query_crud.go
type Query[T any] struct {
	client  *Client
	ctx     context.Context
	table   string
	schema  string // optional schema prefix for multi-tenant isolation
	dialect Dialect
	guard   *SQLGuard
	pk      pkMeta
	exec    Executor // *sql.DB or *sql.Tx
	meta    *ModelMeta

	// Query state (cloned on each builder method)
	selectCols []string
	where      []condition
	orderBy    []order
	joins      []join
	preloads   []string
	limit      int
	offset     int
	hasLimit   bool // tracks if Limit() was explicitly called
	unscoped bool // if true, includes soft-deleted records
	tenantID string // for RowLevelSecurity isolation
	tenantCol string // column name for tenant isolation
	err      error // stores initialization error from ClientProvider
}

// fullTableName returns the table name optionally prefixed by a schema.
func (q *Query[T]) fullTableName() string {
	if q.schema != "" {
		return q.dialect.Quote(q.schema) + "." + q.dialect.Quote(q.table)
	}
	return q.dialect.Quote(q.table)
}

// clone creates a shallow copy of the Query with deep-copied slices.
// This ensures builder methods are safe for concurrent use from a shared base.
func (q *Query[T]) clone() *Query[T] {
	c := *q // shallow copy (copies all scalar fields)
	c.where = append([]condition(nil), q.where...)
	c.orderBy = append([]order(nil), q.orderBy...)
	c.selectCols = append([]string(nil), q.selectCols...)
	c.joins = append([]join(nil), q.joins...)
	c.preloads = append([]string(nil), q.preloads...)
	c.unscoped = q.unscoped
	c.tenantID = q.tenantID
	c.tenantCol = q.tenantCol
	return &c
}

// Preload specifies relations to load automatically.
func (q *Query[T]) Preload(relations ...string) *Query[T] {
	c := q.clone()
	c.preloads = append(c.preloads, relations...)
	return c
}

// Unscoped ignores soft-delete filters for the query.
func (q *Query[T]) Unscoped() *Query[T] {
	c := q.clone()
	c.unscoped = true
	return c
}

// Select specifies columns to select. If empty, all columns are selected.
func (q *Query[T]) Select(columns ...string) *Query[T] {
	c := q.clone()
	c.selectCols = columns
	return c
}

// Where adds a WHERE condition with AND logic.
func (q *Query[T]) Where(column string, operator string, value any) *Query[T] {
	c := q.clone()
	c.where = append(c.where, condition{
		column:   column,
		operator: operator,
		value:    value,
		logic:    "AND",
	})
	return c
}

// WhereIn adds a WHERE ... IN condition.
func (q *Query[T]) WhereIn(column string, values []any) *Query[T] {
	c := q.clone()
	c.where = append(c.where, condition{
		column:   column,
		operator: "IN",
		value:    values,
		logic:    "AND",
	})
	return c
}

// WhereBetween adds a WHERE ... BETWEEN condition.
func (q *Query[T]) WhereBetween(column string, start, end any) *Query[T] {
	c := q.clone()
	c.where = append(c.where, condition{
		column:   column,
		operator: "BETWEEN",
		value:    []any{start, end},
		logic:    "AND",
	})
	return c
}

// Or adds an OR condition group. The callback receives a fresh Query to build conditions.
// All conditions within the callback are grouped with AND and joined to the outer query with OR.
//
// Example:
//
//	quark.For[User](ctx, client).
//	    Where("active", "=", true).
//	    Or(func(q *Query[User]) *Query[User] {
//	        return q.Where("role", "=", "admin").Where("role", "=", "superadmin")
//	    }).List()
//
// Generates: WHERE "active" = $1 OR ("role" = $2 AND "role" = $3)
func (q *Query[T]) Or(fn func(*Query[T]) *Query[T]) *Query[T] {
	// Create a blank query to collect conditions from the callback
	blank := &Query[T]{
		client:  q.client,
		ctx:     q.ctx,
		table:   q.table,
		dialect: q.dialect,
		guard:   q.guard,
		pk:      q.pk,
		exec:    q.exec,
		meta:    q.meta,
	}
	result := fn(blank)

	c := q.clone()
	c.where = append(c.where, condition{
		logic: "OR",
		group: result.where,
	})
	return c
}

// OrderBy adds an ORDER BY clause.
func (q *Query[T]) OrderBy(column string, direction string) *Query[T] {
	c := q.clone()
	c.orderBy = append(c.orderBy, order{
		column: column,
		desc:   direction == "DESC" || direction == "desc",
	})
	return c
}

// Limit sets the maximum number of rows to return.
func (q *Query[T]) Limit(n int) *Query[T] {
	c := q.clone()
	c.limit = n
	c.hasLimit = true
	return c
}

// Offset sets the number of rows to skip.
func (q *Query[T]) Offset(n int) *Query[T] {
	c := q.clone()
	c.offset = n
	return c
}

// Join adds an INNER JOIN clause.
//
// Example:
//
//	quark.For[Order](ctx, client).
//	    Join("users", "users.id = orders.user_id").
//	    List()
func (q *Query[T]) Join(table, on string) *Query[T] {
	c := q.clone()
	c.joins = append(c.joins, join{joinType: "INNER JOIN", table: table, onClause: on})
	return c
}

// LeftJoin adds a LEFT JOIN clause.
func (q *Query[T]) LeftJoin(table, on string) *Query[T] {
	c := q.clone()
	c.joins = append(c.joins, join{joinType: "LEFT JOIN", table: table, onClause: on})
	return c
}

// RightJoin adds a RIGHT JOIN clause.
func (q *Query[T]) RightJoin(table, on string) *Query[T] {
	c := q.clone()
	c.joins = append(c.joins, join{joinType: "RIGHT JOIN", table: table, onClause: on})
	return c
}

// notifyObservers notifies all registered observers of a query event.
func (q *Query[T]) notifyObservers(event QueryEvent) {
	for _, obs := range q.client.observers {
		obs.ObserveQuery(event)
	}
}
