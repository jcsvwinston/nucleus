package model

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"strings"

	gferrors "github.com/goframe/goframe/pkg/errors"
	"github.com/goframe/goframe/pkg/signals"
	"gorm.io/gorm"
)

// QueryOpts controls filtering, searching, sorting, and pagination.
type QueryOpts struct {
	Page     int               // 1-based page number (default: 1)
	PageSize int               // Items per page (default: from ModelConfig)
	Search   string            // Free-text search across SearchFields
	Filters  map[string]string // Exact-match filters: column -> value
	OrderBy  string            // SQL ORDER BY clause (e.g. "created_at desc")
	Fields   []string          // SELECT specific columns (empty = all)
}

// PaginatedResult wraps a paginated query response.
type PaginatedResult struct {
	Items      interface{} `json:"items"`
	Total      int64       `json:"total"`
	Page       int         `json:"page"`
	PageSize   int         `json:"page_size"`
	TotalPages int         `json:"total_pages"`
}

// CRUD provides generic create/read/update/delete operations for a registered model.
// It uses reflection to create model instances dynamically so the admin panel can
// operate on any registered model without compile-time type knowledge.
type CRUD struct {
	db   *gorm.DB
	meta *ModelMeta
	bus  *signals.Bus
}

// NewCRUD creates a CRUD operator for the given model metadata.
// The signals bus is optional (pass nil to disable signal emission).
func NewCRUD(db *gorm.DB, meta *ModelMeta, bus *signals.Bus) *CRUD {
	return &CRUD{db: db, meta: meta, bus: bus}
}

// FindAll retrieves a paginated, searchable, filterable list of records.
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

	// Create a slice to hold results: reflect.SliceOf(meta.Type)
	slicePtr := reflect.New(reflect.SliceOf(c.meta.Type))

	query := c.db.WithContext(ctx).Table(c.meta.Table)

	// Apply search across search fields using LOWER(col) LIKE ?
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
			query = query.Where(strings.Join(clauses, " OR "), args...)
		}
	}

	// Apply exact-match filters
	for col, val := range opts.Filters {
		if c.isValidColumn(col) {
			query = query.Where(fmt.Sprintf("%s = ?", col), val)
		}
	}

	// Soft delete filter: only non-deleted records if model has DeletedAt
	if c.hasDeletedAt() {
		query = query.Where("deleted_at IS NULL")
	}

	// Count total matching records
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("model.CRUD.FindAll count model=%s: %w", c.meta.Name, err)
	}

	// Apply ordering
	orderBy := opts.OrderBy
	if orderBy == "" {
		orderBy = c.meta.Config.OrderBy
	}
	if orderBy == "" {
		orderBy = c.meta.PrimaryKey + " desc"
		if c.meta.PrimaryKey == "" {
			orderBy = "id desc"
		}
	}
	query = query.Order(orderBy)

	// Apply select fields
	if len(opts.Fields) > 0 {
		query = query.Select(opts.Fields)
	}

	// Apply pagination
	offset := (opts.Page - 1) * opts.PageSize
	query = query.Offset(offset).Limit(opts.PageSize)

	if err := query.Find(slicePtr.Interface()).Error; err != nil {
		return nil, fmt.Errorf("model.CRUD.FindAll model=%s: %w", c.meta.Name, err)
	}

	totalPages := int(math.Ceil(float64(total) / float64(opts.PageSize)))

	return &PaginatedResult{
		Items:      slicePtr.Elem().Interface(),
		Total:      total,
		Page:       opts.Page,
		PageSize:   opts.PageSize,
		TotalPages: totalPages,
	}, nil
}

// FindByID retrieves a single record by primary key.
func (c *CRUD) FindByID(ctx context.Context, id interface{}) (interface{}, error) {
	entity := reflect.New(c.meta.Type).Interface()
	query := c.db.WithContext(ctx).Table(c.meta.Table)

	if c.hasDeletedAt() {
		query = query.Where("deleted_at IS NULL")
	}

	if err := query.First(entity, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, gferrors.NotFound(c.meta.Name, fmt.Sprintf("%v", id))
		}
		return nil, fmt.Errorf("model.CRUD.FindByID model=%s id=%v: %w", c.meta.Name, id, err)
	}

	return entity, nil
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
		if err := c.meta.Config.BeforeCreate(c.db, entity); err != nil {
			return fmt.Errorf("model.CRUD.Create BeforeCreate model=%s: %w", c.meta.Name, err)
		}
	}

	if err := c.db.WithContext(ctx).Table(c.meta.Table).Create(entity).Error; err != nil {
		return fmt.Errorf("model.CRUD.Create model=%s: %w", c.meta.Name, err)
	}

	if c.meta.Config.AfterCreate != nil {
		if err := c.meta.Config.AfterCreate(c.db, entity); err != nil {
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
		if err := c.meta.Config.BeforeUpdate(c.db, updates); err != nil {
			return fmt.Errorf("model.CRUD.Update BeforeUpdate model=%s: %w", c.meta.Name, err)
		}
	}

	result := c.db.WithContext(ctx).Table(c.meta.Table).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("model.CRUD.Update model=%s id=%v: %w", c.meta.Name, id, result.Error)
	}
	if result.RowsAffected == 0 {
		return gferrors.NotFound(c.meta.Name, fmt.Sprintf("%v", id))
	}

	if c.meta.Config.AfterUpdate != nil {
		if err := c.meta.Config.AfterUpdate(c.db, updates); err != nil {
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
		if err := c.meta.Config.BeforeDelete(c.db, id); err != nil {
			return fmt.Errorf("model.CRUD.Delete BeforeDelete model=%s: %w", c.meta.Name, err)
		}
	}

	entity := reflect.New(c.meta.Type).Interface()
	result := c.db.WithContext(ctx).Table(c.meta.Table).Delete(entity, id)
	if result.Error != nil {
		return fmt.Errorf("model.CRUD.Delete model=%s id=%v: %w", c.meta.Name, id, result.Error)
	}
	if result.RowsAffected == 0 {
		return gferrors.NotFound(c.meta.Name, fmt.Sprintf("%v", id))
	}

	if c.bus != nil {
		c.bus.EmitAsync(signals.Event{
			Signal: signals.PostDelete, ModelName: c.meta.Name, Payload: id, Ctx: ctx,
		})
	}

	return nil
}

// searchFields returns the column names configured for search.
func (c *CRUD) searchFields() []string {
	var cols []string
	for _, f := range c.meta.Fields {
		if f.IsSearch {
			cols = append(cols, f.Column)
		}
	}
	return cols
}

// isValidColumn checks if a column name corresponds to a real field.
func (c *CRUD) isValidColumn(col string) bool {
	for _, f := range c.meta.Fields {
		if f.Column == col {
			return true
		}
	}
	return false
}

// hasDeletedAt checks if the model has a DeletedAt field for soft deletes.
func (c *CRUD) hasDeletedAt() bool {
	for _, f := range c.meta.Fields {
		if f.Column == "deleted_at" {
			return true
		}
	}
	return false
}
