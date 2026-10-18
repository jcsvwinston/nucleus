// Package model is the framework's internal data layer: metadata extraction
// from struct tags, the migration scaffolds the generators and AutoMigrate
// render per engine, the SQL observer the observability bus is fed from, and
// the generic CRUD the test kit's factories write through.
//
// It left the public API at v2.0.0 (DEP-2026-024): Quark is the data layer
// of the suite, and an application defines its models with Quark. What is
// here is what the framework itself still needs; nothing in it is a contract
// for applications.
package model

import (
	"time"
)

// BaseModel provides the standard fields the framework's own test models
// embed: the primary key, the timestamps and the soft-delete marker.
type BaseModel struct {
	ID        uint       `db:"pk" json:"id"`
	CreatedAt time.Time  `db:"readonly" json:"created_at"`
	UpdatedAt time.Time  `db:"readonly" json:"updated_at"`
	DeletedAt *time.Time `db:"column:deleted_at" json:"-"`
}
