// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package mysql

import (
	"database/sql"
	"fmt"
	"slices"
	"testing"

	gomysql "github.com/go-sql-driver/mysql"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/db/driver/drivertest"
)

func TestClassifierConformance(t *testing.T) {
	drivertest.VerifyClassifier(t, drivertest.Case{
		Engine:    "mysql",
		Classify:  uniqueViolation,
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

// Importing the module is enough. This test binary links the module and the
// framework and nothing else: no RegisterAll, no other driver. If the
// framework's own predicate answers here, the classifier came from this
// module's init(), next to the driver — which is what an application gets.
func TestImportingTheModuleIsEnough(t *testing.T) {
	if !db.IsUniqueViolation(fmt.Errorf("insert user: %w", &gomysql.MySQLError{Number: 1062})) {
		t.Error("db.IsUniqueViolation did not recognise a wrapped mysql violation with only this module imported")
	}
}
