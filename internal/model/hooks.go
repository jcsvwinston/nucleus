package model

import (
	"context"
	"database/sql"
)

const (
	HookEngineSQL = "sql"
)

// HookContext exposes runtime information to lifecycle hooks in an
// engine-agnostic way.
type HookContext struct {
	Context context.Context
	Engine  string
	DB      *sql.DB
	Tx      *sql.Tx
}

// HookFunc is the signature for model lifecycle hooks.
type HookFunc func(ctx HookContext, entity interface{}) error

func newSQLHookContext(ctx context.Context, db *sql.DB, tx *sql.Tx) HookContext {
	return HookContext{
		Context: ctx,
		Engine:  HookEngineSQL,
		DB:      db,
		Tx:      tx,
	}
}
