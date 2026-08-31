// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package db

// The framework links no database driver: each ships as its own module under
// drivers/ (ADR-031). The tests, however, run against every engine — the
// live DB matrix in CI connects to real PostgreSQL, MySQL, SQL Server and
// Oracle — so the TEST binary links them all, and registers the classifiers
// that the driver modules register in their init().
//
// It imports the driver packages directly rather than the nucleus modules
// that wrap them: pkg/db is what those modules import, so importing them back
// here would be a cycle in everything but the module graph. Keeping the two
// in step is the job of drivers-and-exporters CI lane, which runs each
// module's own conformance suite.
import (
	"errors"

	_ "github.com/go-sql-driver/mysql"
	gomysql "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/microsoft/go-mssqldb"
	mssql "github.com/microsoft/go-mssqldb"
	_ "github.com/sijms/go-ora/v2"
	goora "github.com/sijms/go-ora/v2/network"
	_ "modernc.org/sqlite"
	moderncsqlite "modernc.org/sqlite"

	"github.com/jcsvwinston/nucleus/pkg/db/driver"
)

func init() {
	driver.MustRegisterUniqueViolation("mysql", func(err error) bool {
		var e *gomysql.MySQLError
		return errors.As(err, &e) && e.Number == 1062
	})
	driver.MustRegisterUniqueViolation("sqlite", func(err error) bool {
		var e *moderncsqlite.Error
		if errors.As(err, &e) {
			code := e.Code()
			return code == 2067 || code == 1555
		}
		return false
	})
	driver.MustRegisterUniqueViolation("sqlserver", func(err error) bool {
		var e mssql.Error
		return errors.As(err, &e) && (e.Number == 2627 || e.Number == 2601)
	})
	driver.MustRegisterUniqueViolation("oracle", func(err error) bool {
		var e *goora.OracleError
		return errors.As(err, &e) && e.ErrCode == 1
	})
	// PostgreSQL needs none: pkg/db reads the SQLSTATE through the method
	// every PostgreSQL driver exposes.
}
