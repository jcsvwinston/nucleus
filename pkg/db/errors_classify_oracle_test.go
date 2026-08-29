// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

//go:build oracle

package db

import (
	"fmt"
	"testing"

	goora "github.com/sijms/go-ora/v2/network"
)

// TestIsUniqueViolation_Oracle pins the Oracle mapping. It lives behind the
// same build tag that registers the driver.
func TestIsUniqueViolation_Oracle(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"ORA-00001 unique constraint violated", &goora.OracleError{ErrCode: 1}, true},
		{"ORA-02291 foreign key is not unique", &goora.OracleError{ErrCode: 2291}, false},
		{"ORA-00060 deadlock is not unique", &goora.OracleError{ErrCode: 60}, false},
		{"wrapped ORA-00001", fmt.Errorf("insert user: %w", &goora.OracleError{ErrCode: 1}), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsUniqueViolation(tc.err); got != tc.want {
				t.Errorf("IsUniqueViolation(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestPGSQLState_DoesNotCaptureOracle: same guard as the SQL Server case —
// if *network.OracleError ever grew a SQLState() method, the PostgreSQL
// branch would shadow Oracle entirely.
func TestPGSQLState_DoesNotCaptureOracle(t *testing.T) {
	if state, ok := pgSQLState(&goora.OracleError{ErrCode: 1}); ok {
		t.Errorf("pgSQLState captured an oracle error (state %q); the PostgreSQL branch "+
			"now shadows oracle and its violations classify as false", state)
	}
}
