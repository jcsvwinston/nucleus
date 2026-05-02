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
func (q *BaseQuery) executeExec(ctx context.Context, sqlStr string, args []any) (sql.Result, error) {
	if q.err != nil {
		return nil, q.err
	}
	// Base handler: direct execution
	handler := ExecFunc(func(ctx context.Context, exec Executor, s string, a []any) (sql.Result, error) {
		start := time.Now()
		res, err := exec.ExecContext(ctx, s, a...)
		duration := time.Since(start)

		// Automatic Cache Invalidation (Maintain data freshness)
		if err == nil && q.client.cacheStore != nil && q.table != "" {
			_ = q.client.cacheStore.InvalidateTags(ctx, q.table)
		}

		// Notify observers
		rowsAffected := int64(0)
		if err == nil {
			rowsAffected, _ = res.RowsAffected()
		}
		q.notifyObservers(QueryEvent{
			SQL:       s,
			Args:      a,
			Duration:  duration,
			Error:     err,
			Table:     q.table,
			Operation: "EXEC",
			Rows:      rowsAffected,
		})

		return res, err
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
func (q *BaseQuery) ensureTenantID(v reflect.Value) {
	if q.tenantID == "" || q.tenantCol == "" {
		return
	}

	if q.meta != nil {
		if fm, ok := q.meta.FieldByCol[q.tenantCol]; ok {
			field := v.Field(fm.Index)
			if field.Kind() == reflect.String && isZeroValue(field) {
				field.SetString(q.tenantID)
			}
		}
	}
}

// saveAny persists an arbitrary struct to the database using its metadata.
// It handles recursive saving of associations if they are present.
func (q *BaseQuery) saveAny(ctx context.Context, exec Executor, entity any, isUpdate bool) (int64, error) {
	v := reflect.ValueOf(entity)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return 0, fmt.Errorf("entity must be a non-nil pointer")
	}
	elem := v.Elem()
	if elem.Kind() != reflect.Struct {
		return 0, fmt.Errorf("entity must be a struct")
	}

	meta := GetModelMetaByType(elem.Type())

	// Decide if we should Insert or Update.
	// If it's an update but the PK is zero, it must be an insert (new record in existing association).
	actualUpdate := isUpdate
	if actualUpdate && isZeroPKValue(elem.Field(meta.PK.Index)) {
		actualUpdate = false
	}
	
	// 1. Save BelongsTo associations FIRST (so we have their PKs)
	for _, rel := range meta.Relations {
		if rel.Type == "belongs_to" {
			field := elem.FieldByName(rel.Field)
			if !field.IsZero() {
				// Save related record
				relatedVal := field
				if relatedVal.Kind() != reflect.Ptr {
					relatedVal = field.Addr()
				}
				
				// Create a sub-query context for the related model, inheriting tenant info
				sq := &BaseQuery{
					client:    q.client,
					ctx:       ctx,
					dialect:   q.dialect,
					guard:     q.guard,
					table:     relMetaFromType(rel.RefType).Table,
					pk:        relMetaFromType(rel.RefType).PK,
					exec:      exec,
					meta:      relMetaFromType(rel.RefType),
					tenantID:  q.tenantID,
					tenantCol: q.tenantCol,
				}

				if _, err := sq.saveAny(ctx, exec, relatedVal.Interface(), actualUpdate); err != nil {
					return 0, err
				}
				// Set foreign key on parent
				relMeta := GetModelMetaByType(rel.RefType)
				relPKVal := reflect.Indirect(field).Field(relMeta.PK.Index).Interface()
				
				if fm, ok := meta.FieldByCol[rel.JoinCol]; ok {
					parentFKField := elem.Field(fm.Index)
					if parentFKField.CanSet() {
						parentFKField.Set(reflect.ValueOf(relPKVal))
					}
				}
			}
		}
	}

	// 2. Save the main entity using a dynamic query
	dq := &BaseQuery{
		client:    q.client,
		ctx:       ctx,
		dialect:   q.dialect,
		guard:     q.guard,
		table:     meta.Table,
		pk:        meta.PK,
		exec:      exec,
		meta:      meta,
		tenantID:  q.tenantID,
		tenantCol: q.tenantCol,
	}

	rowsAffected := int64(0)
	if actualUpdate {
		sqlStr, args, err := dq.buildUpdate(elem)
		if err != nil {
			return 0, err
		}
		res, err := dq.executeExec(ctx, sqlStr, args)
		if err != nil {
			return 0, err
		}
		rowsAffected, _ = res.RowsAffected()
	} else {
		sqlStr, args, err := dq.buildInsert(elem)
		if err != nil {
			return 0, err
		}

		if q.dialect.SupportsReturning() {
			if q.dialect.Name() == "oracle" {
				var id int64
				sqlWithOut := "BEGIN " + sqlStr + " INTO :ret_id; END;"
				_, err = dq.executeExec(ctx, sqlWithOut, append(args, sql.Named("ret_id", sql.Out{Dest: &id})))
				if err != nil {
					return 0, err
				}
				setPKValue(elem, meta.PK, id)
			} else {
				row := dq.executeQueryRow(ctx, sqlStr, args)
				if err := dq.scanReturning(row, elem); err != nil {
					return 0, err
				}
			}
			rowsAffected = 1
		} else {
			// Handle MSSQL/MySQL last id
			if q.dialect.Name() == "mssql" {
				sqlBatch := sqlStr + "; " + q.dialect.LastInsertIDQuery(meta.Table, meta.PK.Column)
				var lastID int64
				err = dq.executeQueryRow(ctx, sqlBatch, args).Scan(&lastID)
				if err != nil {
					return 0, err
				}
				setPKValue(elem, meta.PK, lastID)
				rowsAffected = 1
			} else {
				res, err := dq.executeExec(ctx, sqlStr, args)
				if err != nil {
					return 0, err
				}
				if q.dialect.SupportsLastInsertID() {
					lastID, _ := res.LastInsertId()
					setPKValue(elem, meta.PK, lastID)
				}
				rowsAffected, _ = res.RowsAffected()
			}
		}
	}

	// 3. Save HasOne/HasMany associations AFTER
	if err := dq.saveAssociations(elem, actualUpdate); err != nil {
		return rowsAffected, err
	}

	return rowsAffected, nil
}

func relMetaFromType(t reflect.Type) *ModelMeta {
	return GetModelMetaByType(t)
}

// Create inserts a new record.
// The entity must have a db tag on fields to be persisted.
// Returns with the ID set from the database.
// Create inserts a new record and recursively saves associations.
func (q *Query[T]) Create(entity *T) error {
	if q.client == nil {
		return fmt.Errorf("%w: client not initialized", ErrInvalidQuery)
	}

	if err := q.client.Validate(q.ctx, entity); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	if hook, ok := any(entity).(BeforeCreateHook); ok {
		if err := hook.BeforeCreate(q.ctx); err != nil {
			return err
		}
	}

	if _, err := q.saveAny(q.ctx, q.exec, entity, false); err != nil {
		return err
	}

	if hook, ok := any(entity).(AfterCreateHook); ok {
		if err := hook.AfterCreate(q.ctx); err != nil {
			return err
		}
	}

	return nil
}

// buildInsert constructs the INSERT SQL.
func (q *BaseQuery) buildInsert(v reflect.Value) (string, []any, error) {
	t := v.Type()
	q.ensureTenantID(v) // Inject tenant ID BEFORE processing fields

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

	// Auto-inject tenant ID if needed (only if not already in columns)
	if q.tenantCol != "" {
		// Check if it's already in the columns
		found := false
		for _, col := range columns {
			// Compare lowercase and unquoted to avoid duplicates across dialects (especially Oracle)
			cleanCol := strings.Trim(strings.ToLower(col), `"'[]`)
			if cleanCol == strings.ToLower(q.tenantCol) {
				found = true
				break
			}
		}
		if !found {
			if fm, ok := q.meta.FieldByCol[q.tenantCol]; ok {
				columns = append(columns, q.dialect.Quote(q.tenantCol))
				placeholders = append(placeholders, q.dialect.Placeholder(argIndex))
				args = append(args, v.Field(fm.Index).Interface())
				argIndex++
			}
		}
	}

	var sqlStr strings.Builder
	sqlStr.WriteString("INSERT INTO ")
	sqlStr.WriteString(q.fullTableName())
	sqlStr.WriteString(" (")
	sqlStr.WriteString(strings.Join(columns, ", "))
	sqlStr.WriteString(") VALUES (")
	sqlStr.WriteString(strings.Join(placeholders, ", "))
	sqlStr.WriteString(")")

	// Add RETURNING if supported — use detected PK column
	if q.dialect.SupportsReturning() && q.pk.Column != "" {
		sqlStr.WriteString(" ")
		sqlStr.WriteString(q.dialect.Returning(q.pk.Column))
	}

	return sqlStr.String(), args, nil
}

// scanReturning scans RETURNING clause results into the entity's PK field.
func (q *BaseQuery) scanReturning(row *sql.Row, v reflect.Value) error {
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
// Update performs a partial update of non-zero fields and recursively saves associations.
func (q *Query[T]) Update(entity *T) (int64, error) {
	if q.client == nil {
		return 0, fmt.Errorf("%w: client not initialized", ErrInvalidQuery)
	}

	if hook, ok := any(entity).(BeforeUpdateHook); ok {
		if err := hook.BeforeUpdate(q.ctx); err != nil {
			return 0, err
		}
	}

	rowsAffected, err := q.saveAny(q.ctx, q.exec, entity, true)
	if err != nil {
		return rowsAffected, err
	}

	if hook, ok := any(entity).(AfterUpdateHook); ok {
		if err := hook.AfterUpdate(q.ctx); err != nil {
			return rowsAffected, err
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

	result, err := q.executeExec(ctx, sql, args)

	if err != nil {
		return 0, fmt.Errorf("update failed: %w", err)
	}

	rowsAffected := int64(0)
	if result != nil {
		rowsAffected, _ = result.RowsAffected()
	}

	return rowsAffected, nil
}

// buildUpdate constructs UPDATE SQL from entity (partial update of non-zero fields).
// Merges PK-based WHERE with any additional Where() conditions from the builder.
func (q *BaseQuery) buildUpdate(v reflect.Value) (string, []any, error) {
	t := v.Type()
	q.ensureTenantID(v) // Inject tenant ID BEFORE processing fields

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
func (q *BaseQuery) buildUpdateMap(data map[string]any) (string, []any, error) {
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

	result, err := q.executeExec(ctx, sql.String(), args)
	if err != nil {
		return 0, fmt.Errorf("soft delete failed: %w", err)
	}

	rowsAffected := int64(0)
	if result != nil {
		rowsAffected, _ = result.RowsAffected()
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

	result, err := q.executeExec(ctx, sql.String(), args)
	if err != nil {
		return 0, fmt.Errorf("delete failed: %w", err)
	}

	rowsAffected := int64(0)
	if result != nil {
		rowsAffected, _ = result.RowsAffected()
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

	result, err := q.executeExec(ctx, sql.String(), args)
	if err != nil {
		return 0, fmt.Errorf("delete failed: %w", err)
	}

	rowsAffected := int64(0)
	if result != nil {
		rowsAffected, _ = result.RowsAffected()
	}

	return rowsAffected, nil
}

// saveAssociations recursively saves related models.
func (q *BaseQuery) saveAssociations(v reflect.Value, isUpdate bool) error {
	for _, rel := range q.meta.Relations {
		field := v.FieldByName(rel.Field)
		if !field.IsValid() || field.IsZero() {
			continue
		}

		switch rel.Type {
		case "has_one":
			pkVal := getPKValue(v, q.pk)
			relatedVal := field
			if relatedVal.Kind() != reflect.Ptr {
				relatedVal = field.Addr()
			}
			
			// Set foreign key on related
			relMeta := GetModelMetaByType(rel.RefType)
			if fm, ok := relMeta.FieldByCol[rel.JoinCol]; ok {
				reflect.Indirect(relatedVal).Field(fm.Index).Set(reflect.ValueOf(pkVal))
			}

			if _, err := q.saveAny(q.ctx, q.exec, relatedVal.Interface(), isUpdate); err != nil {
				return err
			}

		case "has_many":
			pkVal := getPKValue(v, q.pk)
			relMeta := GetModelMetaByType(rel.RefType)

			for i := 0; i < field.Len(); i++ {
				item := field.Index(i)
				itemPtr := item.Addr()

				// Set foreign key
				if fm, ok := relMeta.FieldByCol[rel.JoinCol]; ok {
					item.Field(fm.Index).Set(reflect.ValueOf(pkVal))
				}

				if _, err := q.saveAny(q.ctx, q.exec, itemPtr.Interface(), isUpdate); err != nil {
					return err
				}
			}

		case "many_to_many":
			pkVal := getPKValue(v, q.pk)
			relMeta := GetModelMetaByType(rel.RefType)

			for i := 0; i < field.Len(); i++ {
				item := field.Index(i)
				itemPtr := item.Addr()

				// Save related item first
				if _, err := q.saveAny(q.ctx, q.exec, itemPtr.Interface(), isUpdate); err != nil {
					return err
				}

				// Link in join table
				itemPK := getPKValue(item, relMeta.PK)
				if err := q.linkM2M(*rel, pkVal, itemPK); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// linkM2M creates a record in the join table if it doesn't exist.
func (q *BaseQuery) linkM2M(rel RelationMeta, parentPK, childPK any) error {
	sqlStr := fmt.Sprintf("INSERT INTO %s (%s, %s) VALUES (%s, %s)",
		q.dialect.Quote(rel.JoinTable),
		q.dialect.Quote(rel.JoinFK),
		q.dialect.Quote(rel.JoinRefFK),
		q.dialect.Placeholder(1),
		q.dialect.Placeholder(2),
	)

	_, err := q.executeExec(q.ctx, sqlStr, []any{parentPK, childPK})
	if err != nil {
		// Ignore duplicate key errors - already linked
		return nil 
	}
	return nil
}

