package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"

	gferrors "github.com/jcsvwinston/GoFrame/pkg/errors"
	"github.com/jcsvwinston/GoFrame/pkg/signals"
	"github.com/uptrace/bun"
)

type queryCondition struct {
	expr string
	args []interface{}
}

// CRUDBun provides generic CRUD operations backed by Bun.
// It mirrors CRUD behavior to enable an incremental migration from GORM.
type CRUDBun struct {
	db   *bun.DB
	meta *ModelMeta
	bus  *signals.Bus
}

// NewCRUDBun creates a Bun-backed CRUD operator for the given model metadata.
func NewCRUDBun(db *bun.DB, meta *ModelMeta, bus *signals.Bus) *CRUDBun {
	return &CRUDBun{db: db, meta: meta, bus: bus}
}

// FindAll retrieves a paginated, searchable, filterable list of records.
func (c *CRUDBun) FindAll(ctx context.Context, opts QueryOpts) (*PaginatedResult, error) {
	if opts.Page < 1 {
		opts.Page = 1
	}
	if opts.PageSize < 1 {
		opts.PageSize = c.meta.Config.PageSize
	}
	if opts.PageSize < 1 {
		opts.PageSize = 25
	}

	conds := c.buildConditions(opts)

	// Count matching rows.
	countQ := c.db.NewSelect().Table(c.meta.Table)
	for _, cond := range conds {
		countQ = countQ.Where(cond.expr, cond.args...)
	}

	total, err := countQ.Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("model.CRUDBun.FindAll count model=%s: %w", c.meta.Name, err)
	}

	// Load page.
	slicePtr := reflect.New(reflect.SliceOf(c.meta.Type))
	query := c.db.NewSelect().
		Model(slicePtr.Interface())

	for _, cond := range conds {
		query = query.Where(cond.expr, cond.args...)
	}

	orderBy := opts.OrderBy
	if orderBy == "" {
		orderBy = c.meta.Config.OrderBy
	}
	if orderBy == "" {
		orderBy = "id desc"
	}
	query = query.Order(orderBy)

	if len(opts.Fields) > 0 {
		for _, f := range opts.Fields {
			if c.isValidColumn(f) {
				query = query.Column(f)
			}
		}
	}

	offset := (opts.Page - 1) * opts.PageSize
	query = query.Offset(offset).Limit(opts.PageSize)

	if err := query.Scan(ctx); err != nil {
		return nil, fmt.Errorf("model.CRUDBun.FindAll model=%s: %w", c.meta.Name, err)
	}

	totalPages := int(math.Ceil(float64(total) / float64(opts.PageSize)))
	return &PaginatedResult{
		Items:      slicePtr.Elem().Interface(),
		Total:      int64(total),
		Page:       opts.Page,
		PageSize:   opts.PageSize,
		TotalPages: totalPages,
	}, nil
}

// FindByID retrieves a single record by primary key.
func (c *CRUDBun) FindByID(ctx context.Context, id interface{}) (interface{}, error) {
	entity := reflect.New(c.meta.Type).Interface()
	pkCol := c.primaryColumn()

	query := c.db.NewSelect().
		Model(entity).
		Where(fmt.Sprintf("%s = ?", pkCol), id).
		Limit(1)

	if c.hasDeletedAt() {
		query = query.Where("deleted_at IS NULL")
	}

	if err := query.Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, gferrors.NotFound(c.meta.Name, fmt.Sprintf("%v", id))
		}
		return nil, fmt.Errorf("model.CRUDBun.FindByID model=%s id=%v: %w", c.meta.Name, id, err)
	}

	return entity, nil
}

// Create inserts a new record. Emits PreCreate and PostCreate signals.
func (c *CRUDBun) Create(ctx context.Context, entity interface{}) error {
	if c.bus != nil {
		if err := c.bus.Emit(signals.Event{
			Signal: signals.PreCreate, ModelName: c.meta.Name, Payload: entity, Ctx: ctx,
		}); err != nil {
			return err
		}
	}

	if c.meta.Config.BeforeCreate != nil {
		if err := c.meta.Config.BeforeCreate(newBunHookContext(ctx, c.db), entity); err != nil {
			return fmt.Errorf("model.CRUDBun.Create BeforeCreate model=%s: %w", c.meta.Name, err)
		}
	}

	insertQ := c.db.NewInsert().Model(entity)
	if c.shouldExcludePrimaryKey(entity) {
		insertQ = insertQ.ExcludeColumn(c.primaryColumn())
		if c.supportsReturning() {
			insertQ = insertQ.Returning(c.primaryColumn())
		}
	}

	res, err := insertQ.Exec(ctx)
	if err != nil {
		return fmt.Errorf("model.CRUDBun.Create model=%s: %w", c.meta.Name, err)
	}
	c.setLastInsertID(entity, res)

	if c.meta.Config.AfterCreate != nil {
		if err := c.meta.Config.AfterCreate(newBunHookContext(ctx, c.db), entity); err != nil {
			return fmt.Errorf("model.CRUDBun.Create AfterCreate model=%s: %w", c.meta.Name, err)
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
func (c *CRUDBun) Update(ctx context.Context, id interface{}, updates map[string]interface{}) error {
	if c.bus != nil {
		if err := c.bus.Emit(signals.Event{
			Signal: signals.PreUpdate, ModelName: c.meta.Name, Payload: updates, Ctx: ctx,
		}); err != nil {
			return err
		}
	}

	if c.meta.Config.BeforeUpdate != nil {
		if err := c.meta.Config.BeforeUpdate(newBunHookContext(ctx, c.db), updates); err != nil {
			return fmt.Errorf("model.CRUDBun.Update BeforeUpdate model=%s: %w", c.meta.Name, err)
		}
	}

	pkCol := c.primaryColumn()
	query := c.db.NewUpdate().
		Table(c.meta.Table).
		Where(fmt.Sprintf("%s = ?", pkCol), id)

	if c.hasDeletedAt() {
		query = query.Where("deleted_at IS NULL")
	}

	setCount := 0
	for col, val := range updates {
		if !c.isValidColumn(col) {
			continue
		}
		query = query.Set(fmt.Sprintf("%s = ?", col), val)
		setCount++
	}
	if setCount == 0 {
		return gferrors.BadRequest("no valid columns provided")
	}

	res, err := query.Exec(ctx)
	if err != nil {
		return fmt.Errorf("model.CRUDBun.Update model=%s id=%v: %w", c.meta.Name, id, err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return gferrors.NotFound(c.meta.Name, fmt.Sprintf("%v", id))
	}

	if c.meta.Config.AfterUpdate != nil {
		if err := c.meta.Config.AfterUpdate(newBunHookContext(ctx, c.db), updates); err != nil {
			return fmt.Errorf("model.CRUDBun.Update AfterUpdate model=%s: %w", c.meta.Name, err)
		}
	}

	if c.bus != nil {
		c.bus.EmitAsync(signals.Event{
			Signal: signals.PostUpdate, ModelName: c.meta.Name, Payload: updates, Ctx: ctx,
		})
	}

	return nil
}

// Delete removes a record by primary key.
// If model has DeletedAt, performs soft delete.
func (c *CRUDBun) Delete(ctx context.Context, id interface{}) error {
	if c.bus != nil {
		if err := c.bus.Emit(signals.Event{
			Signal: signals.PreDelete, ModelName: c.meta.Name, Payload: id, Ctx: ctx,
		}); err != nil {
			return err
		}
	}

	if c.meta.Config.BeforeDelete != nil {
		if err := c.meta.Config.BeforeDelete(newBunHookContext(ctx, c.db), id); err != nil {
			return fmt.Errorf("model.CRUDBun.Delete BeforeDelete model=%s: %w", c.meta.Name, err)
		}
	}

	pkCol := c.primaryColumn()
	var (
		res sql.Result
		err error
	)

	if c.hasDeletedAt() {
		res, err = c.db.NewUpdate().
			Table(c.meta.Table).
			Set("deleted_at = ?", time.Now()).
			Where(fmt.Sprintf("%s = ?", pkCol), id).
			Where("deleted_at IS NULL").
			Exec(ctx)
	} else {
		res, err = c.db.NewDelete().
			Table(c.meta.Table).
			Where(fmt.Sprintf("%s = ?", pkCol), id).
			Exec(ctx)
	}
	if err != nil {
		return fmt.Errorf("model.CRUDBun.Delete model=%s id=%v: %w", c.meta.Name, id, err)
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

func (c *CRUDBun) buildConditions(opts QueryOpts) []queryCondition {
	var conditions []queryCondition

	if opts.Search != "" {
		searchFields := c.searchFields()
		if len(searchFields) > 0 {
			pattern := "%" + strings.ToLower(opts.Search) + "%"
			var clauses []string
			var args []interface{}
			for _, col := range searchFields {
				clauses = append(clauses, fmt.Sprintf("LOWER(%s) LIKE ?", col))
				args = append(args, pattern)
			}
			conditions = append(conditions, queryCondition{
				expr: "(" + strings.Join(clauses, " OR ") + ")",
				args: args,
			})
		}
	}

	for col, val := range opts.Filters {
		if c.isValidColumn(col) {
			conditions = append(conditions, queryCondition{
				expr: fmt.Sprintf("%s = ?", col),
				args: []interface{}{val},
			})
		}
	}

	if c.hasDeletedAt() {
		conditions = append(conditions, queryCondition{expr: "deleted_at IS NULL"})
	}

	return conditions
}

func (c *CRUDBun) searchFields() []string {
	var cols []string
	for _, f := range c.meta.Fields {
		if f.IsSearch {
			cols = append(cols, f.Column)
		}
	}
	return cols
}

func (c *CRUDBun) isValidColumn(col string) bool {
	for _, f := range c.meta.Fields {
		if f.Column == col {
			return true
		}
		if f.Column == "i_d" && col == "id" {
			return true
		}
	}
	return false
}

func (c *CRUDBun) hasDeletedAt() bool {
	for _, f := range c.meta.Fields {
		if f.Column == "deleted_at" {
			return true
		}
	}
	return false
}

func (c *CRUDBun) primaryColumn() string {
	for _, f := range c.meta.Fields {
		if f.Name == c.meta.PrimaryKey {
			// ExtractMeta currently maps ID -> i_d. Runtime SQL tables use id.
			if f.Column == "i_d" {
				return "id"
			}
			return f.Column
		}
	}
	return "id"
}

func (c *CRUDBun) setLastInsertID(entity interface{}, res sql.Result) {
	if entity == nil || res == nil {
		return
	}

	rv := reflect.ValueOf(entity)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return
	}

	idField := rv.FieldByName("ID")
	if !idField.IsValid() || !idField.CanSet() {
		return
	}

	// Keep existing non-zero IDs.
	if idField.Kind() == reflect.Uint && idField.Uint() != 0 {
		return
	}
	lastID, err := res.LastInsertId()
	if err != nil || lastID <= 0 {
		return
	}

	switch idField.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		idField.SetUint(uint64(lastID))
	}
}

func (c *CRUDBun) shouldExcludePrimaryKey(entity interface{}) bool {
	if entity == nil {
		return false
	}

	rv := reflect.ValueOf(entity)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return false
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return false
	}

	pkName := c.meta.PrimaryKey
	if pkName == "" {
		pkName = "ID"
	}

	pkField := rv.FieldByName(pkName)
	if !pkField.IsValid() {
		return false
	}

	switch pkField.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return pkField.Uint() == 0
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return pkField.Int() == 0
	default:
		return false
	}
}

func (c *CRUDBun) supportsReturning() bool {
	if c.db == nil || c.db.Dialect() == nil {
		return false
	}

	switch strings.ToLower(fmt.Sprint(c.db.Dialect().Name())) {
	case "pg", "postgres", "postgresql", "sqlite":
		return true
	default:
		return false
	}
}
