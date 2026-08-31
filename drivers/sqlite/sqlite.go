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
	"errors"

	moderncsqlite "modernc.org/sqlite"
	_ "modernc.org/sqlite"

	"github.com/jcsvwinston/nucleus/pkg/db/driver"
)

func init() {
	driver.MustRegisterUniqueViolation("sqlite", isUniqueViolation)
}

// isUniqueViolation matches SQLite's extended result codes: 2067 is
// SQLITE_CONSTRAINT_UNIQUE and 1555 SQLITE_CONSTRAINT_PRIMARYKEY. Both mean
// "that value is already taken"; the primary-key code is separate and would
// be missed by a check that only looked for the unique one.
func isUniqueViolation(err error) bool {
	var e *moderncsqlite.Error
	if errors.As(err, &e) {
		code := e.Code()
		return code == 2067 || code == 1555
	}
	return false
}
