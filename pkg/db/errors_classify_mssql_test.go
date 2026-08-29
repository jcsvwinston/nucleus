// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

//go:build mssql

package db

import (
	"fmt"
	"testing"

	mssql "github.com/microsoft/go-mssqldb"
)

// TestIsUniqueViolation_MSSQL pins the SQL Server mapping. It lives behind
// the same build tag that registers the driver: without `-tags mssql` the
// import is not in the build, so neither is this test.
func TestIsUniqueViolation_MSSQL(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"2627 unique constraint", mssql.Error{Number: 2627}, true},
		{"2601 duplicate key in unique index", mssql.Error{Number: 2601}, true},
		{"547 foreign key is not unique", mssql.Error{Number: 547}, false},
		{"1205 deadlock victim is not unique", mssql.Error{Number: 1205}, false},
		{"wrapped 2627", fmt.Errorf("insert user: %w", mssql.Error{Number: 2627}), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsUniqueViolation(tc.err); got != tc.want {
				t.Errorf("IsUniqueViolation(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestPGSQLState_DoesNotCaptureMSSQL guards the branch ordering for the
// engine that comes closest to colliding with it: go-mssqldb exposes
// SQLErrorState(), which differs from SQLState() in both name and return
// type. If a driver upgrade ever renamed it, the PostgreSQL branch would
// answer first and every SQL Server violation would classify as false.
func TestPGSQLState_DoesNotCaptureMSSQL(t *testing.T) {
	if state, ok := pgSQLState(mssql.Error{Number: 2627}); ok {
		t.Errorf("pgSQLState captured an mssql error (state %q); the PostgreSQL branch "+
			"now shadows mssql and its violations classify as false", state)
	}
}
