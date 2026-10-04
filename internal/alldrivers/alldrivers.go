// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package alldrivers links every database driver this repository publishes
// and registers each one's unique-violation classifier. It is for the two
// binaries that legitimately want every engine and for nothing else:
//
//   - the `nucleus` CLI, a tool people install once and point at whatever
//     database they have — `nucleus migrate` has to work against the database
//     in front of it without a rebuild;
//   - the framework's own test binaries, which the DB matrix runs against
//     PostgreSQL, MySQL, SQL Server and Oracle, and the unit tests against
//     SQLite.
//
// Neither can import the drivers/ modules: those require this module, so the
// requirement would be circular. So this package links the drivers directly
// and registers the predicates of internal/dbclassify — the copy the root
// keeps, and the one the driver modules published up to v0.1.7 still
// register.
//
// An application never links it. Its engine comes from that engine's module,
// which registers its own classifier, so importing drivers/sqlite links SQLite
// and no other engine (NU-8). The tests of this package assert both halves:
// only cmd/nucleus imports it outside test files, and each driver module's
// graph holds its own engine and none of the other four.
package alldrivers

import (
	"sync"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/microsoft/go-mssqldb"
	_ "github.com/sijms/go-ora/v2"
	_ "modernc.org/sqlite"

	"github.com/jcsvwinston/nucleus/internal/dbclassify"
	"github.com/jcsvwinston/nucleus/pkg/db/driver"
)

var once sync.Once

// RegisterAll registers the classifier of every engine, once per process.
// PostgreSQL is absent on purpose: pkg/db reads its SQLSTATE through the
// method every PostgreSQL driver exposes, so it needs no registration — and
// registering one typed to pgx would quietly stop covering lib/pq.
func RegisterAll() {
	once.Do(func() {
		driver.MustRegisterUniqueViolation("mysql", dbclassify.MySQLUniqueViolation)
		driver.MustRegisterUniqueViolation("sqlite", dbclassify.SQLiteUniqueViolation)
		driver.MustRegisterUniqueViolation("sqlserver", dbclassify.MSSQLUniqueViolation)
		driver.MustRegisterUniqueViolation("oracle", dbclassify.OracleUniqueViolation)
	})
}
