package model

import "context"

// CRUDOperator defines the generic CRUD behavior used by higher layers.
// CRUD (SQL-backed) implements this contract.
type CRUDOperator interface {
	FindAll(ctx context.Context, opts QueryOpts) (*PaginatedResult, error)
	FindByID(ctx context.Context, id interface{}) (interface{}, error)
	Create(ctx context.Context, entity interface{}) error
	Update(ctx context.Context, id interface{}, updates map[string]interface{}) error
	Delete(ctx context.Context, id interface{}) error
}
