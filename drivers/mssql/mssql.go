// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package mssql links the SQL Server driver into a Nucleus application.
//
// Import it for its side effect, once, anywhere in the program:
//
//	import _ "github.com/jcsvwinston/nucleus/drivers/mssql"
//
// It registers microsoft/go-mssqldb under "sqlserver", which is what pkg/db
// resolves a sqlserver:// or mssql:// URL to.
//
// This replaces the `-tags mssql` build tag that used to gate the driver. A
// build tag is invisible: nothing in the source says it exists, and a build
// that forgets it fails at run time with "unknown driver". An import is in
// the file, and a build that lacks it fails while compiling.
package mssql

import (
	_ "github.com/microsoft/go-mssqldb"

	"github.com/jcsvwinston/nucleus/internal/dbclassify"
	"github.com/jcsvwinston/nucleus/pkg/db/driver"
)

func init() {
	driver.MustRegisterUniqueViolation("sqlserver", dbclassify.MSSQLUniqueViolation)
}
