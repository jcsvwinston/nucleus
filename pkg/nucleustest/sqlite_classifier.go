// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleustest

import (
	"sync"

	"github.com/jcsvwinston/nucleus/internal/dbclassify"
	"github.com/jcsvwinston/nucleus/pkg/db/driver"
)

// The kit links SQLite (runtime.go), and a driver is only half of what an
// engine needs: the other half is how it reports a unique violation. Without
// that classifier db.IsUniqueViolation does not fail — it answers false, and
// a test of the branch that turns "that email is taken" into a 409 fails for
// a reason the application does not have, or is never written (NU-117).
//
// An application gets both halves from drivers/sqlite. The kit cannot import
// that module, which requires this one, so it registers the root's copy of
// the predicate — internal/dbclassify's, the one the framework's own test
// binaries register. It matches the same two codes as the module's.
//
// It registers on first use, not in an init(). Registering an engine twice is
// an error that MustRegisterUniqueViolation turns into a panic, so a kit that
// registered at init would crash any test binary whose drivers/sqlite — or
// whose own init registering a classifier for SQLite — happened to run after
// it. By the time a test calls into the kit every init() has run, so the kit
// registers only when nothing else did, and never collides with whoever did.
var sqliteClassifierOnce sync.Once

// registerSQLiteClassifier makes db.IsUniqueViolation answer for the SQLite
// the kit links. Every kit entry point that can lead to a SQLite database
// calls it: StartApp (and so Start, CheckModule and CheckModuleIn),
// TempSQLite and Transactional.
func registerSQLiteClassifier() {
	sqliteClassifierOnce.Do(func() {
		if driver.HasEngine("sqlite") {
			return
		}
		// The only error left is "already registered", from a registration
		// that won a race with this one; either way SQLite has a classifier.
		_ = driver.RegisterUniqueViolation("sqlite", dbclassify.SQLiteUniqueViolation)
	})
}
