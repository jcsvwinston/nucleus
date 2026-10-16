// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleustest_test

import (
	"database/sql"
	"slices"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
)

// DEP-2026-019 (NU-121) pins the v1 behaviour: importing nucleustest links
// the pure-Go SQLite driver, so TempSQLite and CheckModule work in a test
// binary that imports no driver of its own — and every such binary carries
// SQLite whether its application uses it or not. At v2.0.0 the test
// imports the driver it wants (drivers/sqlite, or another engine's) and
// nucleustest stops linking one; the 2.0 PR changes this test in the same
// commit.
func TestImportingNucleustestLinksSQLiteInV1(t *testing.T) {
	if !slices.Contains(sql.Drivers(), "sqlite") {
		t.Fatalf("sql.Drivers() = %v: importing nucleustest alone registers \"sqlite\" in v1", sql.Drivers())
	}
	dbs := nucleustest.TempSQLite(t)
	if dbs["default"].URL == "" {
		t.Fatal("TempSQLite returned no default database")
	}
}
