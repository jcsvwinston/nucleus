// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package knownproviders

import (
	"database/sql"
	"fmt"
	"slices"
	"strings"
)

// The one refusal for a database/sql driver nothing linked, shared by every
// place the framework opens a database: pkg/db for the application's pools,
// and pkg/nucleustest for the one open it makes outside pkg/db
// (Transactional's probe of the base driver). Since v2.0.0 the test kit
// links no driver (DEP-2026-019), so a test binary that forgot the import
// meets this message on every path — the module that ships the driver, the
// command that adds it and the import line — instead of database/sql's
// `unknown driver "sqlite" (forgotten import?)`. It lives here, beside the
// catalog it reads, rather than in a package of its own: the hello-world
// size lane counts packages, and this is not worth one.

// RequireDBDriver reports a driver that is not registered with database/sql as the
// thing it actually is — a module not imported — when the framework
// publishes a module for it, and as a missing registration otherwise. It
// returns nil when the driver is registered.
func RequireDBDriver(driverName string) error {
	if slices.Contains(sql.Drivers(), driverName) {
		return nil
	}
	registered := "none"
	if linked := sql.Drivers(); len(linked) > 0 {
		registered = strings.Join(linked, ", ")
	}
	if p, ours := DBDriver(driverName); ours {
		return fmt.Errorf("db: the %s driver ships as its own module and is not imported yet.\n\n"+
			"\tAdd it to your build:\n\n%s\n\n"+
			"\t(linked right now: %s)",
			p.Name, p.InstallHint(), registered)
	}
	return fmt.Errorf("db: no database/sql driver is registered under %q — import the package that registers it (linked right now: %s)",
		driverName, registered)
}
