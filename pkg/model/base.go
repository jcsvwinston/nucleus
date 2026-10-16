// Package model provides the model registry, metadata extraction, and generic
// CRUD operations for the Nucleus framework. It uses reflection to extract
// struct metadata at registration time and uses the native SQL runtime.
//
// Deprecated: Quark is the data layer of the suite. This package leaves the
// public API with the major: the starter, the generators and nucleustest
// move to Quark before that, and the admin panel reads Quark models through
// quarkdatasource. Nothing is removed in v1.x. See DEP-2026-024. Scheduled
// for removal in v2.0.0, no earlier than 2027-01-17.
package model

import (
	"time"
)

// BaseModel provides the standard fields that all Nucleus models should embed.
// It is the equivalent of Django's models.Model.
//
// Deprecated: embed a Quark model instead; see DEP-2026-024. Scheduled for
// removal in v2.0.0, no earlier than 2027-01-17.
type BaseModel struct {
	ID        uint       `db:"pk" json:"id"`
	CreatedAt time.Time  `db:"readonly" json:"created_at"`
	UpdatedAt time.Time  `db:"readonly" json:"updated_at"`
	DeletedAt *time.Time `db:"column:deleted_at" json:"-"`
}
