package quark

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// executeQuery runs a QueryContext through the middleware chain.
// This is used for SELECT operations returning multiple rows.
func (q *Query[T]) executeQuery(ctx context.Context, sqlStr string, args []any) (*sql.Rows, error) {
	if q.err != nil {
		return nil, q.err
	}
	// Base handler: direct execution
	handler := QueryFunc(func(ctx context.Context, exec Executor, s string, a []any) (*sql.Rows, error) {
		return exec.QueryContext(ctx, s, a...)
	})

	// Wrap with middleware in reverse order
	for i := len(q.client.middleware) - 1; i >= 0; i-- {
		handler = q.client.middleware[i].WrapQuery(handler)
	}

	return handler(ctx, q.exec, sqlStr, args)
}

// executeQueryRow runs a QueryRowContext through the middleware chain.
// This is used for SELECT operations returning a single row (like Count).
func (q *Query[T]) executeQueryRow(ctx context.Context, sqlStr string, args []any) *sql.Row {
	// Note: We cannot return an error here directly since sql.Row doesn't expose error until Scan.
	// But executing a bad query will cause an error on Scan anyway.
	if q.err != nil {
		// Fall through, the error will be caught during query execution since it's invalid
		// A cleaner approach would be returning an error if possible, but the signature prevents it.
	}
	// Base handler: direct execution
	handler := QueryRowFunc(func(ctx context.Context, exec Executor, s string, a []any) *sql.Row {
		return exec.QueryRowContext(ctx, s, a...)
	})

	// Wrap with middleware in reverse order
	for i := len(q.client.middleware) - 1; i >= 0; i-- {
		handler = q.client.middleware[i].WrapQueryRow(handler)
	}

	return handler(ctx, q.exec, sqlStr, args)
}

// List executes the query and returns all matching rows.
// If Limit() is not called, uses a safe default (100) to prevent OOM.
// Use Iter() for unbounded streaming or Paginate() for large datasets.
func (q *Query[T]) List() ([]T, error) {
	if q.client == nil {
		return nil, fmt.Errorf("%w: client not initialized", ErrInvalidQuery)
	}

	// Safety: if no explicit limit, apply safe default
	if !q.hasLimit {
		q.limit = 100 // Safe default
		q.client.logger.Warn("List() called without explicit Limit(), using safe default of 100. Use Iter() for unbounded queries or call Limit() explicitly.")
	}

	// Build query
	sqlStr, args, err := q.buildSelect()
	if err != nil {
		return nil, err
	}

	// Apply hard limit from client limits
	if q.limit > q.client.limits.MaxResults {
		q.limit = q.client.limits.MaxResults
	}

	// Execute (through middleware if configured)
	ctx, cancel := context.WithTimeout(q.ctx, q.client.limits.QueryTimeout)
	defer cancel()

	start := time.Now()
	rows, err := q.executeQuery(ctx, sqlStr, args)
	duration := time.Since(start)

	// Notify observers
	q.notifyObservers(QueryEvent{
		SQL:       sqlStr,
		Args:      args,
		Duration:  duration,
		Error:     err,
		Table:     q.table,
		Operation: "SELECT",
	})

	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	// Scan results
	var results []T
	for rows.Next() {
		var entity T
		if err := q.scanRow(rows, &entity); err != nil {
			return nil, err
		}
		results = append(results, entity)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(q.preloads) > 0 && len(results) > 0 {
		if err := q.loadRelations(results); err != nil {
			return nil, err
		}
	}

	return results, nil
}

// First returns the first matching row or ErrNotFound.
func (q *Query[T]) First() (T, error) {
	var zero T

	q.limit = 1
	q.hasLimit = true
	results, err := q.List()
	if err != nil {
		return zero, err
	}

	if len(results) == 0 {
		return zero, ErrNotFound
	}

	return results[0], nil
}

// Find retrieves a single row by primary key.
func (q *Query[T]) Find(id any) (T, error) {
	var zero T

	if q.client == nil {
		return zero, fmt.Errorf("%w: client not initialized", ErrInvalidQuery)
	}

	q.where = []condition{{
		column:   q.pk.Column,
		operator: "=",
		value:    id,
		logic:    "AND",
	}}
	q.limit = 1

	return q.First()
}

// Cursor returns a Cursor for manual iteration over large result sets.
// The Cursor must be closed after use (defer cursor.Close()).
//
// Example:
//
//	cursor, err := quark.For[User](ctx, client).Where("active", "=", true).Cursor()
//	if err != nil { log.Fatal(err) }
//	defer cursor.Close()
//
//	for cursor.Next() {
//	    var user User
//	    if err := cursor.Scan(&user); err != nil { break }
//	    process(user)
//	}
func (q *Query[T]) Cursor() (*Cursor[T], error) {
	if q.client == nil {
		return nil, fmt.Errorf("%w: client not initialized", ErrInvalidQuery)
	}

	sqlStr, args, err := q.buildSelect()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(q.ctx, q.client.limits.QueryTimeout)
	rows, err := q.executeQuery(ctx, sqlStr, args)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("query failed: %w", err)
	}

	return &Cursor[T]{
		rows:   rows,
		ctx:    ctx,
		cancel: cancel,
		query:  q,
		sql:    sqlStr,
		args:   args,
		start:  time.Now(),
	}, nil
}

// Iter executes the query and iterates over results one by one.
// Uses streaming to handle large datasets without loading all into memory.
//
// Example:
//
//	err := quark.For[User](ctx, client).Where("active", "=", true).Iter(func(user User) error {
//	    process(user)
//	    return nil
//	})
func (q *Query[T]) Iter(fn func(T) error) error {
	if q.client == nil {
		return fmt.Errorf("%w: client not initialized", ErrInvalidQuery)
	}

	sqlStr, args, err := q.buildSelect()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(q.ctx, q.client.limits.QueryTimeout)
	defer cancel()

	start := time.Now()
	rows, err := q.executeQuery(ctx, sqlStr, args)
	duration := time.Since(start)

	q.notifyObservers(QueryEvent{
		SQL:       sqlStr,
		Args:      args,
		Duration:  duration,
		Error:     err,
		Table:     q.table,
		Operation: "SELECT (stream)",
	})

	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var entity T
		if err := q.scanRow(rows, &entity); err != nil {
			return err
		}
		if err := fn(entity); err != nil {
			return err
		}
	}

	return rows.Err()
}

// Count returns the total number of matching rows.
func (q *Query[T]) Count() (int64, error) {
	if q.err != nil {
		return 0, q.err
	}
	if q.client == nil {
		return 0, fmt.Errorf("%w: client not initialized", ErrInvalidQuery)
	}

	var sqlBuf strings.Builder
	var args []any

	sqlBuf.WriteString("SELECT COUNT(*) FROM ")
	sqlBuf.WriteString(q.fullTableName())

	// JOIN clauses
	for _, j := range q.joins {
		sqlBuf.WriteString(" ")
		sqlBuf.WriteString(j.joinType)
		sqlBuf.WriteString(" ")
		sqlBuf.WriteString(q.dialect.Quote(j.table))
		sqlBuf.WriteString(" ON ")
		sqlBuf.WriteString(j.onClause)
	}

	// WHERE clause
	whereConds := q.where
	if !q.unscoped {
		if _, hasDeletedAt := q.meta.FieldByCol["deleted_at"]; hasDeletedAt {
			whereConds = append([]condition{{
				column:   "deleted_at",
				operator: "IS NULL",
				logic:    "AND",
			}}, whereConds...)
		}
	}

	if len(whereConds) > 0 {
		argIndex := 1
		whereSQL, whereArgs, err := q.buildWhereClause(whereConds, argIndex)
		if err != nil {
			return 0, err
		}
		sqlBuf.WriteString(" WHERE ")
		sqlBuf.WriteString(whereSQL)
		args = append(args, whereArgs...)
	}

	ctx, cancel := context.WithTimeout(q.ctx, q.client.limits.QueryTimeout)
	defer cancel()

	var count int64
	err := q.executeQueryRow(ctx, sqlBuf.String(), args).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count failed: %w", err)
	}

	return count, nil
}

// buildSelect constructs the SELECT SQL query.
func (q *Query[T]) buildSelect() (string, []any, error) {
	var sqlBuf strings.Builder
	var args []any

	// SELECT clause
	sqlBuf.WriteString("SELECT ")
	if len(q.selectCols) > 0 {
		quoted := make([]string, len(q.selectCols))
		for i, col := range q.selectCols {
			if err := q.guard.ValidateIdentifier(col); err != nil {
				return "", nil, err
			}
			quoted[i] = q.dialect.Quote(col)
		}
		sqlBuf.WriteString(strings.Join(quoted, ", "))
	} else {
		sqlBuf.WriteString("*")
	}

	// FROM clause
	sqlBuf.WriteString(" FROM ")
	if err := q.guard.ValidateIdentifier(q.table); err != nil {
		return "", nil, err
	}
	sqlBuf.WriteString(q.fullTableName())

	// JOIN clauses
	if len(q.joins) > 0 {
		if len(q.joins) > q.client.limits.MaxJoins {
			return "", nil, fmt.Errorf("%w: query exceeds maximum of %d joins", ErrInvalidQuery, q.client.limits.MaxJoins)
		}
		for _, j := range q.joins {
			if err := q.guard.ValidateIdentifier(j.table); err != nil {
				return "", nil, err
			}
			sqlBuf.WriteString(" ")
			sqlBuf.WriteString(j.joinType)
			sqlBuf.WriteString(" ")
			sqlBuf.WriteString(q.dialect.Quote(j.table))
			sqlBuf.WriteString(" ON ")
			sqlBuf.WriteString(j.onClause)
		}
	}

	// WHERE clause
	whereConds := q.where
	if !q.unscoped {
		if _, hasDeletedAt := q.meta.FieldByCol["deleted_at"]; hasDeletedAt {
			whereConds = append([]condition{{
				column:   "deleted_at",
				operator: "IS NULL",
				logic:    "AND",
			}}, whereConds...)
		}
	}

	if len(whereConds) > 0 {
		argIndex := 1
		whereSQL, whereArgs, err := q.buildWhereClause(whereConds, argIndex)
		if err != nil {
			return "", nil, err
		}
		sqlBuf.WriteString(" WHERE ")
		sqlBuf.WriteString(whereSQL)
		args = append(args, whereArgs...)
	}

	// ORDER BY clause
	if len(q.orderBy) > 0 {
		sqlBuf.WriteString(" ORDER BY ")
		for i, o := range q.orderBy {
			if i > 0 {
				sqlBuf.WriteString(", ")
			}
			if err := q.guard.ValidateIdentifier(o.column); err != nil {
				return "", nil, err
			}
			sqlBuf.WriteString(q.dialect.Quote(o.column))
			if o.desc {
				sqlBuf.WriteString(" DESC")
			} else {
				sqlBuf.WriteString(" ASC")
			}
		}
	}

	// LIMIT/OFFSET
	limitOffset := q.dialect.LimitOffset(q.limit, q.offset)
	if limitOffset != "" {
		sqlBuf.WriteString(" ")
		sqlBuf.WriteString(limitOffset)
	}

	return sqlBuf.String(), args, nil
}

// buildWhereClause recursively builds WHERE SQL from conditions,
// handling AND/OR logic and grouped sub-conditions.
func (q *Query[T]) buildWhereClause(conds []condition, argIndex int) (string, []any, error) {
	var parts []string
	var args []any

	for i, cond := range conds {
		// Determine connector
		connector := ""
		if i > 0 {
			if cond.logic == "OR" {
				connector = " OR "
			} else {
				connector = " AND "
			}
		}

		// Handle grouped sub-conditions (from Or())
		if len(cond.group) > 0 {
			groupSQL, groupArgs, err := q.buildWhereClause(cond.group, argIndex)
			if err != nil {
				return "", nil, err
			}
			parts = append(parts, connector+"("+groupSQL+")")
			args = append(args, groupArgs...)
			argIndex += len(groupArgs)
			continue
		}

		// Normal condition
		if err := q.guard.ValidateIdentifier(cond.column); err != nil {
			return "", nil, err
		}
		if err := q.guard.ValidateOperator(cond.operator); err != nil {
			return "", nil, err
		}

		var condSQL strings.Builder
		condSQL.WriteString(connector)
		condSQL.WriteString(q.dialect.Quote(cond.column))
		condSQL.WriteString(" ")
		condSQL.WriteString(cond.operator)
		condSQL.WriteString(" ")

		switch cond.operator {
		case "IN", "NOT IN":
			values := cond.value.([]any)
			placeholders := make([]string, len(values))
			for j := range values {
				placeholders[j] = q.dialect.Placeholder(argIndex)
				args = append(args, values[j])
				argIndex++
			}
			condSQL.WriteString("(")
			condSQL.WriteString(strings.Join(placeholders, ", "))
			condSQL.WriteString(")")
		case "BETWEEN", "NOT BETWEEN":
			values := cond.value.([]any)
			condSQL.WriteString(q.dialect.Placeholder(argIndex))
			condSQL.WriteString(" AND ")
			condSQL.WriteString(q.dialect.Placeholder(argIndex + 1))
			args = append(args, values[0], values[1])
			argIndex += 2
		case "IS NULL", "IS NOT NULL":
			// No placeholder or value needed
		default:
			condSQL.WriteString(q.dialect.Placeholder(argIndex))
			args = append(args, cond.value)
			argIndex++
		}

		parts = append(parts, condSQL.String())
	}

	return strings.Join(parts, ""), args, nil
}

// scanRow scans a single row into the entity.
// Uses cached ModelMeta for O(1) field lookups when available.
func (q *Query[T]) scanRow(rows *sql.Rows, dest *T) error {
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return fmt.Errorf("dest must be a non-nil pointer")
	}

	elem := v.Elem()
	if elem.Kind() != reflect.Struct {
		return fmt.Errorf("dest must point to a struct")
	}

	columns, err := rows.Columns()
	if err != nil {
		return err
	}

	scanDest := make([]any, len(columns))
	for i, col := range columns {
		// Fast path: use cached metadata
		if q.meta != nil {
			if fm, ok := q.meta.FieldByCol[col]; ok {
				scanDest[i] = elem.Field(fm.Index).Addr().Interface()
				continue
			}
		}
		// Slow path: reflection lookup
		field := q.findField(elem, col)
		if field.IsValid() && field.CanAddr() {
			scanDest[i] = field.Addr().Interface()
		} else {
			var discard any
			scanDest[i] = &discard
		}
	}

	return rows.Scan(scanDest...)
}

// findField finds a struct field matching the column name (fallback for uncached lookups).
func (q *Query[T]) findField(elem reflect.Value, column string) reflect.Value {
	t := elem.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		dbTag := field.Tag.Get("db")
		if dbTag == column {
			return elem.Field(i)
		}

		if strings.EqualFold(field.Name, column) {
			return elem.Field(i)
		}
	}

	return reflect.Value{}
}

// loadRelations eager loads requested relations for the given results.
func (q *Query[T]) loadRelations(results []T) error {
	if !q.client.limits.AllowRawQueries {
		q.client.limits.AllowRawQueries = true // temporarily enable for internal use
		defer func() { q.client.limits.AllowRawQueries = false }()
	}

	for _, relName := range q.preloads {
		relMeta, ok := q.meta.Relations[relName]
		if !ok {
			return fmt.Errorf("relation %s not found on model %s", relName, q.table)
		}

		relModel := GetModelMetaByType(relMeta.RefType)
		
		// Determine which column in the parent we are joining on
		var parentCol string
		if relMeta.Type == "belongs_to" {
			parentCol = relMeta.JoinCol // The parent holds the FK
		} else {
			parentCol = q.meta.PK.Column // The parent holds the PK
		}

		// Find the field index for the parent column
		parentFieldMeta, ok := q.meta.FieldByCol[parentCol]
		if !ok {
			// Fallback: assume it's a field name
			for _, fm := range q.meta.Fields {
				if strings.EqualFold(fm.Type.Name(), parentCol) {
					parentFieldMeta = &fm
					break
				}
			}
			if parentFieldMeta == nil {
				return fmt.Errorf("could not find parent column %s for relation %s", parentCol, relName)
			}
		}

		// Collect parent keys
		var parentKeys []any
		keyMap := make(map[any][]int) // parent key -> indexes in results slice

		for i := range results {
			val := reflect.ValueOf(&results[i]).Elem()
			pKey := val.Field(parentFieldMeta.Index).Interface()
			
			// Skip zero values
			if reflect.ValueOf(pKey).IsZero() {
				continue
			}

			parentKeys = append(parentKeys, pKey)
			keyMap[pKey] = append(keyMap[pKey], i)
		}

		if len(parentKeys) == 0 {
			continue
		}

		// Determine the foreign column in the related table
		var foreignCol string
		if relMeta.Type == "belongs_to" {
			foreignCol = relModel.PK.Column
		} else {
			foreignCol = relMeta.JoinCol
		}

		// Build query using IN clause
		placeholders := make([]string, len(parentKeys))
		for i := range parentKeys {
			placeholders[i] = q.dialect.Placeholder(i + 1)
		}
		
		query := fmt.Sprintf("SELECT * FROM %s WHERE %s IN (%s)", 
			q.dialect.Quote(relModel.Table),
			q.dialect.Quote(foreignCol),
			strings.Join(placeholders, ", "),
		)

		ctx, cancel := context.WithTimeout(q.ctx, q.client.limits.QueryTimeout)
		rows, err := q.client.RawQuery(ctx, query, parentKeys...)
		cancel()
		if err != nil {
			return fmt.Errorf("failed to load relation %s: %w", relName, err)
		}

		// Read and map results
		cols, _ := rows.Columns()
		
		foreignFieldMeta, ok := relModel.FieldByCol[foreignCol]
		if !ok {
			rows.Close()
			return fmt.Errorf("could not find foreign column %s in related model", foreignCol)
		}

		for rows.Next() {
			// Create a new instance of the related model
			relPtr := reflect.New(relMeta.RefType)
			relVal := relPtr.Elem()

			scanDest := make([]any, len(cols))
			for i, col := range cols {
				if fm, ok := relModel.FieldByCol[col]; ok {
					scanDest[i] = relVal.Field(fm.Index).Addr().Interface()
				} else {
					var discard any
					scanDest[i] = &discard
				}
			}

			if err := rows.Scan(scanDest...); err != nil {
				rows.Close()
				return err
			}

			// Get the foreign key value from the scanned related model
			fKey := relVal.Field(foreignFieldMeta.Index).Interface()

			// Assign to parent structs
			if parentIndexes, ok := keyMap[fKey]; ok {
				for _, pIdx := range parentIndexes {
					parentVal := reflect.ValueOf(&results[pIdx]).Elem()
					relField := parentVal.FieldByName(relName)
					
					if relMeta.IsSlice {
						// Append to slice
						relField.Set(reflect.Append(relField, relVal))
					} else {
						// Set single value
						if relField.Kind() == reflect.Ptr {
							relField.Set(relPtr)
						} else {
							relField.Set(relVal)
						}
					}
				}
			}
		}
		rows.Close()
		
		if err := rows.Err(); err != nil {
			return err
		}
	}

	return nil
}
