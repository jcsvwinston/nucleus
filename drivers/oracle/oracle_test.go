// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package oracle

import (
	"database/sql"
	"slices"
	"testing"

	goora "github.com/sijms/go-ora/v2/network"

	"github.com/jcsvwinston/nucleus/pkg/db/driver/drivertest"
)

func TestClassifierConformance(t *testing.T) {
	drivertest.VerifyClassifier(t, drivertest.Case{
		Engine:    "oracle",
		Classify:  isUniqueViolation,
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
