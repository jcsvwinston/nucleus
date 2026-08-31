// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package sqlite links the pure-Go SQLite driver into a Nucleus application.
//
// Import it for its side effect, once, anywhere in the program:
//
//	import _ "github.com/jcsvwinston/nucleus/drivers/sqlite"
//
// It registers modernc.org/sqlite under "sqlite" — the pure-Go driver, so it
// needs no cgo and cross-compiles — which is what pkg/db resolves a sqlite://
// URL, a bare path ending in .db or .sqlite, and :memory: to.
package sqlite

import (
	_ "modernc.org/sqlite"

	"github.com/jcsvwinston/nucleus/internal/dbclassify"
	"github.com/jcsvwinston/nucleus/pkg/db/driver"
)

func init() {
	driver.MustRegisterUniqueViolation("sqlite", dbclassify.SQLiteUniqueViolation)
}
