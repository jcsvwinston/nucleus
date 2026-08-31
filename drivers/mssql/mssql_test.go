// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package mssql

import (
	"database/sql"
	"slices"
	"testing"

	mssql "github.com/microsoft/go-mssqldb"

	"github.com/jcsvwinston/nucleus/pkg/db/driver/drivertest"
)

func TestClassifierConformance(t *testing.T) {
	drivertest.VerifyClassifier(t, drivertest.Case{
		Engine:   "sqlserver",
		Classify: isUniqueViolation,
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
	if !isUniqueViolation(mssql.Error{Number: 2601}) {
		t.Error("2601 (duplicate row in a unique INDEX) must classify as a unique violation")
	}
}

func TestRegistersTheSQLDriver(t *testing.T) {
	if !slices.Contains(sql.Drivers(), "sqlserver") {
		t.Errorf("importing this module must register the \"sqlserver\" driver; registered: %v", sql.Drivers())
	}
}
