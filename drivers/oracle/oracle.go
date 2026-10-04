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

// The driver and its classifier are registered together, from this module
// and with nothing but Oracle's own error type: importing the module is
// enough, and it links no other engine (NU-8).
func init() {
	driver.MustRegisterUniqueViolation("oracle", uniqueViolation)
}

// uniqueViolation matches ORA-00001 ("unique constraint violated").
//
// go-ora/v2 returns a *network.OracleError; errors.As walks the Unwrap chain,
// so one that a caller wrapped still classifies.
func uniqueViolation(err error) bool {
	var e *goora.OracleError
	return errors.As(err, &e) && e.ErrCode == 1
}
