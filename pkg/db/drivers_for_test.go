// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package db

// The framework no longer links any database driver: each one ships as its
// own module (drivers/postgres, drivers/mysql, …) so an application pays only
// for the engine it uses. The tests still need one to open a database, and
// they use SQLite because it needs no server.
//
// This imports the driver package directly rather than the nucleus module
// that wraps it: pkg/db is what drivers/sqlite imports, so importing it back
// here would be a cycle in everything but the module graph. The classifier
// those tests exercise is registered below, mirroring what drivers/sqlite
// does in its init().
import (
	"errors"

	moderncsqlite "modernc.org/sqlite"
	_ "modernc.org/sqlite"

	"github.com/jcsvwinston/nucleus/pkg/db/driver"
)

func init() {
	driver.MustRegisterUniqueViolation("sqlite", func(err error) bool {
		var e *moderncsqlite.Error
		if errors.As(err, &e) {
			code := e.Code()
			return code == 2067 || code == 1555
		}
		return false
	})
}
