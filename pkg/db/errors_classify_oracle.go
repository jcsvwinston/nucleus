//go:build oracle

// Package db: Oracle error classification.
// Activate with: go build -tags oracle
package db

import (
	"errors"

	goora "github.com/sijms/go-ora/v2/network"
)

// isOracleUniqueViolation reports whether err is ORA-00001 ("unique
// constraint violated").
//
// go-ora/v2 may return *network.OracleError directly or wrapped inside a
// *network.SessionError; errors.As walks the Unwrap chain, so one check
// covers both shapes — do not "simplify" this into a direct type switch.
func isOracleUniqueViolation(err error) bool {
	var oraErr *goora.OracleError
	if errors.As(err, &oraErr) {
		return oraErr.ErrCode == 1
	}
	return false
}
