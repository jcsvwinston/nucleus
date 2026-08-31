// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package oracle links the Oracle driver into a Nucleus application.
//
// Import it for its side effect, once, anywhere in the program:
//
//	import _ "github.com/jcsvwinston/nucleus/drivers/oracle"
//
// It registers sijms/go-ora/v2 under "oracle", which is what pkg/db resolves
// an oracle:// URL to.
//
// This replaces the `-tags oracle` build tag that used to gate the driver —
// see the note in the mssql module on why an import beats a tag.
package oracle

import (
	_ "github.com/sijms/go-ora/v2"

	"github.com/jcsvwinston/nucleus/internal/dbclassify"
	"github.com/jcsvwinston/nucleus/pkg/db/driver"
)

func init() {
	driver.MustRegisterUniqueViolation("oracle", dbclassify.OracleUniqueViolation)
}
