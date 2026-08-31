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
	"errors"

	_ "github.com/sijms/go-ora/v2"
	goora "github.com/sijms/go-ora/v2/network"

	"github.com/jcsvwinston/nucleus/pkg/db/driver"
)

func init() {
	driver.MustRegisterUniqueViolation("oracle", isUniqueViolation)
}

// isUniqueViolation reports whether err is ORA-00001 ("unique constraint
// violated").
//
// go-ora/v2 may return *network.OracleError directly or wrapped inside a
// *network.SessionError; errors.As walks the Unwrap chain, so one check
// covers both shapes — do not "simplify" this into a direct type switch.
func isUniqueViolation(err error) bool {
	var e *goora.OracleError
	if errors.As(err, &e) {
		return e.ErrCode == 1
	}
	return false
}
