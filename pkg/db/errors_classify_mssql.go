//go:build mssql

// Package db: SQL Server error classification.
// Activate with: go build -tags mssql
package db

import (
	"errors"

	mssql "github.com/microsoft/go-mssqldb"
)

// isMSSQLUniqueViolation reports whether err is a SQL Server unique
// violation: 2627 (unique/primary-key CONSTRAINT) or 2601 (duplicate row in
// a unique INDEX). Both exist because the engine raises a different number
// depending on how uniqueness was declared, and a caller cares about neither
// distinction.
//
// mssql.Error has a value receiver on Error(), so errors.As must target the
// VALUE type, not a pointer to it.
func isMSSQLUniqueViolation(err error) bool {
	var mssqlErr mssql.Error
	if errors.As(err, &mssqlErr) {
		return mssqlErr.Number == 2627 || mssqlErr.Number == 2601
	}
	return false
}
