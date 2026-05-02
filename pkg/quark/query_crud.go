package quark

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

// executeExec runs an ExecContext through the middleware chain.
// This is used for INSERT, UPDATE, DELETE operations.
func (q *Query[T]) executeExec(ctx context.Context, sqlStr string, args []any) (sql.Result, error) {
	if q.err != nil {
		return nil, q.err
	}
	// Base handler: direct execution
	handler := ExecFunc(func(ctx context.Context, exec Executor, s string, a []any) (sql.Result, error) {
		return exec.ExecContext(ctx, s, a...)
	})

	// Wrap with middleware in reverse order
	for i := len(q.client.middleware) - 1; i >= 0; i-- {
		handler = q.client.middleware[i].WrapExec(handler)
	}

	return handler(ctx, q.exec, sqlStr, args)
}


// isZeroPKValue checks if a primary key value is its zero value.
func isZeroPKValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.String:
		return v.String() == ""
	default:
		return false
	}
}

// getPKValue returns the primary key value from a struct.
func getPKValue(v reflect.Value, pk pkMeta) any {
	return v.Field(pk.Index).Interface()
}

// setPKValue sets the primary key value on a struct.
func setPKValue(v reflect.Value, pk pkMeta, id int64) {
	field := v.Field(pk.Index)
	switch field.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		field.SetInt(id)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		field.SetUint(uint64(id))
	}
}

// ensureTenantID populates the tenant field if RLS is active and the field is zero.
func (q *Query[T]) ensureTenantID(entity *T) {
	if q.tenantID == "" || q.tenantCol == "" {
		return
	}

	v := reflect.ValueOf(entity).Elem()
	if q.meta != nil {
		if fm, ok := q.meta.FieldByCol[q.tenantCol]; ok {
			field := v.Field(fm.Index)
			if isZeroValue(field) {
				field.SetString(q.tenantID)
			}
		}
	}
}

// Create inserts a new record.
// The entity must have a db tag on fields to be persisted.
// Returns with the ID set from the database.
func (q *Query[T]) Create(entity *T) error {
	if q.client == nil {
		return fmt.Errorf("%w: client not initialized", ErrInvalidQuery)
	}

	if err := q.client.Validate(q.ctx, entity); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	q.ensureTenantID(entity)

	if hook, ok := any(entity).(BeforeCreateHook); ok {
		if err := hook.BeforeCreate(q.ctx); err != nil {
			return err
		}
	}

	// Build INSERT
	sql, args, err := q.buildInsert(entity)
	if err != nil {
		return err
	}

	// Execute with timeout
	ctx, cancel := context.WithTimeout(q.ctx, q.client.limits.QueryTimeout)
	defer cancel()

	start := time.Now()

	if q.dialect.SupportsReturning() {
		// Use RETURNING clause
		row := q.executeQueryRow(ctx, sql, args)
		duration := time.Since(start)

		// Scan returned values (id, timestamps, etc.)
		if err := q.scanReturning(row, entity); err != nil {
			return err
		}

		q.notifyObservers(QueryEvent{
			SQL:       sql,
			Args:      args,
			Duration:  duration,
			Table:     q.table,
			Operation: "INSERT",
		})
	} else {
		// Use LastInsertId
		result, err := q.executeExec(ctx, sql, args)
		duration := time.Since(start)

		q.notifyObservers(QueryEvent{
			SQL:       sql,
			Args:      args,
			Duration:  duration,
			Error:     err,
			Table:     q.table,
			Operation: "INSERT",
		})

		if err != nil {
			return fmt.Errorf("insert failed: %w", err)
		}

		lastID, _ := result.LastInsertId()
		v := reflect.ValueOf(entity).Elem()
		setPKValue(v, q.pk, lastID)
	}

	if hook, ok := any(entity).(AfterCreateHook); ok {
		if err := hook.AfterCreate(q.ctx); err != nil {
			return err
		}
	}

	return nil
}

// buildInsert constructs the INSERT SQL.
func (q *Query[T]) buildInsert(entity *T) (string, []any, error) {
	v := reflect.ValueOf(entity).Elem()
	t := v.Type()

	var columns []string
	var placeholders []string
	var args []any
	argIndex := 1

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		dbTag := field.Tag.Get("db")
		if dbTag == "" || dbTag == "-" {
			continue // Skip fields without db tag
		}

		// Skip auto-increment PK if it's zero (let DB assign it)
		if i == q.pk.Index && isZeroPKValue(v.Field(i)) {
			continue
		}

		if err := q.guard.ValidateIdentifier(dbTag); err != nil {
			return "", nil, err
		}

		columns = append(columns, q.dialect.Quote(dbTag))
		placeholders = append(placeholders, q.dialect.Placeholder(argIndex))
		args = append(args, v.Field(i).Interface())
		argIndex++
	}

	var sql strings.Builder
	sql.WriteString("INSERT INTO ")
	sql.WriteString(q.fullTableName())
	sql.WriteString(" (")
	sql.WriteString(strings.Join(columns, ", "))
	sql.WriteString(") VALUES (")
	sql.WriteString(strings.Join(placeholders, ", "))
	sql.WriteString(")")

	// Add RETURNING if supported — use detected PK column
	if q.dialect.SupportsReturning() && q.pk.Column != "" {
		sql.WriteString(" ")
		sql.WriteString(q.dialect.Returning(q.pk.Column))
	}

	return sql.String(), args, nil
}

// scanReturning scans RETURNING clause results into the entity's PK field.
func (q *Query[T]) scanReturning(row *sql.Row, entity *T) error {
	v := reflect.ValueOf(entity).Elem()
	pkField := v.Field(q.pk.Index)

	if pkField.CanAddr() {
		return row.Scan(pkField.Addr().Interface())
	}

	// Fallback: scan into a temporary and set
	var id int64
	if err := row.Scan(&id); err != nil {
		return err
	}
	setPKValue(v, q.pk, id)
	return nil
}

// Update updates the entity by its primary key.
// Non-zero fields are updated (partial update).
// Any Where() conditions are merged into the WHERE clause alongside the PK.
// Returns the number of rows affected.
func (q *Query[T]) Update(entity *T) (int64, error) {
	if q.client == nil {
		return 0, fmt.Errorf("%w: client not initialized", ErrInvalidQuery)
	}

	if err := q.client.Validate(q.ctx, entity); err != nil {
		return 0, fmt.Errorf("validation failed: %w", err)
	}

	q.ensureTenantID(entity)

	if hook, ok := any(entity).(BeforeUpdateHook); ok {
		if err := hook.BeforeUpdate(q.ctx); err != nil {
			return 0, err
		}
	}

	// Build UPDATE
	sql, args, err := q.buildUpdate(entity)
	if err != nil {
		return 0, err
	}

	// Execute with timeout
	ctx, cancel := context.WithTimeout(q.ctx, q.client.limits.QueryTimeout)
	defer cancel()

	start := time.Now()
	result, err := q.executeExec(ctx, sql, args)
	duration := time.Since(start)

	// Notify observers
	rowsAffected := int64(0)
	if err == nil {
		rowsAffected, _ = result.RowsAffected()
	}
	q.notifyObservers(QueryEvent{
		SQL:       sql,
		Args:      args,
		Duration:  duration,
		Error:     err,
		Table:     q.table,
		Operation: "UPDATE",
		Rows:      rowsAffected,
	})

	if err != nil {
		return 0, fmt.Errorf("update failed: %w", err)
	}

	if hook, ok := any(entity).(AfterUpdateHook); ok {
		if err := hook.AfterUpdate(q.ctx); err != nil {
			return 0, err
		}
	}

	return rowsAffected, nil
}

// UpdateMap updates fields using a map (for partial updates without full entity).
// Requires Where clause for safety.
// Returns the number of rows affected.
func (q *Query[T]) UpdateMap(data map[string]any) (int64, error) {
	if q.client == nil {
		return 0, fmt.Errorf("%w: client not initialized", ErrInvalidQuery)
	}

	if len(data) == 0 {
		return 0, fmt.Errorf("%w: no fields to update", ErrInvalidQuery)
	}

	// Require WHERE clause for safety — validate BEFORE building SQL
	if len(q.where) == 0 {
		return 0, fmt.Errorf("%w: UpdateMap requires Where clause to prevent accidental full table update", ErrInvalidQuery)
	}

	// Build UPDATE from map
	sql, args, err := q.buildUpdateMap(data)
	if err != nil {
		return 0, err
	}

	// Execute with timeout
	ctx, cancel := context.WithTimeout(q.ctx, q.client.limits.QueryTimeout)
	defer cancel()

	start := time.Now()
	result, err := q.executeExec(ctx, sql, args)
	duration := time.Since(start)

	// Notify observers
	rowsAffected := int64(0)
	if err == nil {
		rowsAffected, _ = result.RowsAffected()
	}
	q.notifyObservers(QueryEvent{
		SQL:       sql,
		Args:      args,
		Duration:  duration,
		Error:     err,
		Table:     q.table,
		Operation: "UPDATE",
		Rows:      rowsAffected,
	})

	if err != nil {
		return 0, fmt.Errorf("update failed: %w", err)
	}

	return rowsAffected, nil
}

// buildUpdate constructs UPDATE SQL from entity (partial update of non-zero fields).
// Merges PK-based WHERE with any additional Where() conditions from the builder.
func (q *Query[T]) buildUpdate(entity *T) (string, []any, error) {
	v := reflect.ValueOf(entity).Elem()
	t := v.Type()

	var setClauses []string
	var args []any
	argIndex := 1

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		dbTag := field.Tag.Get("db")
		if dbTag == "" || dbTag == "-" {
			continue
		}

		// Skip primary key in SET clause
		if i == q.pk.Index {
			continue
		}

		fieldValue := v.Field(i)

		// Skip zero values (partial update)
		if isZeroValue(fieldValue) {
			continue
		}

		if err := q.guard.ValidateIdentifier(dbTag); err != nil {
			return "", nil, err
		}

		setClauses = append(setClauses, fmt.Sprintf("%s = %s", q.dialect.Quote(dbTag), q.dialect.Placeholder(argIndex)))
		args = append(args, fieldValue.Interface())
		argIndex++
	}

	if len(setClauses) == 0 {
		return "", nil, fmt.Errorf("%w: no non-zero fields to update", ErrInvalidQuery)
	}

	// Get PK value for WHERE clause
	pkValue := getPKValue(v, q.pk)

	var sql strings.Builder
	sql.WriteString("UPDATE ")
	sql.WriteString(q.fullTableName())
	sql.WriteString(" SET ")
	sql.WriteString(strings.Join(setClauses, ", "))
	sql.WriteString(" WHERE ")
	sql.WriteString(q.dialect.Quote(q.pk.Column))
	sql.WriteString(" = ")
	sql.WriteString(q.dialect.Placeholder(argIndex))
	args = append(args, pkValue)
	argIndex++

	// Merge any additional Where() conditions
	for _, cond := range q.where {
		sql.WriteString(" AND ")

		if err := q.guard.ValidateIdentifier(cond.column); err != nil {
			return "", nil, err
		}
		if err := q.guard.ValidateOperator(cond.operator); err != nil {
			return "", nil, err
		}

		sql.WriteString(q.dialect.Quote(cond.column))
		sql.WriteString(" ")
		sql.WriteString(cond.operator)
		sql.WriteString(" ")
		sql.WriteString(q.dialect.Placeholder(argIndex))
		args = append(args, cond.value)
		argIndex++
	}

	return sql.String(), args, nil
}

// buildUpdateMap constructs UPDATE SQL from map.
// Keys are sorted for deterministic query generation.
func (q *Query[T]) buildUpdateMap(data map[string]any) (string, []any, error) {
	// Sort keys for deterministic SQL output
	keys := make([]string, 0, len(data))
	for col := range data {
		keys = append(keys, col)
	}
	sort.Strings(keys)

	var setClauses []string
	var args []any
	argIndex := 1

	for _, col := range keys {
		val := data[col]
		if err := q.guard.ValidateIdentifier(col); err != nil {
			return "", nil, err
		}

		setClauses = append(setClauses, fmt.Sprintf("%s = %s", q.dialect.Quote(col), q.dialect.Placeholder(argIndex)))
		args = append(args, val)
		argIndex++
	}

	var sql strings.Builder
	sql.WriteString("UPDATE ")
	sql.WriteString(q.fullTableName())
	sql.WriteString(" SET ")
	sql.WriteString(strings.Join(setClauses, ", "))

	// WHERE clause from query conditions
	if len(q.where) > 0 {
		sql.WriteString(" WHERE ")
		for i, cond := range q.where {
			if i > 0 {
				sql.WriteString(" AND ")
			}

			if err := q.guard.ValidateIdentifier(cond.column); err != nil {
				return "", nil, err
			}
			if err := q.guard.ValidateOperator(cond.operator); err != nil {
				return "", nil, err
			}

			sql.WriteString(q.dialect.Quote(cond.column))
			sql.WriteString(" ")
			sql.WriteString(cond.operator)
			sql.WriteString(" ")
			sql.WriteString(q.dialect.Placeholder(argIndex))
			args = append(args, cond.value)
			argIndex++
		}
	}

	return sql.String(), args, nil
}

// isZeroValue checks if a reflect.Value is the zero value for its type.
func isZeroValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.String:
		return v.String() == ""
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Map:
		return v.IsNil()
	default:
		return false
	}
}

// Delete performs a soft delete by setting deleted_at = NOW().
// If the model doesn't have deleted_at field, performs hard delete.
// Returns the number of rows affected.
func (q *Query[T]) Delete(entity *T) (int64, error) {
	if q.client == nil {
		return 0, fmt.Errorf("%w: client not initialized", ErrInvalidQuery)
	}

	if hook, ok := any(entity).(BeforeDeleteHook); ok {
		if err := hook.BeforeDelete(q.ctx); err != nil {
			return 0, err
		}
	}

	v := reflect.ValueOf(entity).Elem()
	t := v.Type()

	if q.pk.Column == "" {
		return 0, fmt.Errorf("%w: no primary key field found", ErrInvalidModel)
	}

	hasDeletedAt := false
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).Tag.Get("db") == "deleted_at" {
			hasDeletedAt = true
			break
		}
	}

	pkValue := getPKValue(v, q.pk)

	var rows int64
	var err error
	if hasDeletedAt {
		rows, err = q.softDelete(pkValue)
	} else {
		rows, err = q.hardDeleteByPK(pkValue)
	}

	if err == nil {
		if hook, ok := any(entity).(AfterDeleteHook); ok {
			if hErr := hook.AfterDelete(q.ctx); hErr != nil {
				return rows, hErr
			}
		}
	}

	return rows, err
}

// DeleteBy performs a hard delete with WHERE conditions.
// Requires Where clause for safety.
func (q *Query[T]) DeleteBy() (int64, error) {
	if q.client == nil {
		return 0, fmt.Errorf("%w: client not initialized", ErrInvalidQuery)
	}

	if len(q.where) == 0 {
		return 0, fmt.Errorf("%w: DeleteBy requires Where clause to prevent accidental full table delete", ErrInvalidQuery)
	}

	return q.hardDeleteWhere()
}

// HardDelete permanently deletes the entity by its primary key.
func (q *Query[T]) HardDelete(entity *T) (int64, error) {
	if q.client == nil {
		return 0, fmt.Errorf("%w: client not initialized", ErrInvalidQuery)
	}

	if hook, ok := any(entity).(BeforeDeleteHook); ok {
		if err := hook.BeforeDelete(q.ctx); err != nil {
			return 0, err
		}
	}

	if q.pk.Column == "" {
		return 0, fmt.Errorf("%w: no primary key field found", ErrInvalidModel)
	}

	v := reflect.ValueOf(entity).Elem()
	pkValue := getPKValue(v, q.pk)

	rows, err := q.hardDeleteByPK(pkValue)
	if err == nil {
		if hook, ok := any(entity).(AfterDeleteHook); ok {
			if hErr := hook.AfterDelete(q.ctx); hErr != nil {
				return rows, hErr
			}
		}
	}

	return rows, err
}

// softDelete performs a soft delete (sets deleted_at = NOW()).
func (q *Query[T]) softDelete(pkValue any) (int64, error) {
	var sql strings.Builder
	var args []any

	sql.WriteString("UPDATE ")
	sql.WriteString(q.fullTableName())
	sql.WriteString(" SET ")
	sql.WriteString(q.dialect.Quote("deleted_at"))
	sql.WriteString(" = ")
	sql.WriteString(q.dialect.CurrentTimestamp())
	sql.WriteString(" WHERE ")
	sql.WriteString(q.dialect.Quote(q.pk.Column))
	sql.WriteString(" = ")
	sql.WriteString(q.dialect.Placeholder(1))
	args = append(args, pkValue)

	// Add deleted_at IS NULL to ensure we don't update already deleted rows
	sql.WriteString(" AND ")
	sql.WriteString(q.dialect.Quote("deleted_at"))
	sql.WriteString(" IS NULL")

	ctx, cancel := context.WithTimeout(q.ctx, q.client.limits.QueryTimeout)
	defer cancel()

	start := time.Now()
	result, err := q.executeExec(ctx, sql.String(), args)
	duration := time.Since(start)

	rowsAffected := int64(0)
	if err == nil {
		rowsAffected, _ = result.RowsAffected()
	}

	q.notifyObservers(QueryEvent{
		SQL:       sql.String(),
		Args:      args,
		Duration:  duration,
		Error:     err,
		Table:     q.table,
		Operation: "DELETE (soft)",
		Rows:      rowsAffected,
	})

	if err != nil {
		return 0, fmt.Errorf("soft delete failed: %w", err)
	}

	return rowsAffected, nil
}

// hardDeleteByPK performs a hard delete by primary key.
func (q *Query[T]) hardDeleteByPK(pkValue any) (int64, error) {
	var sql strings.Builder
	var args []any

	sql.WriteString("DELETE FROM ")
	sql.WriteString(q.fullTableName())
	sql.WriteString(" WHERE ")
	sql.WriteString(q.dialect.Quote(q.pk.Column))
	sql.WriteString(" = ")
	sql.WriteString(q.dialect.Placeholder(1))
	args = append(args, pkValue)

	ctx, cancel := context.WithTimeout(q.ctx, q.client.limits.QueryTimeout)
	defer cancel()

	start := time.Now()
	result, err := q.executeExec(ctx, sql.String(), args)
	duration := time.Since(start)

	rowsAffected := int64(0)
	if err == nil {
		rowsAffected, _ = result.RowsAffected()
	}

	q.notifyObservers(QueryEvent{
		SQL:       sql.String(),
		Args:      args,
		Duration:  duration,
		Error:     err,
		Table:     q.table,
		Operation: "DELETE (hard)",
		Rows:      rowsAffected,
	})

	if err != nil {
		return 0, fmt.Errorf("delete failed: %w", err)
	}

	return rowsAffected, nil
}

// hardDeleteWhere performs a hard delete with WHERE conditions.
func (q *Query[T]) hardDeleteWhere() (int64, error) {
	var sql strings.Builder
	var args []any
	argIndex := 1

	sql.WriteString("DELETE FROM ")
	sql.WriteString(q.fullTableName())

	// WHERE clause
	if len(q.where) > 0 {
		sql.WriteString(" WHERE ")
		for i, cond := range q.where {
			if i > 0 {
				sql.WriteString(" AND ")
			}

			if err := q.guard.ValidateIdentifier(cond.column); err != nil {
				return 0, err
			}
			if err := q.guard.ValidateOperator(cond.operator); err != nil {
				return 0, err
			}

			sql.WriteString(q.dialect.Quote(cond.column))
			sql.WriteString(" ")
			sql.WriteString(cond.operator)
			sql.WriteString(" ")
			sql.WriteString(q.dialect.Placeholder(argIndex))
			args = append(args, cond.value)
			argIndex++
		}
	}

	ctx, cancel := context.WithTimeout(q.ctx, q.client.limits.QueryTimeout)
	defer cancel()

	start := time.Now()
	result, err := q.executeExec(ctx, sql.String(), args)
	duration := time.Since(start)

	rowsAffected := int64(0)
	if err == nil {
		rowsAffected, _ = result.RowsAffected()
	}

	q.notifyObservers(QueryEvent{
		SQL:       sql.String(),
		Args:      args,
		Duration:  duration,
		Error:     err,
		Table:     q.table,
		Operation: "DELETE (hard)",
		Rows:      rowsAffected,
	})

	if err != nil {
		return 0, fmt.Errorf("delete failed: %w", err)
	}

	return rowsAffected, nil
}
