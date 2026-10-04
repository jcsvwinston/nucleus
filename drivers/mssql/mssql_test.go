// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package mssql

import (
	"database/sql"
	"fmt"
	"slices"
	"testing"

	mssql "github.com/microsoft/go-mssqldb"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/db/driver/drivertest"
)

func TestClassifierConformance(t *testing.T) {
	drivertest.VerifyClassifier(t, drivertest.Case{
		Engine:   "sqlserver",
		Classify: uniqueViolation,
		// 2627 is the constraint form; 2601 the unique-index form. Both are
		// covered because the engine picks between them by how uniqueness
		// was declared, which the caller never sees.
		Violation: mssql.Error{Number: 2627},
		NotViolation: []error{
			mssql.Error{Number: 2601 + 1}, // adjacent number, not a violation
			mssql.Error{Number: 547},      // foreign key / check
			mssql.Error{Number: 515},      // not null
		},
	})
	// The second violation number needs its own check: Case takes one.
	if !uniqueViolation(mssql.Error{Number: 2601}) {
		t.Error("2601 (duplicate row in a unique INDEX) must classify as a unique violation")
	}
}

func TestRegistersTheSQLDriver(t *testing.T) {
	if !slices.Contains(sql.Drivers(), "sqlserver") {
		t.Errorf("importing this module must register the \"sqlserver\" driver; registered: %v", sql.Drivers())
	}
}

// Importing the module is enough. This test binary links the module and the
// framework and nothing else: no RegisterAll, no other driver. If the
// framework's own predicate answers here, the classifier came from this
// module's init(), next to the driver — which is what an application gets.
func TestImportingTheModuleIsEnough(t *testing.T) {
	if !db.IsUniqueViolation(fmt.Errorf("insert user: %w", mssql.Error{Number: 2627})) {
		t.Error("db.IsUniqueViolation did not recognise a wrapped sqlserver violation with only this module imported")
	}
}
