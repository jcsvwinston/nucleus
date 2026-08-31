// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package mysql

import (
	"database/sql"
	"slices"
	"testing"

	gomysql "github.com/go-sql-driver/mysql"

	"github.com/jcsvwinston/nucleus/pkg/db/driver/drivertest"
)

func TestClassifierConformance(t *testing.T) {
	drivertest.VerifyClassifier(t, drivertest.Case{
		Engine:    "mysql",
		Classify:  isUniqueViolation,
		Violation: &gomysql.MySQLError{Number: 1062},
		NotViolation: []error{
			&gomysql.MySQLError{Number: 1452}, // foreign key
			&gomysql.MySQLError{Number: 1048}, // not null
			&gomysql.MySQLError{Number: 1213}, // deadlock
		},
	})
}

// The point of the module is the side effect. A build that imports it and
// still cannot open a mysql:// URL has registered nothing.
func TestRegistersTheSQLDriver(t *testing.T) {
	if !slices.Contains(sql.Drivers(), "mysql") {
		t.Errorf("importing this module must register the \"mysql\" driver; registered: %v", sql.Drivers())
	}
}
