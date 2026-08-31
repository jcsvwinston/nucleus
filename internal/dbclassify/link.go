// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package dbclassify

// RegisterAll links every driver this project publishes and registers its
// classifier. It is for the two consumers that legitimately want all of them
// — the `nucleus` CLI and the framework's test binary — and for nothing else:
// an application links the one engine it uses, through its own module.
//
// It is a function rather than an init() so that importing the predicates
// (which the driver modules do, one at a time) does not drag in every driver.

import (
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/microsoft/go-mssqldb"
	_ "github.com/sijms/go-ora/v2"
	_ "modernc.org/sqlite"

	"github.com/jcsvwinston/nucleus/pkg/db/driver"
)

// RegisterAll registers the classifier for every engine. PostgreSQL is absent
// on purpose: pkg/db reads its SQLSTATE through the method every PostgreSQL
// driver exposes, so it needs no registration — and registering one typed to
// pgx would quietly stop covering lib/pq.
func RegisterAll() {
	driver.MustRegisterUniqueViolation("mysql", MySQLUniqueViolation)
	driver.MustRegisterUniqueViolation("sqlite", SQLiteUniqueViolation)
	driver.MustRegisterUniqueViolation("sqlserver", MSSQLUniqueViolation)
	driver.MustRegisterUniqueViolation("oracle", OracleUniqueViolation)
}
