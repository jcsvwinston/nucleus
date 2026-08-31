// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package mysql links the MySQL/MariaDB driver into a Nucleus application.
//
// Import it for its side effect, once, anywhere in the program:
//
//	import _ "github.com/jcsvwinston/nucleus/drivers/mysql"
//
// It registers go-sql-driver/mysql under "mysql", which is what pkg/db
// resolves a mysql:// URL to, and it registers how that driver reports a
// unique-constraint violation so db.IsUniqueViolation keeps answering
// correctly.
package mysql

import (
	"errors"

	gomysql "github.com/go-sql-driver/mysql"
	_ "github.com/go-sql-driver/mysql"

	"github.com/jcsvwinston/nucleus/pkg/db/driver"
)

func init() {
	driver.MustRegisterUniqueViolation("mysql", isUniqueViolation)
}

// isUniqueViolation matches MySQL error 1062 (ER_DUP_ENTRY) on the CODE, not
// on the message: a server running with lc_messages set to another language
// answers the same rejection in that language, and a substring check would
// silently return false there.
func isUniqueViolation(err error) bool {
	var e *gomysql.MySQLError
	if errors.As(err, &e) {
		return e.Number == 1062
	}
	return false
}
