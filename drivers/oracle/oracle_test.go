// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package oracle

import (
	"database/sql"
	"fmt"
	"slices"
	"testing"

	goora "github.com/sijms/go-ora/v2/network"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/db/driver/drivertest"
)

func TestClassifierConformance(t *testing.T) {
	drivertest.VerifyClassifier(t, drivertest.Case{
		Engine:    "oracle",
		Classify:  uniqueViolation,
		Violation: &goora.OracleError{ErrCode: 1}, // ORA-00001
		NotViolation: []error{
			&goora.OracleError{ErrCode: 2291}, // integrity constraint (parent key not found)
			&goora.OracleError{ErrCode: 1400}, // cannot insert NULL
		},
	})
}

func TestRegistersTheSQLDriver(t *testing.T) {
	if !slices.Contains(sql.Drivers(), "oracle") {
		t.Errorf("importing this module must register the \"oracle\" driver; registered: %v", sql.Drivers())
	}
}

// Importing the module is enough. This test binary links the module and the
// framework and nothing else: no RegisterAll, no other driver. If the
// framework's own predicate answers here, the classifier came from this
// module's init(), next to the driver — which is what an application gets.
func TestImportingTheModuleIsEnough(t *testing.T) {
	if !db.IsUniqueViolation(fmt.Errorf("insert user: %w", &goora.OracleError{ErrCode: 1})) {
		t.Error("db.IsUniqueViolation did not recognise a wrapped oracle violation with only this module imported")
	}
}
