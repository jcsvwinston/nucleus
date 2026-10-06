// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package testsqlite links SQLite into the framework's own test binaries and
// registers its unique-violation classifier. SQLite is the engine the root
// module's tests run on; nothing outside a _test.go file imports this
// package, and a test asserts that.
//
// It replaces internal/alldrivers, which linked all five engines into every
// test binary — and therefore put four of them into the go.mod every
// application inherits, since a module's go.mod lists what its own tests
// import (NU-106). The lanes that run the tests against PostgreSQL, MySQL,
// SQL Server and Oracle link those engines on top, through their driver
// modules: scripts/ci/test_with_engines.sh builds the test binary in a
// workspace with an overlay that imports them, so the root module requires
// no engine but this one.
//
// The classifier registered here is internal/dbclassify's copy, the one the
// driver modules published up to v0.1.7 register; the driver modules since
// carry their own.
package testsqlite

import (
	"sync"

	_ "modernc.org/sqlite"

	"github.com/jcsvwinston/nucleus/internal/dbclassify"
	"github.com/jcsvwinston/nucleus/pkg/db/driver"
)

var once sync.Once

// Register registers SQLite's classifier, once per process. The driver
// itself registers on import, as database/sql drivers do.
func Register() {
	once.Do(func() {
		driver.MustRegisterUniqueViolation("sqlite", dbclassify.SQLiteUniqueViolation)
	})
}
